package player

import (
	"fmt"
	"log"
	"os"
	"strconv"
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

	// Discovered connection info
	hostIP     string
	hostIfNum  uint32
	targetIP   string
	targetPort string
	targetIf   uint32
	targetName string

	// Playback state
	playing bool
	paused  bool
	stopped bool

	// Current track timing info
	currentTrackDuration   int64 // Duration in seconds
	lastRemainingTime      int64   // Last known elapsed time in seconds (cached from polling)
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
func NewPlayer(cfg *config.Config) (*Player, error) {
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
		config:          cfg,
		client:          nil, // Client created lazily in Connect()
		cache:           c,
		pl:              playlist.NewPlaylist(),
		hostIP:          hostIP,
		hostIfNum:       hostIfNum,
		targetIP:        targetIP,
		targetPort:      targetPort,
		targetIf:        targetIf,
		targetName:      targetName,
		playing:         false,
		stopped:         false,
		lastRemainingTime: -1, // Initialize to -1 to indicate not yet set
	}, nil
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
		log.Printf("Creating MemoryPlay client for target: %s", p.targetName)
		mpTarget := &memoryplay.Target{
			Name:      p.targetName,
			IP:        p.targetIP,
			Port:      p.targetPort,
			Interface: fmt.Sprintf("%d", p.targetIf),
		}
		p.client = memoryplay.NewClient(p.hostIP, mpTarget)
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

// Play starts playback from the beginning or resumes from pause
func (p *Player) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If paused, resume playback
	if p.paused {
		log.Printf("Resuming playback from pause")
		p.paused = false
		if p.client != nil {
			if err := p.client.Play(); err != nil {
				return fmt.Errorf("failed to resume playback: %w", err)
			}
		}
		return nil
	}

	// If already playing, nothing to do
	if p.playing {
		return nil
	}

	// Start new playback
	p.playing = true
	p.stopped = false

	// Start playback loop in goroutine
	p.pl.Seek(0)
	go p.playbackLoop()

	return nil
}

// Pause pauses playback (can be resumed with Play)
func (p *Player) Pause() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.playing || p.paused {
		return nil // Already paused or not playing
	}

	log.Printf("Pausing playback")
	p.paused = true

	if p.client != nil {
		return p.client.Pause()
	}
	return nil
}

// playbackLoop is the main playback loop that processes the playlist
func (p *Player) playbackLoop() {
	log.Printf("Starting playback loop")

	for {
		// Check if we should stop
		p.mu.Lock()
		if p.stopped || !p.playing {
			p.mu.Unlock()
			log.Printf("Playback loop stopped")
			return
		}
		p.mu.Unlock()

		// Get current track, or start from beginning if no current track
		track, err := p.pl.Current()
		if err != nil {
			log.Printf("Invalid current track")
			p.Stop()
			return
		}

		// Play the track
		log.Printf("Playing track %d: %s", track.Index, track.URL)
		err = p.PlayTrack(track)
		if err != nil {
			log.Printf("Error playing track %s: %v", track.URL, err)
			p.Stop()
            return
		}

		// Wait for track to finish playing by polling session status
		p.waitForTrackCompletion()

		// Track completed successfully, advance to next
		_, err = p.pl.Next()
		if err != nil {
			// Reached end of playlist, wait for more tracks to be added
			p.Stop()
            return
		}
	}
}

// waitForTrackCompletion polls until the track finishes (current time returns -1)
func (p *Player) waitForTrackCompletion() {
	if p.client == nil {
		return
	}

	// Poll current time until it returns -1 (track finished)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if we should stop polling
			p.mu.Lock()
			stopped := p.stopped
			playing := p.playing
			paused := p.paused
			p.mu.Unlock()

			// Stop polling if explicitly stopped or playback ended
			if stopped || !playing {
				return
			}

			// If paused, just continue waiting (don't check time)
			if paused {
				continue
			}

			// Get current playback time
			currentTime, err := p.client.GetCurrentTime()
			if err != nil {
				log.Printf("Error getting current time: %v", err)
				return
			}

		// Cache the elapsed time for GetPlaybackTiming to use
			p.mu.Lock()
			p.lastRemainingTime = currentTime
			p.mu.Unlock()

			// Track is finished when GetCurrentTime returns -1
			if currentTime == -1 {
				log.Printf("Track finished")
				return
			}
		}
	}
}

// Stop stops playback completely (cannot be resumed, unlike Pause)
func (p *Player) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopped = true
	p.playing = false
	p.paused = false

	if p.client != nil {
		return p.client.Quit()
	}
	return nil
}

// PlayTrack plays a single track using the C library
// The C library supports multiple formats including AIFF, so we decode to AIFF for better metadata support
func (p *Player) PlayTrack(track *playlist.Track) error {
	log.Printf("Playing track: %s", track.URL)

	// Fetch and decode to cached AIFF file
	aiffPath, err := p.fetchDecodeAndCache(track)
	if err != nil {
		return fmt.Errorf("failed to fetch and decode: %w", err)
	}

	log.Printf("Using AIFF file: %s", aiffPath)

	// Open AIFF file with C library
	wavFile, err := memoryplay.OpenWavFile(aiffPath)
	if err != nil {
		return fmt.Errorf("failed to open WAV file: %w", err)
	}
	defer wavFile.Close()

	// Get format handle from WAV file
	formatHandle, err := wavFile.GetFormat()
	if err != nil {
		return fmt.Errorf("failed to get format: %w", err)
	}

	// Upload audio to MemoryPlay host
	log.Printf("Uploading audio to MemoryPlay host...")

	if err := memoryplay.UploadAudio(p.hostIP, p.hostIfNum, []*memoryplay.WavFile{wavFile}, formatHandle, false); err != nil {
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

	// Reset elapsed time for new track
	p.mu.Lock()
	p.lastRemainingTime = 0
	p.mu.Unlock()

	// Get track duration from tag list
	// Tag format is "INDEX:TIME:NAME" where TIME is duration in seconds
	tags, err := p.client.GetTagList()
	if err == nil && len(tags) > 0 {
		// Find the tag that matches the current AIFF file
		// Extract just the filename from the full path for matching
		aiffFilename := aiffPath
		if lastSlash := strings.LastIndex(aiffPath, "/"); lastSlash != -1 {
			aiffFilename = aiffPath[lastSlash+1:]
		}

		// Search through tags to find the matching one
		for _, tag := range tags {
			tagParts := strings.Split(tag.Tag, ":")
			if len(tagParts) >= 3 {
				// tagParts[2] contains the NAME portion
				tagName := tagParts[2]
				// Check if the tag name partially matches the AIFF filename
				if strings.Contains(aiffFilename, tagName) || strings.Contains(tagName, aiffFilename) {
					if duration, err := strconv.ParseInt(tagParts[1], 10, 64); err == nil {
						p.mu.Lock()
						p.currentTrackDuration = duration
						p.mu.Unlock()
						log.Printf("Track duration: %d seconds (matched tag: %s)", duration, tagName)
						break
					}
				}
			}
		}
	}

	return nil
}

// backgroundCache pre-fetches and decodes a track in the background
func (p *Player) backgroundCache(url string) {
	cachePath := p.cache.GetPathForKey(url)

	// Check if already cached
	if _, err := os.Stat(cachePath); err == nil {
		log.Printf("Background cache: already cached: %s", url)
		return
	}

	log.Printf("Background cache: starting decode for: %s", url)

	// Decode to cache AIFF file
	_, err := decoder.DecodeToAIFFFile(url, cachePath)
	if err != nil {
		log.Printf("Background cache: failed to decode %s: %v", url, err)
		return
	}

	// Register the file with cache
	if err := p.cache.RegisterFile(url); err != nil {
		log.Printf("Background cache: warning: failed to register cache file for %s: %v", url, err)
	} else {
		log.Printf("Background cache: completed for: %s", url)
	}
}

// fetchDecodeAndCache fetches and decodes audio directly to an AIFF file in the cache
// Returns the AIFF file path
func (p *Player) fetchDecodeAndCache(track *playlist.Track) (string, error) {
	// Get cache path for this track
	cachePath := p.cache.GetPathForKey(track.URL)

	// Check if already cached
	if _, err := os.Stat(cachePath); err == nil {
		log.Printf("Using cached AIFF file: %s", cachePath)
		return cachePath, nil
	}

	// Decode directly to cache AIFF file
	log.Printf("Decoding to cache: %s", track.URL)
	_, err := decoder.DecodeToAIFFFile(track.URL, cachePath)
	if err != nil {
		return "", fmt.Errorf("failed to decode to AIFF: %w", err)
	}

	log.Printf("Decoded successfully to: %s", cachePath)

	// Register the file with cache
	if err := p.cache.RegisterFile(track.URL); err != nil {
		log.Printf("Warning: failed to register cache file: %v", err)
	} else {
		log.Printf("Registered in cache: %s", track.URL)
	}

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

	if p.paused {
		return StatePaused
	}
	if p.playing {
		return StatePlaying
	}
	return StateStopped
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

	log.Printf("remaining %d duration: %d", remaining, duration)

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