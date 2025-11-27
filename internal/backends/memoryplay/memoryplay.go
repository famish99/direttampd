package memoryplay

import (
	"fmt"
	"log"
	"strings"

	"github.com/famish99/direttampd/internal/cache"
	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/decoder"
	"github.com/famish99/direttampd/internal/memoryplay"
	"github.com/famish99/direttampd/internal/playlist"
)

// Backend implements the backends.PlaybackBackend interface using the MemoryPlay protocol
type Backend struct {
	client               *memoryplay.Client
	cache                *cache.DiskCache
	config               *config.Config
	hostIP               string
	hostIfNum            uint32
	targetIP             string
	targetPort           string
	targetIf             uint32
	targetName           string
	useNative            bool
	currentTrackDuration int64 // Duration in seconds
}

// New creates a new MemoryPlay backend with discovery
func New(
	cache *cache.DiskCache,
	cfg *config.Config,
	useNative bool,
) (*Backend, error) {
	// Initialize the MemoryPlay C library
	if err := memoryplay.InitLibrary(true, false); err != nil {
		return nil, fmt.Errorf("failed to initialize MemoryPlay library: %w", err)
	}

	// Perform host discovery
	selectedHost, err := DiscoverAndSelectHost(cfg)
	if err != nil {
		memoryplay.CleanupLibrary()
		return nil, fmt.Errorf("host discovery failed: %w", err)
	}
	hostIP := selectedHost.IPAddress
	hostIfNum := selectedHost.InterfaceNumber

	// Check if we need to discover target or can use config
	preferredTarget := cfg.GetPreferredTarget()
	needTargetDiscovery := preferredTarget == nil ||
		preferredTarget.IP == "" ||
		preferredTarget.Port == "" ||
		preferredTarget.Interface == ""

	var targetIP, targetPort, targetName string
	var targetIf uint32

	if needTargetDiscovery {
		// Perform target discovery
		selectedTarget, err := DiscoverAndSelectTarget(hostIP, hostIfNum, cfg)
		if err != nil {
			memoryplay.CleanupLibrary()
			return nil, fmt.Errorf("target discovery failed: %w", err)
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
		_, err := fmt.Sscanf(preferredTarget.Interface, "%d", &ifNum)
		if err != nil {
			return nil, err
		}
		targetIf = ifNum

		log.Printf("Using configured target: %s (IP: %s,%s%%%d)",
			targetName, targetIP, targetPort, targetIf)
	}

	return &Backend{
		cache:      cache,
		config:     cfg,
		hostIP:     hostIP,
		hostIfNum:  hostIfNum,
		targetIP:   targetIP,
		targetPort: targetPort,
		targetName: targetName,
		targetIf:   targetIf,
		useNative:  useNative,
	}, nil
}

// Connect establishes the session to the MemoryPlay host
func (b *Backend) Connect() error {
	// Lazily create the client on first connect
	// This ensures upload happens before client connection is established
	if b.client == nil {
		log.Printf("Creating MemoryPlay client for target: %s (native: %v)", b.targetName, b.useNative)
	}

	if b.client != nil {
		if err := b.client.Connect(); err != nil {
			return fmt.Errorf("failed to connect to MemoryPlay: %w", err)
		}
		log.Printf("Connected to MemoryPlay session")
	}

	return nil
}

// Disconnect closes the MemoryPlay connection
func (b *Backend) Disconnect() error {
	if b.client != nil {
		return b.client.Disconnect()
	}
	return nil
}

// Close cleans up the backend resources
func (b *Backend) Close() {
	log.Printf("Cleaning up MemoryPlay backend")
	memoryplay.CleanupLibrary()
}

// PrepareTrack fetches, decodes, and uploads a track for playback
func (b *Backend) PrepareTrack(track *playlist.Track) error {
	log.Printf("Preparing track: %s", track.URL)

	// Fetch and decode to cached WAV file
	wavPath, err := b.fetchDecodeAndCache(track)
	if err != nil {
		return fmt.Errorf("failed to fetch and decode: %w", err)
	}

	log.Printf("Using WAV file: %s", wavPath)

	// Open WAV file with C library
	wavFile, err := memoryplay.OpenWavFile(wavPath)
	if err != nil {
		// Invalidate cache - file may be corrupt
		if invalidateErr := b.cache.Invalidate(track.URL); invalidateErr != nil {
			log.Printf("Warning: failed to invalidate cache: %v", invalidateErr)
		}
		return fmt.Errorf("failed to open WAV file: %w", err)
	}
	defer wavFile.Close()

	// Get format handle from WAV file
	formatHandle, err := wavFile.GetFormat()
	if err != nil {
		// Invalidate cache - file may be corrupt
		if invalidateErr := b.cache.Invalidate(track.URL); invalidateErr != nil {
			log.Printf("Warning: failed to invalidate cache: %v", invalidateErr)
		}
		return fmt.Errorf("failed to get format: %w", err)
	}
	defer memoryplay.FreeFormat(formatHandle)

	// Upload audio to MemoryPlay host
	log.Printf("Uploading audio to MemoryPlay host...")

	if err := memoryplay.UploadAudio(b.hostIP, b.hostIfNum, []*memoryplay.WavFile{wavFile}, formatHandle, false); err != nil {
		// Invalidate cache - file may be corrupt or incompatible
		if invalidateErr := b.cache.Invalidate(track.URL); invalidateErr != nil {
			log.Printf("Warning: failed to invalidate cache: %v", invalidateErr)
		}
		return fmt.Errorf("failed to upload audio: %w", err)
	}

	// Create client if not already created
	if b.client == nil {
		mpTarget := &memoryplay.Target{
			Name:      b.targetName,
			IP:        b.targetIP,
			Port:      b.targetPort,
			Interface: fmt.Sprintf("%d", b.targetIf),
		}
		b.client = memoryplay.NewClient(b.hostIP, mpTarget, b.useNative)
	}

	// Get track duration from metadata
	if durationStr, ok := track.Metadata["duration"]; ok && durationStr != "" {
		// Parse duration as float and convert to integer seconds
		var durationSec float64
		if _, err := fmt.Sscanf(durationStr, "%f", &durationSec); err == nil {
			b.currentTrackDuration = int64(durationSec)
			log.Printf("Track duration: %d seconds (from metadata)", int64(durationSec))
		}
	}

	return nil
}

// StartPlayback connects the session and starts playback
func (b *Backend) StartPlayback() error {
	// Ensure session is connected
	if err := b.Connect(); err != nil {
		return fmt.Errorf("failed to connect session: %w", err)
	}

	// Connect to target to start playback (this triggers playback automatically)
	if err := b.SelectTarget(); err != nil {
		return fmt.Errorf("failed to select target: %w", err)
	}
	log.Printf("Target connected, playback started")
	return nil
}

// Play starts or resumes playback
func (b *Backend) Play() error {
	if b.client != nil {
		return b.client.Play()
	}
	return nil
}

// Pause pauses playback
func (b *Backend) Pause() error {
	if b.client != nil {
		return b.client.Pause()
	}
	return nil
}

// Quit quits the current playback session
func (b *Backend) Stop() error {
	if b.client != nil {
		return b.client.Quit()
	}
	return nil
}

// GetTrackDuration returns the total duration of the current track in seconds
func (b *Backend) GetTrackDuration() (int64, error) {
	if b.currentTrackDuration == 0 {
		return 0, fmt.Errorf("no track duration available")
	}
	return b.currentTrackDuration, nil
}

// GetElapsedTime returns the elapsed time in seconds
func (b *Backend) GetElapsedTime() (int64, error) {
	if b.client == nil {
		return -1, fmt.Errorf("no client available")
	}

	// GetCurrentTime returns remaining seconds
	remaining, err := b.client.GetCurrentTime()
	if err != nil {
		return -1, err
	}

	// If remaining is -1, track is complete or not playing
	if remaining == -1 {
		return -1, nil
	}

	// Calculate elapsed from duration - remaining
	if b.currentTrackDuration == 0 {
		return -1, fmt.Errorf("no track duration available")
	}

	elapsed := b.currentTrackDuration - remaining
	if elapsed < 0 {
		elapsed = 0
	}

	return elapsed, nil
}

// IsTrackComplete returns true if the track has finished playing
func (b *Backend) IsTrackComplete() (bool, error) {
	if b.client == nil {
		return false, fmt.Errorf("no client available")
	}

	remaining, err := b.client.GetCurrentTime()
	if err != nil {
		return false, err
	}

	// Track is complete when GetCurrentTime returns -1
	return remaining == -1, nil
}

// SelectTarget connects to the target device
func (b *Backend) SelectTarget() error {
	if b.client != nil {
		return b.client.SelectTarget()
	}
	return fmt.Errorf("no client available")
}

// GetBackendName returns the name of this backend
func (b *Backend) GetBackendName() string {
	return "MemoryPlay"
}

// GetOutputName returns the name of the output device
func (b *Backend) GetOutputName() string {
	return b.targetName
}

// fetchDecodeAndCache fetches and decodes audio directly to a WAV file in the cache
// Returns the WAV file path
func (b *Backend) fetchDecodeAndCache(track *playlist.Track) (string, error) {
	log.Printf("Fetching and decoding track: %s", track.URL)
	cachePath, err := b.cache.EnsureDecoded(track.URL, func(source, dest string) error {
		_, err := decoder.DecodeToWAVFile(source, dest)
		return err
	})
	if err != nil {
		return "", err
	}
	log.Printf("Using cached WAV file: %s", cachePath)
	return cachePath, nil
}
