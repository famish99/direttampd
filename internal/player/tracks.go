package player

import (
	"fmt"
	"log"

	"github.com/famish99/direttampd/internal/decoder"
	"github.com/famish99/direttampd/internal/memoryplay"
	"github.com/famish99/direttampd/internal/playlist"
)

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

// BackgroundCacheTrack is a public wrapper for background caching
// Exposes backgroundCache for MPD commands to use
func (p *Player) BackgroundCacheTrack(url string) {
	p.backgroundCache(url)
}