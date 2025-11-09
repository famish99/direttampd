package playlist

import (
	"fmt"
	"sync"
)

// Track represents a single audio track
type Track struct {
	URL      string
	Title    string
	Index    int
	Metadata map[string]string
}

// Playlist manages a list of tracks to play
type Playlist struct {
	mu      sync.RWMutex
	tracks  []Track
	current int
}

// NewPlaylist creates a new empty playlist
func NewPlaylist() *Playlist {
	return &Playlist{
		tracks:  make([]Track, 0),
		current: -1,
	}
}

// Add adds a track to the playlist
func (p *Playlist) Add(url string, title string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	track := Track{
		URL:      url,
		Title:    title,
		Index:    len(p.tracks),
		Metadata: make(map[string]string),
	}
	p.tracks = append(p.tracks, track)
}

// AddMultiple adds multiple URLs to the playlist
func (p *Playlist) AddMultiple(urls []string) {
	for i, url := range urls {
		title := fmt.Sprintf("Track %d", i+1)
		p.Add(url, title)
	}
}

// Clear removes all tracks
func (p *Playlist) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tracks = make([]Track, 0)
	p.current = -1
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
