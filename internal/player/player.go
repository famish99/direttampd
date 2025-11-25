package player

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/famish99/direttampd/internal/cache"
	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/memoryplay"
	"github.com/famish99/direttampd/internal/playlist"
)

// Player coordinates audio playback to MemoryPlay targets
type Player struct {
	mu     sync.Mutex
	config *config.Config
	client *memoryplay.Client
	cache  *cache.DiskCache
	pl     *playlist.Playlist

	// Playlist transition support
	pendingPlaylist *playlist.Playlist // New playlist being built during transition (nil if not transitioning)
	playbackCtx     context.Context    // Controls current playback loop
	playbackCancel  context.CancelFunc // Cancels current playback loop

	// Discovered connection info
	hostIP     string
	hostIfNum  uint32
	targetIP   string
	targetPort string
	targetIf   uint32
	targetName string

	// Client configuration
	useNative bool // Use native Go implementation instead of CGo

	// Playback state
	state PlaybackState

	// Current track timing info
	currentTrackDuration   int64 // Duration in seconds
	lastRemainingTime      int64   // Last known elapsed time in seconds (cached from polling)

	// Subsystem change notification callback (e.g., for MPD idle notifications)
	notifySubsystem func(subsystem string)
}

// NewPlayer creates a new player instance.
//
// This function checks the config first to determine if discovery is needed.
// Discovery is only performed if the config doesn't contain the necessary connection information.
// The client connection is established lazily when Connect() is called, allowing for:
//   1. NewPlayer() - setup with config or discovery
//   2. Upload audio data using the host info
//   3. Connect() - establish the control session
//   4. SelectTarget() - connect to the target and start playback
func NewPlayer(cfg *config.Config, useNative bool) (*Player, error) {
	// Initialize the MemoryPlay C library
	if err := memoryplay.InitLibrary(true, false); err != nil {
		return nil, fmt.Errorf("failed to initialize MemoryPlay library: %w", err)
	}

	var hostIP string
	var hostIfNum uint32
	var targetIP, targetPort, targetName string
	var targetIf uint32

    // Perform host discovery
    selectedHost, err := DiscoverAndSelectHost(cfg)
    if err != nil {
        memoryplay.CleanupLibrary()
        return nil, err
    }
    hostIP = selectedHost.IPAddress
    hostIfNum = selectedHost.InterfaceNumber

	// Check if we need to discover target or can use config
	preferredTarget := cfg.GetPreferredTarget()
	needTargetDiscovery := preferredTarget == nil ||
		preferredTarget.IP == "" ||
		preferredTarget.Port == "" ||
		preferredTarget.Interface == ""

	if needTargetDiscovery {
		// Perform target discovery
		selectedTarget, err := DiscoverAndSelectTarget(hostIP, hostIfNum, cfg)
		if err != nil {
			memoryplay.CleanupLibrary()
			return nil, err
		}

		// Parse port from target IP (format: "IP,PORT")
		targetIP = selectedTarget.IPAddress
		targetPort = "19644" // default
		if strings.Contains(targetIP, ",") {
			parts := strings.SplitN(targetIP, ",", 2)
			targetIP = parts[0]
			targetPort = parts[1]
		}
		targetName = selectedTarget.TargetName
		targetIf = selectedTarget.InterfaceNumber

		log.Printf("Discovered target: %s (IP: %s,%s%%%d)",
			targetName, targetIP, targetPort, targetIf)
	} else {
		// Use config values
		targetIP = preferredTarget.IP
		targetPort = preferredTarget.Port
		if targetPort == "" {
			targetPort = "19644" // default
		}
		targetName = preferredTarget.Name

		// Parse interface number from string
		var ifNum uint32
		fmt.Sscanf(preferredTarget.Interface, "%d", &ifNum)
		targetIf = ifNum

		log.Printf("Using configured target: %s (IP: %s,%s%%%d)",
			targetName, targetIP, targetPort, targetIf)
	}

	// Create cache
	cacheSize := int64(cfg.Cache.MaxSizeGB) * 1024 * 1024 * 1024
	c, err := cache.NewDiskCache(cfg.Cache.Directory, cacheSize)
	if err != nil {
		memoryplay.CleanupLibrary()
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	// Store connection info for later use
	// NOTE: We don't create the client yet - it will be created when Connect() is called
	// This allows for the proper sequence: discovery/config -> upload -> connect
	return &Player{
		config:            cfg,
		client:            nil, // Client created lazily in Connect()
		cache:             c,
		pl:                playlist.NewPlaylist(),
		hostIP:            hostIP,
		hostIfNum:         hostIfNum,
		targetIP:          targetIP,
		targetPort:        targetPort,
		targetIf:          targetIf,
		targetName:        targetName,
		useNative:         useNative,
		state:             StateStopped,
		lastRemainingTime: -1, // Initialize to -1 to indicate not yet set
		notifySubsystem:   nil, // No notification callback by default
	}, nil
}

// SetNotifySubsystem sets the callback for subsystem change notifications
// The callback will be invoked when player or playlist state changes
func (p *Player) SetNotifySubsystem(callback func(subsystem string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notifySubsystem = callback
}

// GetHostInfo returns the discovered host connection information
// This can be used for uploading audio before establishing the control connection
func (p *Player) GetHostInfo() (hostIP string, hostIfNum uint32) {
	return p.hostIP, p.hostIfNum
}

// GetTargetInfo returns the discovered target connection information
func (p *Player) GetTargetInfo() (targetIP, targetPort, targetName string, targetIf uint32) {
	return p.targetIP, p.targetPort, p.targetName, p.targetIf
}

// Close cleans up the player resources
func (p *Player) Close() {
    log.Printf("Closing player")
	memoryplay.CleanupLibrary()
}

// Connect establishes the session to the MemoryPlay host
// This method creates the client (if not already created) and connects to the host.
// Should be called AFTER audio upload is complete.
func (p *Player) Connect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Lazily create the client on first connect
	// This ensures upload happens before client connection is established
	if p.client == nil {
		log.Printf("Creating MemoryPlay client for target: %s (native: %v)", p.targetName, p.useNative)
		mpTarget := &memoryplay.Target{
			Name:      p.targetName,
			IP:        p.targetIP,
			Port:      p.targetPort,
			Interface: fmt.Sprintf("%d", p.targetIf),
		}
		p.client = memoryplay.NewClient(p.hostIP, mpTarget, p.useNative)
	}

	if err := p.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to MemoryPlay: %w", err)
	}
	log.Printf("Connected to MemoryPlay session")

	return nil
}

// Disconnect closes the MemoryPlay connection
func (p *Player) Disconnect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return p.client.Disconnect()
	}
	return nil
}

// GetPlaylist returns the current playlist
func (p *Player) GetPlaylist() *playlist.Playlist {
	return p.pl
}