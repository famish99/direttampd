package playlist

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/famish99/direttampd/internal/decoder"
)

// Track represents a single audio track
type Track struct {
	URL      string
	Metadata map[string]string
}

// PlaylistEvent records a modification to the playlist
type PlaylistEvent struct {
	Version   uint32
	Operation string // "add" or "clear"
	Track     *Track // nil for clear operations
	Position  int    // Position where track was added
}

// Playlist manages a list of tracks to play
type Playlist struct {
	mu      sync.RWMutex
	tracks  []Track
	current int
	version uint32          // Increments on each playlist modification
	history []PlaylistEvent // Event log of all modifications
}

// NewPlaylist creates a new empty playlist
func NewPlaylist() *Playlist {
	return &Playlist{
		tracks:  make([]Track, 0),
		current: -1,
	}
}

// Add adds a track to the playlist with metadata extraction
func (p *Playlist) Add(url string) {
	// Extract metadata using ffprobe
	metadata, err := decoder.ProbeMetadata(url)
	if err != nil {
		log.Printf("Warning: failed to extract metadata for %s: %v", url, err)
		metadata = make(map[string]string)
	}

	// If no title in metadata, use filename as fallback
	if metadata["title"] == "" {
		title := filepath.Base(url)
		// Remove extension for cleaner display
		if ext := filepath.Ext(title); ext != "" {
			title = strings.TrimSuffix(title, ext)
		}
		metadata["title"] = title
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	position := len(p.tracks)
	track := Track{
		URL:      url,
		Metadata: metadata,
	}
	p.tracks = append(p.tracks, track)
	p.version++ // Increment version on playlist modification

	// Record the event
	trackCopy := track // Make a copy for the event log
	p.history = append(p.history, PlaylistEvent{
		Version:   p.version,
		Operation: "add",
		Track:     &trackCopy,
		Position:  position,
	})
}

// AddMultiple adds multiple URLs to the playlist
func (p *Playlist) AddMultiple(urls []string) {
	for _, url := range urls {
		p.Add(url)
	}
}

// AddAt adds a track at a specific position in the playlist
// If position is out of bounds, adds at the end
// Returns the actual position where the track was added
func (p *Playlist) AddAt(url string, position int) int {
	// Extract metadata using ffprobe
	metadata, err := decoder.ProbeMetadata(url)
	if err != nil {
		log.Printf("Warning: failed to extract metadata for %s: %v", url, err)
		metadata = make(map[string]string)
	}

	// If no title in metadata, use filename as fallback
	if metadata["title"] == "" {
		title := filepath.Base(url)
		// Remove extension for cleaner display
		if ext := filepath.Ext(title); ext != "" {
			title = strings.TrimSuffix(title, ext)
		}
		metadata["title"] = title
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Clamp position to valid range [0, len(tracks)]
	if position < 0 {
		position = 0
	}
	if position > len(p.tracks) {
		position = len(p.tracks)
	}

	track := Track{
		URL:      url,
		Metadata: metadata,
	}

	// Insert at position
	p.tracks = append(p.tracks[:position], append([]Track{track}, p.tracks[position:]...)...)

	// Adjust current index if we inserted before or at current position
	if position <= p.current {
		p.current++
	}

	p.version++ // Increment version on playlist modification

	// Record the event
	trackCopy := track // Make a copy for the event log
	p.history = append(p.history, PlaylistEvent{
		Version:   p.version,
		Operation: "add",
		Track:     &trackCopy,
		Position:  position,
	})

	return position
}

// Clear removes all tracks
func (p *Playlist) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tracks = make([]Track, 0)
	p.current = -1
	p.version = 0
	p.history = make([]PlaylistEvent, 0)
}

// Current returns the current track
func (p *Playlist) Current() (*Track, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.current < 0 || p.current >= len(p.tracks) {
		return nil, fmt.Errorf("no current track")
	}

	return &p.tracks[p.current], nil
}

// Next moves to the next track and returns it
func (p *Playlist) Next() (*Track, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.tracks) == 0 {
		return nil, fmt.Errorf("playlist is empty")
	}

	p.current++
	if p.current >= len(p.tracks) {
		return nil, fmt.Errorf("end of playlist")
	}

	return &p.tracks[p.current], nil
}

// Previous moves to the previous track and returns it
func (p *Playlist) Previous() (*Track, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.tracks) == 0 {
		return nil, fmt.Errorf("playlist is empty")
	}

	p.current--
	if p.current < 0 {
		p.current = 0
		return nil, fmt.Errorf("beginning of playlist")
	}

	return &p.tracks[p.current], nil
}

// Seek moves to a specific track index
func (p *Playlist) Seek(index int) (*Track, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if index < 0 || index >= len(p.tracks) {
		return nil, fmt.Errorf("invalid track index: %d", index)
	}

	p.current = index
	return &p.tracks[p.current], nil
}

// Length returns the number of tracks
func (p *Playlist) Length() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.tracks)
}

// GetAll returns all tracks
func (p *Playlist) GetAll() []Track {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Return a copy to prevent external modification
	tracks := make([]Track, len(p.tracks))
	copy(tracks, p.tracks)
	return tracks
}

// HasNext returns true if there are more tracks after current
func (p *Playlist) HasNext() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current+1 < len(p.tracks)
}

// CurrentIndex returns the current track index
func (p *Playlist) CurrentIndex() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// GetVersion returns the current playlist version
func (p *Playlist) GetVersion() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.version
}

// GetChangesSince returns all playlist events since the given version
func (p *Playlist) GetChangesSince(version uint32) []PlaylistEvent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var changes []PlaylistEvent
	for _, event := range p.history {
		if event.Version > version {
			changes = append(changes, event)
		}
	}
	return changes
}
