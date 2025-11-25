package player

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/famish99/direttampd/internal/decoder"
	"github.com/famish99/direttampd/internal/playlist"
)

// GetPendingPlaylist returns the pending playlist (nil if not transitioning)
func (p *Player) GetPendingPlaylist() *playlist.Playlist {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pendingPlaylist
}

// BeginTransition creates a new playlist instance for transition
func (p *Player) BeginTransition() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pendingPlaylist = playlist.NewPlaylist()
	log.Printf("Created new pending playlist for transition")
}

// CancelTransition discards the pending playlist
func (p *Player) CancelTransition() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingPlaylist != nil {
		p.pendingPlaylist = nil
		log.Printf("Cancelled transition")
	}
}

// ReplacePlaylist atomically replaces the current playlist with a new one
// This should only be used when playback is stopped (no active playback loop)
func (p *Player) ReplacePlaylist(newPl *playlist.Playlist) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pl = newPl
	log.Printf("Replaced playlist with new instance")
}

// CompleteTransition waits for cache, cancels old loop, starts new loop
func (p *Player) CompleteTransition() error {
	p.mu.Lock()
	pending := p.pendingPlaylist
	if pending == nil {
		p.mu.Unlock()
		return fmt.Errorf("no pending playlist")
	}
	p.mu.Unlock()

	// Get first track from pending playlist
	firstTrack, err := pending.Current()
	if err != nil {
		return fmt.Errorf("no tracks in pending playlist: %w", err)
	}

	log.Printf("Completing transition - waiting for cache: %s", firstTrack.URL)

	// Wait for cache with timeout
	done := make(chan error, 1)
	go func() {
		_, err := p.cache.EnsureDecoded(firstTrack.URL, func(source, dest string) error {
			_, err := decoder.DecodeToWAVFile(source, dest)
			return err
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("cache failed: %w", err)
		}
	case <-time.After(30 * time.Second):
		return fmt.Errorf("cache timeout")
	}

	log.Printf("Cache ready, swapping playlists")

	// Cancel old playback loop if running
	p.mu.Lock()
	if p.playbackCancel != nil {
		log.Printf("Cancelling old playback loop")
		p.playbackCancel()
		p.playbackCancel = nil
		p.playbackCtx = nil
	}

	// Atomic reference swap
	p.pl = pending
	p.pendingPlaylist = nil

	// Create new context for new playback loop
	ctx, cancel := context.WithCancel(context.Background())
	p.playbackCtx = ctx
	p.playbackCancel = cancel
	p.state = StatePlaying

	// Capture playlist for closure
	pl := p.pl
	p.mu.Unlock()

	// Start new playback loop
	log.Printf("Starting new playback loop with transitioned playlist")
	go p.playbackLoop(ctx, pl)

	// Notify subsystem change
	if p.notifySubsystem != nil {
		p.notifySubsystem("player")
	}

	log.Printf("Transition complete")
	return nil
}