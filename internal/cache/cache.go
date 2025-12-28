package cache

import (
	"container/list"
	"encoding/gob"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// cacheNode wraps a cache entry with metadata for LRU tracking.
type cacheNode struct {
	key   string
	entry CacheEntry
	size  int64 // Body size for this entry
}

// CacheEntry represents a single cached HTTP response.
type CacheEntry struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Expiry     time.Time
}

// CacheStats holds statistics about the cache's performance.
type CacheStats struct {
	Hits          uint64
	Misses        uint64
	EntryCount    int
	TotalSize     int64
	UptimeSeconds float64
}

// MemoryCache is a thread-safe in-memory cache for HTTP responses with LRU eviction.
type MemoryCache struct {
	mu          sync.RWMutex
	items       map[string]*list.Element // Maps key -> list element
	lruList     *list.List               // Doubly-linked list for LRU order (head=recent, tail=old)
	currentSize int64                    // Total size of all cached bodies in bytes
	maxSize     int64                    // Maximum cache size in bytes (0 = unlimited)
	defaultTTL  time.Duration
	startTime   time.Time
	hits        atomic.Uint64
	misses      atomic.Uint64
	evictions   atomic.Uint64 // Number of LRU evictions
	stopCleanup chan struct{}  // Signal to stop background cleanup goroutine
}

// NewMemoryCache creates a new MemoryCache with a default TTL and maximum size.
// maxSizeMB of 0 means unlimited (no eviction).
func NewMemoryCache(defaultTTL time.Duration, maxSizeMB int) *MemoryCache {
	c := &MemoryCache{
		items:       make(map[string]*list.Element),
		lruList:     list.New(),
		maxSize:     int64(maxSizeMB) * 1024 * 1024,
		defaultTTL:  defaultTTL,
		startTime:   time.Now(),
		stopCleanup: make(chan struct{}),
	}
	go c.cleanupExpired()
	return c
}

// Get retrieves a CacheEntry from the cache and marks it as recently used.
func (c *MemoryCache) Get(key string) (CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, found := c.items[key]
	if !found {
		c.misses.Add(1)
		return CacheEntry{}, false
	}

	node := elem.Value.(*cacheNode)

	// Check if expired
	if time.Now().After(node.entry.Expiry) {
		c.removeElement(elem)
		c.misses.Add(1)
		return CacheEntry{}, false
	}

	// Move to front (mark as recently used)
	c.lruList.MoveToFront(elem)
	c.hits.Add(1)
	return node.entry, true
}

// removeElement removes an element from both the list and map.
// Must be called with lock held.
func (c *MemoryCache) removeElement(elem *list.Element) {
	node := elem.Value.(*cacheNode)
	c.lruList.Remove(elem)
	delete(c.items, node.key)
	c.currentSize -= node.size
}

// evictLRU removes the least recently used entry from the cache.
// Returns true if an entry was evicted, false if cache was empty.
// Must be called with lock held.
func (c *MemoryCache) evictLRU() bool {
	elem := c.lruList.Back()
	if elem == nil {
		return false
	}

	c.removeElement(elem)
	c.evictions.Add(1)
	return true
}

// evictUntilSize evicts entries until currentSize + neededSize <= maxSize.
// Must be called with lock held.
func (c *MemoryCache) evictUntilSize(neededSize int64) {
	if c.maxSize == 0 {
		return // Unlimited cache
	}

	for c.currentSize+neededSize > c.maxSize {
		if !c.evictLRU() {
			break // Cache is empty
		}
	}
}

// Set adds a CacheEntry to the cache.
func (c *MemoryCache) Set(key string, entry CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry.Expiry = time.Now().Add(c.defaultTTL)
	c.items[key] = entry
	// Note: Debug logging would require logger injection - skipping for now
}

// SetWithTTL adds a CacheEntry to the cache with a custom TTL.
func (c *MemoryCache) SetWithTTL(key string, entry CacheEntry, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry.Expiry = time.Now().Add(ttl)
	c.items[key] = entry
	// Note: Debug logging would require logger injection - skipping for now
}

// delete removes an entry from the cache.
func (c *MemoryCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// GetStats returns the current statistics for the cache.
func (c *MemoryCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalSize int64
	for _, item := range c.items {
		totalSize += int64(len(item.Body))
	}

	return CacheStats{
		Hits:          c.hits.Load(),
		Misses:        c.misses.Load(),
		EntryCount:    len(c.items),
		TotalSize:     totalSize,
		UptimeSeconds: time.Since(c.startTime).Seconds(),
	}
}

// UpdateTTL updates the default TTL for new cache entries.
func (c *MemoryCache) UpdateTTL(newTTL time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultTTL = newTTL
}

// SaveToFile saves the cache to a file atomically.
func (c *MemoryCache) SaveToFile(filename string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Ensure the directory exists.
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write to a temporary file first.
	tmpFile, err := os.CreateTemp(dir, "gocache-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name()) // Clean up the temp file

	encoder := gob.NewEncoder(tmpFile)
	if err := encoder.Encode(c.items); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Atomically rename the temporary file to the final destination.
	return os.Rename(tmpFile.Name(), filename)
}

// LoadFromFile loads the cache from a file.
func (c *MemoryCache) LoadFromFile(filename string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	return decoder.Decode(&c.items)
}

// PurgeAll clears the entire cache and resets statistics.
func (c *MemoryCache) PurgeAll() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := len(c.items)
	c.items = make(map[string]CacheEntry)
	c.hits.Store(0)
	c.misses.Store(0)
	// Note: Debug logging would require logger injection - skipping for now
	return count
}

// PurgeByURL removes a single entry from the cache by its URL.
func (c *MemoryCache) PurgeByURL(rawURL string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, found := c.items[rawURL]
	if found {
		delete(c.items, rawURL)
	}
	return found
}

// PurgeByDomain removes all entries belonging to a specific domain.
func (c *MemoryCache) PurgeByDomain(domain string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	keysToDelete := []string{}
	for key := range c.items {
		u, err := url.Parse(key)
		if err != nil {
			continue
		}
		if strings.HasPrefix(u.Host, domain) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(c.items, key)
		count++
	}
	return count
}
