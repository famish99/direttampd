package player

import (
	"context"
	"log"
	"time"

	"github.com/famish99/direttampd/internal/playlist"
)

// playbackLoop is the main playback loop that processes the playlist
// Closes over the playlist to work independently of player state changes
func (p *Player) playbackLoop(ctx context.Context, pl *playlist.Playlist) {
	defer log.Printf("Playback loop exiting")

	// Get interrupt channel from the closed-over playlist
	interruptCh := pl.GetInterruptChannel()

	for {
		// Check if context cancelled (transition to new playlist)
		select {
		case <-ctx.Done():
			log.Printf("Playback loop cancelled via context")
            p.client.Quit()
			return
		default:
		}

		// Check if we should stop
		p.mu.Lock()
		if p.state != StatePlaying {
			p.mu.Unlock()
			log.Printf("Playback loop stopped")
			return
		}
		p.mu.Unlock()

		// Get current track from closed-over playlist
		track, err := pl.Current()
		if err != nil {
			log.Printf("Invalid current track")
			p.Stop()
			return
		}

		// Play the track
		currentIndex := pl.CurrentIndex()
		log.Printf("Playing track %d: %s", currentIndex, track.URL)
		err = p.PlayTrack(track)
		if err != nil {
			log.Printf("Error playing track %s: %v", track.URL, err)
            return
		}

		// Notify that player state changed (track started)
		p.mu.Lock()
		if p.notifySubsystem != nil {
			p.notifySubsystem("player")
		}
		p.mu.Unlock()

		// Wait for track to finish playing or be interrupted
		shouldNotify, shouldExit := p.waitForTrackCompletion(ctx, interruptCh)

		// Notify that player state changed (track finished) if requested
		if shouldNotify {
			p.mu.Lock()
			if p.notifySubsystem != nil {
				p.notifySubsystem("player")
			}
			p.mu.Unlock()
		}

		// Exit loop if interrupt requested it (e.g., Stop or PlayAt)
		if shouldExit {
			log.Printf("Playback loop exiting due to interrupt")
			p.client.Quit()
			return
		}

		// Commit staged track (from interrupt) or auto-advance to next
		err = pl.CommitStaged()
		if err != nil {
			// Reached end of playlist or error
			p.Stop()
            return
		}
	}
}

// waitForCondition polls GetCurrentTime until checkFn returns true or timeout occurs
func (p *Player) waitForCondition(checkFn func(int64, error) bool, timeout time.Duration, successMsg, timeoutMsg string) bool {
	if p.client == nil {
		return true // No client, nothing to wait for
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timer.C:
			log.Printf(timeoutMsg)
			return false
		case <-ticker.C:
			currentTime, err := p.client.GetCurrentTime()
			if checkFn(currentTime, err) {
				log.Printf(successMsg)
				return true
			}
		}
	}
}

// waitForPlaybackStart waits for playback to actually start
// Returns true if playback started, false on timeout
func (p *Player) waitForPlaybackStart() bool {
	return p.waitForCondition(
		func(time int64, err error) bool { return err == nil && time >= 0 },
		5*time.Second,
		"Playback started",
		"Timeout waiting for playback to start",
	)
}

// waitForPlaybackStop waits for playback to actually stop
// Returns true if playback stopped, false on timeout
func (p *Player) waitForPlaybackStop() bool {
	return p.waitForCondition(
		func(time int64, err error) bool { return err != nil || time == -1 },
		2*time.Second,
		"Playback stopped",
		"Timeout waiting for playback to stop",
	)
}

// waitForTrackCompletion polls until the track finishes or is interrupted
// Returns (shouldNotify, shouldExitLoop) - whether to notify subsystem and whether to exit playback loop
func (p *Player) waitForTrackCompletion(ctx context.Context, interruptCh <-chan playlist.InterruptEvent) (bool, bool) {
	if p.client == nil {
		return true, true // Default to notify, don't exit
	}

	// Wait for playback to actually start
	log.Printf("waitForTrackCompletion: waiting for playback to start")
	if !p.waitForPlaybackStart() {
		log.Printf("waitForTrackCompletion: playback failed to start (timeout)")
		return true, true // Default to notify, don't exit
	}
	log.Printf("waitForTrackCompletion: playback started successfully")

	// Poll current time until it returns -1 (track finished) or interrupt received
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Playback loop cancelled (transition)
			log.Printf("Track completion polling cancelled via context")
			return false, true  // Exit loop

		case event := <-interruptCh:
			// Interrupted by playlist event (Next/Previous/Stop)
			log.Printf("Track interrupted (shouldNotify: %v, shouldExitLoop: %v)", event.ShouldNotify, event.ShouldExitLoop)
			return event.ShouldNotify, event.ShouldExitLoop

		case <-ticker.C:
			// Check if we should stop polling
			p.mu.Lock()
			state := p.state
			p.mu.Unlock()

			// Stop polling if explicitly stopped or playback ended
			if state == StateStopped {
				return false, true
			}

			// If paused, just continue waiting (don't check time)
			if state == StatePaused {
				continue
			}

			// Get current playback time
			currentTime, err := p.client.GetCurrentTime()
			log.Printf("waitForTrackCompletion: GetCurrentTime() returned: %d", currentTime)
			if err != nil {
				return false, true
			}

			// Cache the elapsed time for GetPlaybackTiming to use
			p.mu.Lock()
			p.lastRemainingTime = currentTime
			p.mu.Unlock()

			// Track is finished when GetCurrentTime returns -1
			if currentTime == -1 {
				log.Printf("Track finished naturally")
				return true, false // Natural completion - notify, don't exit
			}
		}
	}
}