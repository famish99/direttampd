package player

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/famish99/direttampd/internal/backends"
	"github.com/famish99/direttampd/internal/backends/memoryplay"
	"github.com/famish99/direttampd/internal/cache"
	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/playlist"
)

// Player coordinates audio playback using a pluggable backend
type Player struct {
	mu      sync.Mutex
	config  *config.Config
	backend backends.PlaybackBackend
	cache   *cache.DiskCache
	pl      *playlist.Playlist

	// Playlist transition support
	pendingPlaylist *playlist.Playlist // New playlist being built during transition (nil if not transitioning)
	playbackCtx     context.Context    // Controls current playback loop
	playbackCancel  context.CancelFunc // Cancels current playback loop

	// Playback state
	state PlaybackState

	// Cached timing info (updated by polling loop)
	lastElapsedTime int64 // Elapsed time in seconds (from backend polling)

	// Subsystem change notification callback (e.g., for MPD idle notifications)
	notifySubsystem func(subsystem string)
}

// NewPlayer creates a new player instance with a MemoryPlay backend
func NewPlayer(cfg *config.Config, useNative bool) (*Player, error) {
	// Create cache
	cacheSize := int64(cfg.Cache.MaxSizeGB) * 1024 * 1024 * 1024
	c, err := cache.NewDiskCache(cfg.Cache.Directory, cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	// Create the MemoryPlay backend (handles init and discovery internally)
	backend, err := memoryplay.New(c, cfg, useNative)
	if err != nil {
		return nil, fmt.Errorf("failed to create backend: %w", err)
	}

	return &Player{
		config:          cfg,
		backend:         backend,
		cache:           c,
		pl:              playlist.NewPlaylist(),
		state:           StateStopped,
		notifySubsystem: nil,
	}, nil
}

// SetNotifySubsystem sets the callback for subsystem change notifications
// The callback will be invoked when player or playlist state changes
func (p *Player) SetNotifySubsystem(callback func(subsystem string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notifySubsystem = callback
}

// Close cleans up the player resources
func (p *Player) Close() {
	log.Printf("Closing player")
	if p.backend != nil {
		p.backend.Close()
	}
}

// GetPlaylist returns the current playlist
func (p *Player) GetPlaylist() *playlist.Playlist {
	return p.pl
}

// GetOutputName returns the name of the output device
func (p *Player) GetOutputName() string {
	if p.backend != nil {
		return p.backend.GetOutputName()
	}
	return ""
}
