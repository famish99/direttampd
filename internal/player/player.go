package player

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"

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

	// Playback state
	playing bool
	stopped bool
}

// AudioFormat represents audio format information
type AudioFormat struct {
	SampleRate    uint32
	BitsPerSample uint32
	Channels      uint32
}

// NewPlayer creates a new player instance
func NewPlayer(cfg *config.Config) (*Player, error) {
	// Create cache
	cacheSize := int64(cfg.Cache.MaxSizeGB) * 1024 * 1024 * 1024
	c, err := cache.NewDiskCache(cfg.Cache.Directory, cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	return &Player{
		config:  cfg,
		cache:   c,
		pl:      playlist.NewPlaylist(),
		playing: false,
		stopped: false,
	}, nil
}

// Connect connects to the configured MemoryPlay target
func (p *Player) Connect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	target := p.config.GetPreferredTarget()
	if target == nil {
		return fmt.Errorf("no target configured")
	}

	mpTarget := &memoryplay.Target{
		Name:      target.Name,
		IP:        target.IP,
		Port:      target.Port,
		Interface: target.Interface,
	}

	p.client = memoryplay.NewClient(mpTarget)
	if err := p.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to MemoryPlay target: %w", err)
	}

	log.Printf("Connected to MemoryPlay target: %s (%s)", target.Name, target.IP)
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

// AddURLs adds URLs to the playlist
func (p *Player) AddURLs(urls []string) {
	p.pl.AddMultiple(urls)
	log.Printf("Added %d URLs to playlist", len(urls))
}

// Play starts playback from the beginning or resumes
func (p *Player) Play() error {
	p.mu.Lock()
	if p.playing {
		p.mu.Unlock()
		return nil
	}
	p.playing = true
	p.stopped = false
	p.mu.Unlock()

	// Start playback loop in goroutine
	go p.playbackLoop()
	return nil
}

// Stop stops playback
func (p *Player) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopped = true
	p.playing = false

	if p.client != nil {
		return p.client.Pause()
	}
	return nil
}

// playbackLoop is the main playback loop
func (p *Player) playbackLoop() {
	defer func() {
		p.mu.Lock()
		p.playing = false
		p.mu.Unlock()
	}()

	// Start from first track
	track, err := p.pl.Next()
	if err != nil {
		log.Printf("No tracks to play: %v", err)
		return
	}

	for {
		// Check if stopped
		p.mu.Lock()
		if p.stopped {
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()

		log.Printf("Playing: %s (%s)", track.Title, track.URL)

		// Send track metadata
		if p.client != nil {
			p.client.SendTag(track.Index, 0, track.Title)
		}

		// Play the track
		if err := p.playTrack(track); err != nil {
			log.Printf("Error playing track: %v", err)
		}

		// Move to next track
		track, err = p.pl.Next()
		if err != nil {
			log.Printf("End of playlist")
			return
		}
	}
}

// playTrack plays a single track (always uses same streaming path)
func (p *Player) playTrack(track *playlist.Track) error {
	var audioReader io.ReadCloser
	var format *AudioFormat

	// Try cache first
	cachedData, found := p.cache.Get(track.URL)

	if found {
		log.Printf("Cache hit: %s", track.URL)
		audioReader = cachedData
		format = &AudioFormat{
			SampleRate:    cachedData.Format.SampleRate,
			BitsPerSample: cachedData.Format.BitsPerSample,
			Channels:      cachedData.Format.Channels,
		}
	} else {
		// Cache miss - fetch, decode, and cache
		log.Printf("Cache miss: %s", track.URL)
		var err error
		audioReader, format, err = p.fetchDecodeAndCache(track)
		if err != nil {
			return fmt.Errorf("failed to fetch and decode: %w", err)
		}
	}

	defer audioReader.Close()

	// Stream the audio (same path regardless of cache hit/miss)
	return p.streamAudio(audioReader, format)
}

// fetchDecodeAndCache fetches, decodes, caches audio and returns a reader
func (p *Player) fetchDecodeAndCache(track *playlist.Track) (io.ReadCloser, *AudioFormat, error) {
	// Create decoder
	dec, err := decoder.NewDecoder(track.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create decoder: %w", err)
	}
	defer dec.Close()

	decoderFormat := dec.Format()
	log.Printf("Decoded format: %dHz %dbit %dch", decoderFormat.SampleRate, decoderFormat.BitsPerSample, decoderFormat.Channels)

	format := &AudioFormat{
		SampleRate:    uint32(decoderFormat.SampleRate),
		BitsPerSample: uint32(decoderFormat.BitsPerSample),
		Channels:      uint32(decoderFormat.Channels),
	}

	// Read all decoded audio into buffer
	var audioBuffer bytes.Buffer
	if _, err := io.Copy(&audioBuffer, dec); err != nil {
		return nil, nil, fmt.Errorf("failed to decode audio: %w", err)
	}

	log.Printf("Decoded %d bytes", audioBuffer.Len())

	// Prepare cache format
	cacheFormat := &cache.CachedAudioFormat{
		SampleRate:    format.SampleRate,
		BitsPerSample: format.BitsPerSample,
		Channels:      format.Channels,
	}

	// Async cache write (don't block playback on disk I/O)
	cacheData := audioBuffer.Bytes()
	go func() {
		if err := p.cache.Put(track.URL, cacheFormat, bytes.NewReader(cacheData)); err != nil {
			log.Printf("Warning: failed to cache audio: %v", err)
		} else {
			log.Printf("Cached successfully: %s", track.URL)
		}
	}()

	// Return reader for immediate streaming
	return io.NopCloser(&audioBuffer), format, nil
}

// streamAudio streams audio to MemoryPlay
func (p *Player) streamAudio(reader io.Reader, format *AudioFormat) error {
	log.Printf("Streaming: %dHz %dbit %dch", format.SampleRate, format.BitsPerSample, format.Channels)

	// Convert to MemoryPlay format
	mpFormat := &memoryplay.FormatID{
		SampleRate:    format.SampleRate,
		BitsPerSample: format.BitsPerSample,
		Channels:      format.Channels,
		Format:        memoryplay.FormatPCM,
	}

	// Get silence buffer duration from config
	silenceSeconds := p.config.Playback.SilenceBufferSeconds
	if silenceSeconds <= 0 {
		silenceSeconds = 3 // Fallback default
	}

	// Send initial silence buffer for sync
	if p.client != nil {
		log.Printf("Sending initial sync buffer (%ds)...", silenceSeconds)
		if err := p.client.SendSilence(mpFormat, silenceSeconds); err != nil {
			return fmt.Errorf("failed to send sync buffer: %w", err)
		}

		// Send play command
		if err := p.client.Play(); err != nil {
			return fmt.Errorf("failed to send play command: %w", err)
		}
	}

	// Stream audio in 1-second chunks
	bytesPerSample := int(format.BitsPerSample) / 8
	samplesPerSec := int(format.SampleRate)
	chunkSize := bytesPerSample * samplesPerSec * int(format.Channels)

	buf := make([]byte, chunkSize)

	for {
		// Check if stopped
		p.mu.Lock()
		stopped := p.stopped
		p.mu.Unlock()
		if stopped {
			break
		}

		n, err := io.ReadFull(reader, buf)
		if n > 0 {
			// Send to MemoryPlay
			if p.client != nil {
				if err := p.client.SendAudioData(mpFormat, buf[:n]); err != nil {
					log.Printf("Error sending audio data: %v", err)
				}
			}
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading audio: %w", err)
		}
	}

	// Send final silence buffer
	if p.client != nil {
		log.Printf("Sending final sync buffer (%ds)...", silenceSeconds)
		p.client.SendSilence(mpFormat, silenceSeconds)
	}

	return nil
}

// GetPlaylist returns the current playlist
func (p *Player) GetPlaylist() *playlist.Playlist {
	return p.pl
}
