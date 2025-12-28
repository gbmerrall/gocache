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
	Evictions     uint64  // LRU evictions due to size limit
	EntryCount    int
	TotalSize     int64   // Current total size in bytes
	MaxSize       int64   // Configured maximum size in bytes
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

// Set adds a CacheEntry to the cache with size enforcement and LRU eviction.
func (c *MemoryCache) Set(key string, entry CacheEntry) {
	c.SetWithTTL(key, entry, c.defaultTTL)
}

// SetWithTTL adds a CacheEntry to the cache with a custom TTL and size enforcement.
func (c *MemoryCache) SetWithTTL(key string, entry CacheEntry, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entrySize := int64(len(entry.Body))

	// Check if single entry exceeds max size
	if c.maxSize > 0 && entrySize > c.maxSize {
		// Entry too large - reject it
		return
	}

	// If key already exists, remove old entry first
	if elem, exists := c.items[key]; exists {
		c.removeElement(elem)
	}

	// Evict LRU entries until we have space
	c.evictUntilSize(entrySize)

	// Add new entry to front of list
	entry.Expiry = time.Now().Add(ttl)
	node := &cacheNode{
		key:   key,
		entry: entry,
		size:  entrySize,
	}
	elem := c.lruList.PushFront(node)
	c.items[key] = elem
	c.currentSize += entrySize
}

// delete removes an entry from the cache.
func (c *MemoryCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.items[key]; exists {
		c.removeElement(elem)
	}
}

// GetStats returns the current statistics for the cache.
func (c *MemoryCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Hits:          c.hits.Load(),
		Misses:        c.misses.Load(),
		Evictions:     c.evictions.Load(),
		EntryCount:    len(c.items),
		TotalSize:     c.currentSize,
		MaxSize:       c.maxSize,
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
	c.items = make(map[string]*list.Element)
	c.lruList = list.New()
	c.currentSize = 0
	c.hits.Store(0)
	c.misses.Store(0)
	c.evictions.Store(0)
	return count
}

// PurgeByURL removes a single entry from the cache by its URL.
func (c *MemoryCache) PurgeByURL(rawURL string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, found := c.items[rawURL]
	if found {
		c.removeElement(elem)
	}
	return found
}

// PurgeByDomain removes all entries belonging to a specific domain.
func (c *MemoryCache) PurgeByDomain(domain string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	elemsToDelete := []*list.Element{}

	for key, elem := range c.items {
		u, err := url.Parse(key)
		if err != nil {
			continue
		}
		if strings.HasPrefix(u.Host, domain) {
			elemsToDelete = append(elemsToDelete, elem)
		}
	}

	for _, elem := range elemsToDelete {
		c.removeElement(elem)
		count++
	}
	return count
}

// cleanupExpired runs periodically to remove expired entries.
// Runs in background goroutine started by NewMemoryCache.
func (c *MemoryCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.removeExpiredEntries()
		case <-c.stopCleanup:
			return
		}
	}
}

// removeExpiredEntries scans cache and removes expired entries.
func (c *MemoryCache) removeExpiredEntries() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	elemsToDelete := []*list.Element{}

	for _, elem := range c.items {
		node := elem.Value.(*cacheNode)
		if now.After(node.entry.Expiry) {
			elemsToDelete = append(elemsToDelete, elem)
		}
	}

	for _, elem := range elemsToDelete {
		c.removeElement(elem)
	}
}

// Shutdown gracefully stops the background cleanup goroutine.
func (c *MemoryCache) Shutdown() {
	close(c.stopCleanup)
}
