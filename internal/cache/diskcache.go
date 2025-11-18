package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Entry represents a cache entry
type Entry struct {
	Key     string
	Path    string
	Size    int64
	element *list.Element
}

// DiskCache implements LRU disk-based cache with persistence across sessions
type DiskCache struct {
	mu          sync.Mutex
	cacheDir    string
	maxSize     int64
	currentSize int64

	// LRU tracking
	entries map[string]*Entry
	lru     *list.List
}

// NewDiskCache creates a new disk-based LRU cache
// On startup, it scans the cache directory and loads existing cached files
func NewDiskCache(cacheDir string, maxSizeBytes int64) (*DiskCache, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	c := &DiskCache{
		cacheDir: cacheDir,
		maxSize:  maxSizeBytes,
		entries:  make(map[string]*Entry),
		lru:      list.New(),
	}

	// Load existing cache entries from disk (persistence across sessions)
	if err := c.scan(); err != nil {
		return nil, fmt.Errorf("failed to scan cache: %w", err)
	}

	return c, nil
}

// scan loads existing cache entries from disk
func (c *DiskCache) scan() error {
	return filepath.Walk(c.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		// Skip temporary files
		if filepath.Ext(path) == ".tmp" {
			return nil
		}

		// Use filename as key
		key := filepath.Base(path)

		entry := &Entry{
			Key:  key,
			Path: path,
			Size: info.Size(),
		}
		entry.element = c.lru.PushBack(entry)
		c.entries[key] = entry
		c.currentSize += info.Size()

		return nil
	})
}

// hashKey creates a consistent hash for a key
func (c *DiskCache) hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// keyToPath converts a cache key to filesystem path
func (c *DiskCache) keyToPath(key string) string {
	hash := c.hashKey(key)
	return filepath.Join(c.cacheDir, hash)
}

// GetPathForKey returns the filesystem path for a cache key
// This allows external code to write directly to the cache location
func (c *DiskCache) GetPathForKey(key string) string {
	return c.keyToPath(key)
}

// RegisterFile registers an existing file at the cache path into the cache
// Should be called after writing a file to the path returned by GetPathForKey
func (c *DiskCache) RegisterFile(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := c.hashKey(key)

	// Check if already exists
	if entry, exists := c.entries[hash]; exists {
		c.lru.MoveToFront(entry.element)
		return nil
	}

	path := c.keyToPath(key)

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat cache file: %w", err)
	}

	fileSize := info.Size()

	// Evict until there's space
	for c.currentSize+fileSize > c.maxSize && c.lru.Len() > 0 {
		c.evictOldest()
	}

	// Add to cache
	entry := &Entry{
		Key:  hash,
		Path: path,
		Size: fileSize,
	}
	entry.element = c.lru.PushFront(entry)
	c.entries[hash] = entry
	c.currentSize += fileSize

	return nil
}

// Get retrieves a cache entry reader with format information
func (c *DiskCache) Get(key string) (*CachedAudioReader, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Look up by hashed key
	hash := c.hashKey(key)
	entry, exists := c.entries[hash]
	if !exists {
		return nil, false
	}

	// Move to front (most recently used)
	c.lru.MoveToFront(entry.element)

	// Open file for reading
	f, err := os.Open(entry.Path)
	if err != nil {
		// File disappeared, remove from cache
		delete(c.entries, hash)
		c.lru.Remove(entry.element)
		c.currentSize -= entry.Size
		return nil, false
	}

	// Read format header
	format, err := ReadCacheHeader(f)
	if err != nil {
		f.Close()
		// Invalid cache file, remove it
		delete(c.entries, hash)
		c.lru.Remove(entry.element)
		c.currentSize -= entry.Size
		os.Remove(entry.Path)
		return nil, false
	}

	// Return reader with format info
	return &CachedAudioReader{
		Format: format,
		Reader: f,
	}, true
}

// Put adds data to cache with format information
func (c *DiskCache) Put(key string, format *CachedAudioFormat, reader io.Reader) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := c.hashKey(key)

	// Check if already exists
	if entry, exists := c.entries[hash]; exists {
		c.lru.MoveToFront(entry.element)
		return nil
	}

	// Create temp file
	path := c.keyToPath(key)
	tempPath := path + ".tmp"

	f, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}

	// Write format header first
	if err := WriteCacheHeader(f, format); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to write cache header: %w", err)
	}

	// Copy audio data and track size
	dataSize, err := io.Copy(f, reader)
	f.Close()

	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Total size includes header
	totalSize := int64(cacheHeaderSize) + dataSize

	// Evict until there's space
	for c.currentSize+totalSize > c.maxSize && c.lru.Len() > 0 {
		c.evictOldest()
	}

	// Rename temp file to final path (atomic)
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to finalize cache file: %w", err)
	}

	// Add to cache
	entry := &Entry{
		Key:  hash,
		Path: path,
		Size: totalSize,
	}
	entry.element = c.lru.PushFront(entry)
	c.entries[hash] = entry
	c.currentSize += totalSize

	return nil
}

// evictOldest removes the least recently used entry
func (c *DiskCache) evictOldest() {
	element := c.lru.Back()
	if element == nil {
		return
	}

	entry := element.Value.(*Entry)
	c.lru.Remove(element)
	delete(c.entries, entry.Key)
	c.currentSize -= entry.Size

	os.Remove(entry.Path)
}

// Clear removes all cache entries
func (c *DiskCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*Entry)
	c.lru = list.New()
	c.currentSize = 0

	return os.RemoveAll(c.cacheDir)
}

// Size returns current cache size in bytes
func (c *DiskCache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentSize
}
