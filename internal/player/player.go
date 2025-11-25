package player

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/famish99/direttampd/internal/cache"
	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/decoder"
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

// DiscoverHosts discovers all available MemoryPlay hosts.
// Returns a list of all discovered hosts without selection.
func DiscoverHosts() ([]memoryplay.HostInfo, error) {
	log.Printf("Discovering MemoryPlay hosts...")
	hosts, err := memoryplay.ListHosts()
	if err != nil {
		return nil, fmt.Errorf("failed to discover hosts: %w", err)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no MemoryPlay hosts found")
	}
	return hosts, nil
}

// DiscoverTargets discovers all available targets from a MemoryPlay host.
// Returns a list of all discovered targets without selection.
func DiscoverTargets(hostIP string, hostIfNum uint32) ([]memoryplay.TargetInfo, error) {
	log.Printf("Discovering available targets...")
	targets, err := memoryplay.ListTargets(hostIP, hostIfNum)
	if err != nil {
		return nil, fmt.Errorf("failed to discover targets: %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found on host")
	}
	return targets, nil
}

// DiscoverAndSelectHost discovers available MemoryPlay hosts and selects one based on config.
// This function always performs discovery and returns the selected host info.
func DiscoverAndSelectHost(cfg *config.Config) (*memoryplay.HostInfo, error) {
	log.Printf("Discovering MemoryPlay hosts...")
	hosts, err := memoryplay.ListHosts()
	if err != nil || len(hosts) == 0 {
		return nil, fmt.Errorf("no MemoryPlay hosts found: %w", err)
	}

	// Select host: prefer config IP if specified, otherwise prefer loopback, otherwise first
	var selectedHost *memoryplay.HostInfo
	if cfg.Host.IP != "" {
		// Look for host matching config IP
		// Host IP format is "IP,PORT%IFNO" so we need to extract just the IP part
		for i := range hosts {
			hostIP := hosts[i].IPAddress
			// Strip port and interface: "::1,43425%0" -> "::1"
			if idx := strings.Index(hostIP, ","); idx != -1 {
				hostIP = hostIP[:idx]
			}
			if hostIP == cfg.Host.IP {
				selectedHost = &hosts[i]
				break
			}
		}
		if selectedHost == nil {
			return nil, fmt.Errorf("configured host IP %s not found", cfg.Host.IP)
		}
	} else {
		// Auto-select: prefer loopback
		for i := range hosts {
			if hosts[i].IsLoopback {
				selectedHost = &hosts[i]
				break
			}
		}
		if selectedHost == nil {
			selectedHost = &hosts[0]
		}
	}

	log.Printf("Discovered MemoryPlay host: %s%%%d (%s - %s)",
		selectedHost.IPAddress,
		selectedHost.InterfaceNumber,
		selectedHost.TargetName,
		selectedHost.OutputName)

	return selectedHost, nil
}

// DiscoverAndSelectTarget discovers available targets from a host and selects one based on config.
// This function always performs discovery and returns the selected target info.
func DiscoverAndSelectTarget(hostIP string, hostIfNum uint32, cfg *config.Config) (*memoryplay.TargetInfo, error) {
	log.Printf("Discovering available targets...")
	targets, err := memoryplay.ListTargets(hostIP, hostIfNum)
	if err != nil || len(targets) == 0 {
		return nil, fmt.Errorf("no targets found on host: %w", err)
	}

	// Select target: prefer config preferred_target if specified, otherwise first
	var selectedTarget *memoryplay.TargetInfo
	if cfg.PreferredTarget != "" {
		// Look for target matching config name
		for i := range targets {
			if targets[i].TargetName == cfg.PreferredTarget {
				selectedTarget = &targets[i]
				break
			}
		}
		if selectedTarget == nil {
			return nil, fmt.Errorf("configured target %s not found", cfg.PreferredTarget)
		}
	} else {
		selectedTarget = &targets[0]
	}

	return selectedTarget, nil
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
	if p.client != nil {
		err = p.client.Pause()
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
	if p.client != nil {
		if err := p.client.Play(); err != nil {
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

func (p *Player) Quit() error {
	if p.client != nil {
		return p.client.Quit()
	}
    return nil
}

// PlayTrack plays a single track
// Decodes to WAV format and uploads to MemoryPlay host
func (p *Player) PlayTrack(track *playlist.Track) error {
	log.Printf("Playing track: %s", track.URL)

	// Fetch and decode to cached WAV file
	wavPath, err := p.fetchDecodeAndCache(track)
	if err != nil {
		return fmt.Errorf("failed to fetch and decode: %w", err)
	}

	log.Printf("Using WAV file: %s", wavPath)

	// Open WAV file with C library
	wavFile, err := memoryplay.OpenWavFile(wavPath)
	if err != nil {
		// Invalidate cache - file may be corrupt
		if invalidateErr := p.cache.Invalidate(track.URL); invalidateErr != nil {
			log.Printf("Warning: failed to invalidate cache: %v", invalidateErr)
		}
		return fmt.Errorf("failed to open WAV file: %w", err)
	}
	defer wavFile.Close()

	// Get format handle from WAV file
	formatHandle, err := wavFile.GetFormat()
	if err != nil {
		// Invalidate cache - file may be corrupt
		if invalidateErr := p.cache.Invalidate(track.URL); invalidateErr != nil {
			log.Printf("Warning: failed to invalidate cache: %v", invalidateErr)
		}
		return fmt.Errorf("failed to get format: %w", err)
	}
	defer memoryplay.FreeFormat(formatHandle)

	// Upload audio to MemoryPlay host
	log.Printf("Uploading audio to MemoryPlay host...")

	if err := memoryplay.UploadAudio(p.hostIP, p.hostIfNum, []*memoryplay.WavFile{wavFile}, formatHandle, false); err != nil {
		// Invalidate cache - file may be corrupt or incompatible
		if invalidateErr := p.cache.Invalidate(track.URL); invalidateErr != nil {
			log.Printf("Warning: failed to invalidate cache: %v", invalidateErr)
		}
		return fmt.Errorf("failed to upload audio: %w", err)
	}

	// Ensure session is connected (needed for SelectTarget later)
	if err := p.Connect(); err != nil {
		return fmt.Errorf("failed to connect session: %w", err)
	}

	// Connect to target to start playback (this triggers playback automatically)
	if err := p.client.SelectTarget(); err != nil {
		return fmt.Errorf("failed to select target: %w", err)
	}
	log.Printf("Target connected, playback started")

	// Get track duration from metadata
	if durationStr, ok := track.Metadata["duration"]; ok && durationStr != "" {
		// Parse duration as float and convert to integer seconds
		var durationSec float64
		if _, err := fmt.Sscanf(durationStr, "%f", &durationSec); err == nil {
			p.mu.Lock()
			p.currentTrackDuration = int64(durationSec)
			p.lastRemainingTime = int64(durationSec)
			p.mu.Unlock()
			log.Printf("Track duration: %d seconds (from metadata)", int64(durationSec))
		}
	}

	return nil
}

// backgroundCache pre-fetches and decodes a track in the background
func (p *Player) backgroundCache(url string) {
	log.Printf("Background cache: starting for: %s", url)
	_, err := p.cache.EnsureDecoded(url, func(source, dest string) error {
		_, err := decoder.DecodeToWAVFile(source, dest)
		return err
	})
	if err != nil {
		log.Printf("Background cache: failed for %s: %v", url, err)
	} else {
		log.Printf("Background cache: completed for: %s", url)
	}
}

// fetchDecodeAndCache fetches and decodes audio directly to a WAV file in the cache
// Returns the WAV file path
func (p *Player) fetchDecodeAndCache(track *playlist.Track) (string, error) {
	log.Printf("Fetching and decoding track: %s", track.URL)
	cachePath, err := p.cache.EnsureDecoded(track.URL, func(source, dest string) error {
		_, err := decoder.DecodeToWAVFile(source, dest)
		return err
	})
	if err != nil {
		return "", err
	}
	log.Printf("Using cached WAV file: %s", cachePath)
	return cachePath, nil
}

// GetPlaylist returns the current playlist
func (p *Player) GetPlaylist() *playlist.Playlist {
	return p.pl
}

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

// BackgroundCacheTrack is a public wrapper for background caching
// Exposes backgroundCache for MPD commands to use
func (p *Player) BackgroundCacheTrack(url string) {
	p.backgroundCache(url)
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