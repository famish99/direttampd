package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	// Download synchronization - prevents concurrent downloads of same URL
	downloadLocks sync.Map // map[string]*sync.Mutex
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

// Invalidate removes a cache entry both from memory and disk
// Use this when a cached file is discovered to be corrupt or invalid
func (c *DiskCache) Invalidate(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := c.hashKey(key)
	entry, exists := c.entries[hash]
	if !exists {
		// Entry not in cache, nothing to do
		return nil
	}

	// Remove from cache tracking
	delete(c.entries, hash)
	c.lru.Remove(entry.element)
	c.currentSize -= entry.Size

	// Remove file from disk
	if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}

	log.Printf("Invalidated cache entry: %s (hash: %s)", key, hash)
	return nil
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

// getDownloadLock returns a mutex for the given URL to prevent concurrent downloads
func (c *DiskCache) getDownloadLock(url string) *sync.Mutex {
	lock, _ := c.downloadLocks.LoadOrStore(url, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// fetchToTempFile downloads a remote URL to a temporary file
// Uses URL hash for consistent temp file naming to enable deduplication
// Returns the temp file path
// NOTE: Caller must hold the download lock for this URL
func (c *DiskCache) fetchToTempFile(url string) (string, error) {
	log.Printf("Starting download for: %s", url)
	// Create temp file with hash-based name for consistency
	urlHash := c.hashKey(url)
	tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("direttampd-fetch-%s.tmp", urlHash))

	// Check if file already exists (from another request or previous download)
	if _, err := os.Stat(tempPath); err == nil {
		log.Printf("Reusing existing temp file: %s", tempPath)
		return tempPath, nil
	}

	// Create the temp file
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	// Download the URL
	log.Printf("Downloading URL: %s", url)
	resp, err := http.Get(url)
	if err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tempFile.Close()
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to fetch URL: HTTP %d", resp.StatusCode)
	}

	// Copy response to temp file
	_, err = io.Copy(tempFile, resp.Body)
	tempFile.Close()
	if err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	log.Printf("Download complete: %s", tempPath)
	return tempPath, nil
}

// EnsureDecoded ensures a URL is decoded and cached
// decodeFn should decode from source path to destination path
// Returns the cached file path
func (c *DiskCache) EnsureDecoded(url string, decodeFn func(source, dest string) error) (string, error) {
	cachePath := c.GetPathForKey(url)

	// Quick check if already cached (without lock)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	// Get lock for this URL to prevent concurrent decode operations
	lock := c.getDownloadLock(url)
	lock.Lock()
	defer lock.Unlock()

	// Check again after acquiring lock (another goroutine may have completed it)
	if _, err := os.Stat(cachePath); err == nil {
		log.Printf("Using cached file: %s", cachePath)
		return cachePath, nil
	}

	// Determine source path - fetch remote URLs locally first
	sourcePath := url
	var tempFile string
	isRemote := strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")

	if isRemote {
		// Fetch remote URL to temporary file first
		log.Printf("Fetching remote URL to local file: %s", url)
		var err error
		tempFile, err = c.fetchToTempFile(url)
		if err != nil {
			return "", fmt.Errorf("failed to fetch remote URL: %w", err)
		}
		defer os.Remove(tempFile) // Clean up temp file when done
		sourcePath = tempFile
		log.Printf("Fetched to temporary file: %s", tempFile)
	}

	// Decode to cache
	log.Printf("Decoding to cache: %s", sourcePath)
	if err := decodeFn(sourcePath, cachePath); err != nil {
		return "", fmt.Errorf("failed to decode: %w", err)
	}

	log.Printf("Decoded successfully to: %s", cachePath)

	// Register the file with cache
	if err := c.RegisterFile(url); err != nil {
		log.Printf("Warning: failed to register cache file: %v", err)
	} else {
		log.Printf("Registered in cache: %s", url)
	}

	return cachePath, nil
}
