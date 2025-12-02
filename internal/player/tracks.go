package player

import (
	"log"

	"github.com/famish99/direttampd/internal/decoder"
	"github.com/famish99/direttampd/internal/playlist"
)

// PlayTrack plays a single track using the backend
func (p *Player) PlayTrack(track *playlist.Track) error {
	log.Printf("Playing track: %s", track.URL)

	// Prepare the track (decode, upload)
	if err := p.backend.PrepareTrack(track); err != nil {
		return err
	}

	// Start playback
	return p.backend.StartPlayback()
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

// BackgroundCacheTrack is a public wrapper for background caching
// Exposes backgroundCache for MPD commands to use
func (p *Player) BackgroundCacheTrack(url string) {
	p.backgroundCache(url)
}
