package cache

import (
	"encoding/binary"
	"fmt"
	"io"
)

// CachedAudioFormat represents audio format stored in cache
type CachedAudioFormat struct {
	SampleRate    uint32
	BitsPerSample uint32
	Channels      uint32
}

// Cache file format:
// - Magic bytes (4): "DPCA" (Diretta PCM Audio Cache)
// - Version (1): 0x01
// - Sample Rate (4): uint32 little-endian
// - Bits Per Sample (4): uint32 little-endian
// - Channels (4): uint32 little-endian
// - Reserved (3): padding for alignment
// - Total header: 20 bytes
// - Followed by raw PCM audio data

const (
	cacheMagic      = "DPCA"
	cacheVersion    = 0x01
	cacheHeaderSize = 20
)

// WriteCacheHeader writes the cache file header
func WriteCacheHeader(w io.Writer, format *CachedAudioFormat) error {
	// Magic bytes
	if _, err := w.Write([]byte(cacheMagic)); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Version
	if err := binary.Write(w, binary.LittleEndian, uint8(cacheVersion)); err != nil {
		return fmt.Errorf("failed to write version: %w", err)
	}

	// Sample rate
	if err := binary.Write(w, binary.LittleEndian, format.SampleRate); err != nil {
		return fmt.Errorf("failed to write sample rate: %w", err)
	}

	// Bits per sample
	if err := binary.Write(w, binary.LittleEndian, format.BitsPerSample); err != nil {
		return fmt.Errorf("failed to write bits per sample: %w", err)
	}

	// Channels
	if err := binary.Write(w, binary.LittleEndian, format.Channels); err != nil {
		return fmt.Errorf("failed to write channels: %w", err)
	}

	// Reserved padding (3 bytes)
	padding := []byte{0, 0, 0}
	if _, err := w.Write(padding); err != nil {
		return fmt.Errorf("failed to write padding: %w", err)
	}

	return nil
}

// ReadCacheHeader reads the cache file header
func ReadCacheHeader(r io.Reader) (*CachedAudioFormat, error) {
	// Read magic bytes
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, fmt.Errorf("failed to read magic: %w", err)
	}
	if string(magic) != cacheMagic {
		return nil, fmt.Errorf("invalid cache file: bad magic bytes")
	}

	// Read version
	var version uint8
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	if version != cacheVersion {
		return nil, fmt.Errorf("unsupported cache version: %d", version)
	}

	// Read format info
	format := &CachedAudioFormat{}

	if err := binary.Read(r, binary.LittleEndian, &format.SampleRate); err != nil {
		return nil, fmt.Errorf("failed to read sample rate: %w", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &format.BitsPerSample); err != nil {
		return nil, fmt.Errorf("failed to read bits per sample: %w", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &format.Channels); err != nil {
		return nil, fmt.Errorf("failed to read channels: %w", err)
	}

	// Skip reserved padding (3 bytes)
	padding := make([]byte, 3)
	if _, err := io.ReadFull(r, padding); err != nil {
		return nil, fmt.Errorf("failed to read padding: %w", err)
	}

	return format, nil
}

// CachedAudioReader wraps a reader with format information
type CachedAudioReader struct {
	Format *CachedAudioFormat
	Reader io.ReadCloser
}

// Read implements io.Reader
func (r *CachedAudioReader) Read(p []byte) (n int, err error) {
	return r.Reader.Read(p)
}

// Close implements io.Closer
func (r *CachedAudioReader) Close() error {
	return r.Reader.Close()
}
