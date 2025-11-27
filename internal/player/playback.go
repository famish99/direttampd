package player

import (
	"context"
	"fmt"
	"log"
)

// AddURLs adds URLs to the playlist and starts background caching
func (p *Player) AddURLs(urls []string) {
	p.pl.AddMultiple(urls)
	log.Printf("Added %d URLs to playlist", len(urls))

	// Start background caching for all added tracks
	for _, url := range urls {
		go p.backgroundCache(url)
	}
}

// AddURLAt adds a URL at a specific position and starts background caching
// Returns the position where the track was added
// If adding at or before current position while playing, restarts playback
func (p *Player) AddURLAt(url string, position int) int {
	// Add track at position
	actualPosition := p.pl.AddAt(url, position)
	log.Printf("Added URL at position %d: %s", actualPosition, url)

	// Start background caching
	go p.backgroundCache(url)

	return actualPosition
}

// Play starts playback of a new track
func (p *Player) Play() error {
	p.mu.Lock()

	// Start new playback from current position
	p.state = StatePlaying

	// Create cancellable context for playback loop
	ctx, cancel := context.WithCancel(context.Background())
	p.playbackCtx = ctx
	p.playbackCancel = cancel

	// Capture playlist for playback loop to close over
	pl := p.pl
	p.mu.Unlock()

	// Start playback loop in goroutine (will play from current position)
	go p.playbackLoop(ctx, pl)

	return nil
}

// PlayAt seeks to a specific position and starts playback
func (p *Player) PlayAt(position int) error {
	// If already playing, cancel the playback loop
	p.mu.Lock()
	wasPlaying := p.state == StatePlaying
	if wasPlaying {
		// Cancel playback loop via context
		if p.playbackCancel != nil {
			log.Printf("Cancelling playback loop for PlayAt")
			p.playbackCancel()
			p.playbackCancel = nil
			p.playbackCtx = nil
		}
	}
	p.mu.Unlock()

	// Seek to the position (stages the track)
	if err := p.pl.Seek(position); err != nil {
		return fmt.Errorf("failed to seek to position %d: %w", position, err)
	}

	// Commit the staged position
	if err := p.pl.CommitStaged(); err != nil {
		return fmt.Errorf("failed to commit seek: %w", err)
	}

	// Now call Play() to start playback
	return p.Play()
}

// Pause pauses playback (can be resumed with Resume)
func (p *Player) Pause() error {
	p.mu.Lock()

	if p.state != StatePlaying {
		p.mu.Unlock()
		return nil // Already paused or not playing
	}

	log.Printf("Pausing playback")
	p.state = StatePaused

	var err error
	if p.backend != nil {
		err = p.backend.Pause()
	}
	p.mu.Unlock()

	// Notify subsystem change
	if p.notifySubsystem != nil {
		p.notifySubsystem("player")
	}

	return err
}

// Resume resumes playback from pause
func (p *Player) Resume() error {
	p.mu.Lock()

	if p.state != StatePaused {
		p.mu.Unlock()
		return nil // Not paused, nothing to resume
	}

	log.Printf("Resuming playback from pause")
	if p.backend != nil {
		if err := p.backend.Play(); err != nil {
			p.mu.Unlock()
			return fmt.Errorf("failed to resume playback: %w", err)
		}

		// Unlock before waiting
		p.mu.Unlock()

		// Wait for playback to actually start
		if !p.waitForPlaybackStart() {
			p.mu.Lock()
			p.state = StatePaused // Restore paused state on failure
			p.mu.Unlock()
			return fmt.Errorf("timeout waiting for playback to resume")
		}

		// Only change to playing state after playback confirmed
		p.mu.Lock()
		p.state = StatePlaying
		p.mu.Unlock()
	} else {
		p.state = StatePlaying
		p.mu.Unlock()
	}

	// Notify subsystem change
	if p.notifySubsystem != nil {
		p.notifySubsystem("player")
	}

	return nil
}

// Next skips to the next track in the playlist
func (p *Player) Next() error {
	// Stage next track in playlist
	err := p.pl.Next()
	if err != nil {
		return err
	}

	// Signal interrupt to playback loop (notify, don't exit)
	p.pl.SignalInterrupt(true, false)

	return nil
}

// Previous skips to the previous track in the playlist
func (p *Player) Previous() error {
	// Stage previous track in playlist
	err := p.pl.Previous()
	if err != nil {
		return err
	}

	// Signal interrupt to playback loop (notify, don't exit)
	p.pl.SignalInterrupt(true, false)

	return nil
}

// Stop stops playback completely (cannot be resumed, unlike Pause)
func (p *Player) Stop() error {
	p.mu.Lock()

	p.state = StateStopped

	// Cancel playback loop via context (cleaner than interrupt)
	if p.playbackCancel != nil {
		log.Printf("Stopping playback by cancelling context")
		p.playbackCancel()
		p.playbackCancel = nil
		p.playbackCtx = nil
	}

	p.mu.Unlock()

	// Wait for playback to actually stop before notifying
	p.waitForPlaybackStop()

	// Notify subsystem change
	if p.notifySubsystem != nil {
		p.notifySubsystem("player")
	}

	return nil
}

// Quit quits the current playback session
func (p *Player) Quit() error {
	if p.backend != nil {
		return p.backend.Stop()
	}
	return nil
}

// Seek seeks to an absolute position in seconds within the current track
func (p *Player) Seek(positionSeconds int64) error {
	p.mu.Lock()

	if p.backend == nil {
		p.mu.Unlock()
		return fmt.Errorf("no backend available")
	}

	if p.state != StatePlaying && p.state != StatePaused {
		p.mu.Unlock()
		return fmt.Errorf("not playing or paused")
	}

	log.Printf("Seeking to position %d seconds", positionSeconds)
	err := p.backend.Seek(positionSeconds)
	p.mu.Unlock()

	// Notify subsystem change so MPD clients update their display
	if err == nil && p.notifySubsystem != nil {
		p.notifySubsystem("player")
	}

	return err
}

// SeekCur seeks relative to current position (offsetSeconds can be positive or negative)
func (p *Player) SeekCur(offsetSeconds int64) error {
	p.mu.Lock()

	if p.backend == nil {
		p.mu.Unlock()
		return fmt.Errorf("no backend available")
	}

	if p.state != StatePlaying && p.state != StatePaused {
		p.mu.Unlock()
		return fmt.Errorf("not playing or paused")
	}

	// Get current elapsed time
	elapsed, err := p.backend.GetElapsedTime()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("failed to get current time: %w", err)
	}

	// Calculate new absolute position
	newPosition := elapsed + offsetSeconds
	if newPosition < 0 {
		newPosition = 0
	}

	log.Printf("Seeking by %d seconds to position %d seconds", offsetSeconds, newPosition)
	err = p.backend.Seek(newPosition)
	p.mu.Unlock()

	// Notify subsystem change so MPD clients update their display
	if err == nil && p.notifySubsystem != nil {
		p.notifySubsystem("player")
	}

	return err
}
