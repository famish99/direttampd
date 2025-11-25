package player

// PlaybackState represents the current playback state
type PlaybackState int

const (
	StateStopped PlaybackState = iota
	StatePlaying
	StatePaused
)

// GetState returns the current playback state
func (p *Player) GetState() PlaybackState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// PlaybackTiming contains current playback timing information
type PlaybackTiming struct {
	Elapsed   int64 // Elapsed time in seconds
	Duration  int64 // Total duration in seconds
	Remaining int64 // Remaining time in seconds
}

// GetPlaybackTiming returns current playback timing information
// Returns nil if no track is currently playing or timing info is unavailable
// Uses cached elapsed time updated by the polling loop to avoid blocking
func (p *Player) GetPlaybackTiming() *PlaybackTiming {
	p.mu.Lock()
	duration := p.currentTrackDuration
	remaining := p.lastRemainingTime
	p.mu.Unlock()

// 	log.Printf("remaining: %d duration: %d", remaining, duration)

	// Return nil if we don't have duration info
	if duration <= 0 {
		return nil
	}

	// Return nil if elapsed time is negative (not yet set or track finished)
	if remaining < 0 {
		return nil
	}

    elapsed := duration - remaining
	if elapsed < 0 {
		elapsed = 0
	}

	return &PlaybackTiming{
		Elapsed:   elapsed,
		Duration:  duration,
		Remaining: remaining,
	}
}