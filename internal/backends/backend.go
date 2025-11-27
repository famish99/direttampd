package backends

import "github.com/famish99/direttampd/internal/playlist"

// PlaybackBackend defines the interface that different audio backends must implement
type PlaybackBackend interface {
	// Connection lifecycle
	Close()

	// Track preparation and playback
	PrepareTrack(track *playlist.Track) error // Prepare/upload/queue a track
	StartPlayback() error                     // Start playing prepared track(s)

	// Playback control
	Play() error  // Resume playback
	Pause() error // Pause playback
	Stop() error  // Quit current session

	// Playback state queries
	GetTrackDuration() (int64, error) // Returns total duration in seconds
	GetElapsedTime() (int64, error)   // Returns elapsed time in seconds
	IsTrackComplete() (bool, error)   // Returns true if track has finished

	// Target/device selection
	SelectTarget() error

	// Backend information
	GetBackendName() string
	GetOutputName() string // Returns the name of the output device
}

// BackendFactory creates a new backend instance
type BackendFactory func() (PlaybackBackend, error)
