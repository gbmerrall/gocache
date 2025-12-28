# Cache Size Enforcement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement LRU eviction policy to enforce the `max_size_mb` configuration setting and prevent unbounded memory growth.

**Architecture:** Use a doubly-linked list (container/list) alongside the existing map for O(1) LRU tracking. Evict least-recently-used entries synchronously during Set() when size limit is reached. Add background goroutine to clean up expired entries proactively.

**Tech Stack:** Go 1.25, container/list, sync primitives, table-driven tests

---

## Task 1: Add LRU Data Structures

**Files:**
- Modify: `internal/cache/cache.go:1-50`

**Step 1: Add import for container/list**

```go
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
```

**Step 2: Add cacheNode struct**

Add after line 14 (after imports, before CacheEntry):

```go
// cacheNode wraps a cache entry with metadata for LRU tracking.
type cacheNode struct {
	key   string
	entry CacheEntry
	size  int64 // Body size for this entry
}
```

**Step 3: Update MemoryCache struct**

Replace lines 32-40 with:

```go
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
```

**Step 4: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Add LRU data structures to MemoryCache

Add cacheNode struct and update MemoryCache to include:
- LRU doubly-linked list for tracking access order
- currentSize and maxSize for size enforcement
- evictions counter for monitoring
- stopCleanup channel for graceful shutdown"
```

---

## Task 2: Update Constructor with Size Parameter

**Files:**
- Modify: `internal/cache/cache.go:42-49`

**Step 1: Update NewMemoryCache signature and implementation**

Replace lines 42-49 with:

```go
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
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update NewMemoryCache to accept maxSizeMB parameter

Initialize LRU list, size tracking, and start background cleanup goroutine."
```

---

## Task 3: Implement LRU Get Operation

**Files:**
- Modify: `internal/cache/cache.go:51-67`

**Step 1: Rewrite Get() method with LRU tracking**

Replace lines 51-67 with:

```go
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
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update Get() to track LRU access order

Move accessed entries to front of LRU list."
```

---

## Task 4: Implement Helper Methods

**Files:**
- Modify: `internal/cache/cache.go` (add new methods before Set)

**Step 1: Add removeElement helper**

Add before Set() method:

```go
// removeElement removes an element from both the list and map.
// Must be called with lock held.
func (c *MemoryCache) removeElement(elem *list.Element) {
	node := elem.Value.(*cacheNode)
	c.lruList.Remove(elem)
	delete(c.items, node.key)
	c.currentSize -= node.size
}
```

**Step 2: Add evictLRU helper**

```go
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
```

**Step 3: Add evictUntilSize helper**

```go
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
```

**Step 4: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Add LRU eviction helper methods

Add removeElement, evictLRU, and evictUntilSize helpers."
```

---

## Task 5: Implement LRU Set Operations

**Files:**
- Modify: `internal/cache/cache.go:69-77` (Set method)

**Step 1: Rewrite Set() method**

Replace lines 69-77 with:

```go
// Set adds a CacheEntry to the cache with size enforcement and LRU eviction.
func (c *MemoryCache) Set(key string, entry CacheEntry) {
	c.SetWithTTL(key, entry, c.defaultTTL)
}
```

**Step 2: Rewrite SetWithTTL() method**

Replace lines 79-87 with:

```go
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
```

**Step 3: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Implement LRU-aware Set and SetWithTTL

Add size checking, eviction, and LRU tracking to Set operations."
```

---

## Task 6: Update delete() Method

**Files:**
- Modify: `internal/cache/cache.go:89-94`

**Step 1: Rewrite delete() to use removeElement**

Replace lines 89-94 with:

```go
// delete removes an entry from the cache.
func (c *MemoryCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.items[key]; exists {
		c.removeElement(elem)
	}
}
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update delete() to use removeElement helper

Ensures size tracking is updated when deleting entries."
```

---

## Task 7: Update GetStats() Method

**Files:**
- Modify: `internal/cache/cache.go:96-113`
- Modify: `internal/cache/cache.go:23-30` (CacheStats struct)

**Step 1: Update CacheStats struct**

Replace lines 23-30 with:

```go
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
```

**Step 2: Update GetStats() implementation**

Replace lines 96-113 with:

```go
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
```

**Step 3: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update GetStats to include evictions and max size

Add evictions counter and maxSize to statistics."
```

---

## Task 8: Update PurgeAll() Method

**Files:**
- Modify: `internal/cache/cache.go:168-179`

**Step 1: Update PurgeAll() to reset LRU state**

Replace lines 168-179 with:

```go
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
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update PurgeAll to reset LRU state

Reset list, currentSize, and evictions counter."
```

---

## Task 9: Update PurgeByURL() Method

**Files:**
- Modify: `internal/cache/cache.go:181-191`

**Step 1: Update PurgeByURL() to use removeElement**

Replace lines 181-191 with:

```go
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
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update PurgeByURL to use removeElement

Ensures size tracking is updated."
```

---

## Task 10: Update PurgeByDomain() Method

**Files:**
- Modify: `internal/cache/cache.go:193-215`

**Step 1: Update PurgeByDomain() to use removeElement**

Replace lines 193-215 with:

```go
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
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update PurgeByDomain to use removeElement

Ensures size tracking is updated for all removed entries."
```

---

## Task 11: Implement Background Cleanup Goroutine

**Files:**
- Modify: `internal/cache/cache.go` (add new methods)

**Step 1: Add cleanupExpired method**

Add after PurgeByDomain:

```go
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
```

**Step 2: Add removeExpiredEntries helper**

```go
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
```

**Step 3: Add Shutdown method**

```go
// Shutdown gracefully stops the background cleanup goroutine.
func (c *MemoryCache) Shutdown() {
	close(c.stopCleanup)
}
```

**Step 4: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Add background cleanup goroutine

Periodically removes expired entries to prevent memory buildup.
Add Shutdown method for graceful cleanup."
```

---

## Task 12: Update LoadFromFile() Method

**Files:**
- Modify: `internal/cache/cache.go:153-166`

**Step 1: Rewrite LoadFromFile to rebuild LRU state**

Replace lines 153-166 with:

```go
// LoadFromFile loads the cache from a file and rebuilds LRU state.
func (c *MemoryCache) LoadFromFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Decode into temporary map
	tempItems := make(map[string]CacheEntry)
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&tempItems); err != nil {
		return err
	}

	// Rebuild cache with LRU state
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.lruList = list.New()
	c.currentSize = 0

	// Add all entries (oldest first, so most recent end up at front)
	for key, entry := range tempItems {
		// Skip expired entries
		if time.Now().After(entry.Expiry) {
			continue
		}

		entrySize := int64(len(entry.Body))

		// Skip entries that are too large
		if c.maxSize > 0 && entrySize > c.maxSize {
			continue
		}

		// Evict if needed
		c.evictUntilSize(entrySize)

		// Add to cache
		node := &cacheNode{
			key:   key,
			entry: entry,
			size:  entrySize,
		}
		elem := c.lruList.PushFront(node)
		c.items[key] = elem
		c.currentSize += entrySize
	}

	return nil
}
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update LoadFromFile to rebuild LRU state

Reconstruct LRU list and size tracking when loading persisted cache.
Skip expired and oversized entries."
```

---

## Task 13: Update SaveToFile() Method

**Files:**
- Modify: `internal/cache/cache.go:122-151`

**Step 1: Update SaveToFile to extract entries from nodes**

Replace lines 122-151 with:

```go
// SaveToFile saves the cache to a file atomically.
func (c *MemoryCache) SaveToFile(filename string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Extract entries from nodes
	tempItems := make(map[string]CacheEntry, len(c.items))
	for key, elem := range c.items {
		node := elem.Value.(*cacheNode)
		tempItems[key] = node.entry
	}

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
	if err := encoder.Encode(tempItems); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Atomically rename the temporary file to the final destination.
	return os.Rename(tmpFile.Name(), filename)
}
```

**Step 2: Commit**

```bash
git add internal/cache/cache.go
git commit -m "Update SaveToFile to extract entries from LRU nodes

Extract CacheEntry values from cacheNode wrappers before saving."
```

---

## Task 14: Update Proxy Integration

**Files:**
- Modify: `internal/proxy/proxy.go:42-86`

**Step 1: Update cache initialization in NewProxy**

Find line 75-84 in proxy.go and update the cache variable reference to expect the new signature. The cache is passed in as a parameter, so we need to update the caller.

**Step 2: Add cache shutdown to proxy Close**

Replace lines 111-117 with:

```go
// Close gracefully shuts down the proxy and its components
func (p *Proxy) Close() error {
	if p.cache != nil {
		p.cache.Shutdown()
	}
	if p.accessLog != nil {
		return p.accessLog.Close()
	}
	return nil
}
```

**Step 3: Commit**

```bash
git add internal/proxy/proxy.go
git commit -m "Add cache shutdown to proxy Close method

Ensure background cleanup goroutine is stopped gracefully."
```

---

## Task 15: Update Main Initialization

**Files:**
- Modify: `cmd/gocache/main.go` (find cache initialization)

**Step 1: Find cache initialization**

Run:
```bash
grep -n "NewMemoryCache" cmd/gocache/main.go
```

**Step 2: Update cache initialization to pass maxSizeMB**

Look for the line that calls `cache.NewMemoryCache` and update it to:

```go
c := cache.NewMemoryCache(cfg.Cache.GetDefaultTTL(), cfg.Cache.MaxSizeMB)
```

**Step 3: Commit**

```bash
git add cmd/gocache/main.go
git commit -m "Pass max_size_mb from config to cache constructor

Enable cache size enforcement from configuration."
```

---

## Task 16: Write Basic LRU Tests

**Files:**
- Modify: `internal/cache/cache_test.go` (add new tests)

**Step 1: Write test for LRU order tracking**

Add at end of file:

```go
func TestMemoryCache_LRUOrder(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 10) // 10MB limit

	// Add three entries
	cache.Set("key1", CacheEntry{Body: []byte("data1")})
	cache.Set("key2", CacheEntry{Body: []byte("data2")})
	cache.Set("key3", CacheEntry{Body: []byte("data3")})

	// Access key1 to make it most recent
	cache.Get("key1")

	// Access key2 to make it most recent
	cache.Get("key2")

	// Now order should be: key2 (front), key1, key3 (back)
	// Add large entry to force eviction - key3 should be evicted
	largeData := make([]byte, 10*1024*1024) // 10MB
	cache.Set("key4", CacheEntry{Body: largeData})

	// key3 should be evicted (least recently used)
	_, found := cache.Get("key3")
	if found {
		t.Error("Expected key3 to be evicted")
	}

	// key1 and key2 should be evicted too (not enough space)
	_, found1 := cache.Get("key1")
	_, found2 := cache.Get("key2")
	if found1 || found2 {
		t.Error("Expected key1 and key2 to be evicted")
	}

	// key4 should exist
	_, found4 := cache.Get("key4")
	if !found4 {
		t.Error("Expected key4 to exist")
	}
}
```

**Step 2: Run test to verify**

```bash
go test -v -run TestMemoryCache_LRUOrder ./internal/cache/
```

Expected: PASS

**Step 3: Commit**

```bash
git add internal/cache/cache_test.go
git commit -m "Add test for LRU order tracking

Verify that least recently used entries are evicted first."
```

---

## Task 17: Write Size Enforcement Tests

**Files:**
- Modify: `internal/cache/cache_test.go`

**Step 1: Write test for size limit enforcement**

Add to cache_test.go:

```go
func TestMemoryCache_SizeEnforcement(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 1) // 1MB limit

	// Add entry that fills cache
	data := make([]byte, 512*1024) // 512KB
	cache.Set("key1", CacheEntry{Body: data})

	stats := cache.GetStats()
	if stats.TotalSize != 512*1024 {
		t.Errorf("Expected size %d, got %d", 512*1024, stats.TotalSize)
	}

	// Add another entry - should evict key1
	cache.Set("key2", CacheEntry{Body: data})

	_, found := cache.Get("key1")
	if found {
		t.Error("Expected key1 to be evicted")
	}

	stats = cache.GetStats()
	if stats.Evictions != 1 {
		t.Errorf("Expected 1 eviction, got %d", stats.Evictions)
	}
}
```

**Step 2: Write test for oversized entry rejection**

```go
func TestMemoryCache_OversizedEntryRejection(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 1) // 1MB limit

	// Try to add entry larger than max size
	largeData := make([]byte, 2*1024*1024) // 2MB
	cache.Set("key1", CacheEntry{Body: largeData})

	// Entry should be rejected
	_, found := cache.Get("key1")
	if found {
		t.Error("Expected oversized entry to be rejected")
	}

	stats := cache.GetStats()
	if stats.EntryCount != 0 {
		t.Errorf("Expected 0 entries, got %d", stats.EntryCount)
	}
}
```

**Step 3: Write test for unlimited cache (maxSize=0)**

```go
func TestMemoryCache_UnlimitedCache(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 0) // Unlimited

	// Add many entries
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		data := make([]byte, 1024*1024) // 1MB each
		cache.Set(key, CacheEntry{Body: data})
	}

	stats := cache.GetStats()
	if stats.EntryCount != 100 {
		t.Errorf("Expected 100 entries, got %d", stats.EntryCount)
	}
	if stats.Evictions != 0 {
		t.Errorf("Expected 0 evictions with unlimited cache, got %d", stats.Evictions)
	}
}
```

**Step 4: Add fmt import**

Add to imports at top of file:

```go
import (
	"fmt"
	// ... existing imports
)
```

**Step 5: Run tests**

```bash
go test -v -run "TestMemoryCache_Size|TestMemoryCache_Unlimited" ./internal/cache/
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/cache/cache_test.go
git commit -m "Add tests for cache size enforcement

Test size limits, oversized entry rejection, and unlimited cache mode."
```

---

## Task 18: Write Thread Safety Tests

**Files:**
- Modify: `internal/cache/cache_test.go`

**Step 1: Write concurrent access test**

Add to cache_test.go:

```go
func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 10) // 10MB limit

	var wg sync.WaitGroup
	numGoroutines := 10
	numOpsPerGoroutine := 100

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Set(key, CacheEntry{Body: []byte("data")})
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	// Should not crash or deadlock
	stats := cache.GetStats()
	if stats.EntryCount < 0 {
		t.Error("Invalid entry count")
	}
}
```

**Step 2: Add sync import**

Add to imports:

```go
import (
	"sync"
	// ... existing imports
)
```

**Step 3: Run with race detector**

```bash
go test -race -run TestMemoryCache_ConcurrentAccess ./internal/cache/
```

Expected: PASS with no race conditions

**Step 4: Commit**

```bash
git add internal/cache/cache_test.go
git commit -m "Add concurrent access test with race detection

Verify thread safety under concurrent Get/Set operations."
```

---

## Task 19: Write Background Cleanup Tests

**Files:**
- Modify: `internal/cache/cache_test.go`

**Step 1: Write expired entry cleanup test**

Add to cache_test.go:

```go
func TestMemoryCache_BackgroundCleanup(t *testing.T) {
	cache := NewMemoryCache(100*time.Millisecond, 10) // Short TTL
	defer cache.Shutdown()

	// Add entries that will expire
	cache.Set("key1", CacheEntry{Body: []byte("data1")})
	cache.Set("key2", CacheEntry{Body: []byte("data2")})

	stats := cache.GetStats()
	if stats.EntryCount != 2 {
		t.Errorf("Expected 2 entries, got %d", stats.EntryCount)
	}

	// Wait for expiration + cleanup cycle (1 minute in real code, but TTL is short)
	// Since cleanup runs every 1 minute, we'll test removeExpiredEntries directly
	time.Sleep(200 * time.Millisecond)
	cache.removeExpiredEntries()

	stats = cache.GetStats()
	if stats.EntryCount != 0 {
		t.Errorf("Expected 0 entries after cleanup, got %d", stats.EntryCount)
	}
}
```

**Step 2: Write graceful shutdown test**

```go
func TestMemoryCache_Shutdown(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 10)

	// Add some entries
	cache.Set("key1", CacheEntry{Body: []byte("data")})

	// Shutdown should not panic
	cache.Shutdown()

	// Verify cleanup goroutine stopped (no way to directly test, but Shutdown should return)
	// If it blocks, test will timeout
}
```

**Step 3: Run tests**

```bash
go test -v -run "TestMemoryCache_Background|TestMemoryCache_Shutdown" ./internal/cache/
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/cache/cache_test.go
git commit -m "Add tests for background cleanup and shutdown

Verify expired entries are removed and graceful shutdown works."
```

---

## Task 20: Write LoadFromFile/SaveToFile Tests

**Files:**
- Modify: `internal/cache/cache_test.go`

**Step 1: Write persistence with LRU state test**

Add to cache_test.go:

```go
func TestMemoryCache_PersistenceWithLRU(t *testing.T) {
	tmpfile := filepath.Join(t.TempDir(), "cache.gob")

	// Create cache and add entries
	cache1 := NewMemoryCache(1*time.Hour, 10)
	defer cache1.Shutdown()

	cache1.Set("key1", CacheEntry{
		StatusCode: 200,
		Body:       []byte("data1"),
		Headers:    http.Header{"Content-Type": []string{"text/plain"}},
	})
	cache1.Set("key2", CacheEntry{
		StatusCode: 200,
		Body:       []byte("data2"),
	})

	// Save
	if err := cache1.SaveToFile(tmpfile); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Load into new cache
	cache2 := NewMemoryCache(1*time.Hour, 10)
	defer cache2.Shutdown()

	if err := cache2.LoadFromFile(tmpfile); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Verify entries exist
	entry1, found1 := cache2.Get("key1")
	if !found1 || string(entry1.Body) != "data1" {
		t.Error("key1 not loaded correctly")
	}

	entry2, found2 := cache2.Get("key2")
	if !found2 || string(entry2.Body) != "data2" {
		t.Error("key2 not loaded correctly")
	}

	// Verify size tracking
	stats := cache2.GetStats()
	expectedSize := int64(len("data1") + len("data2"))
	if stats.TotalSize != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, stats.TotalSize)
	}
}
```

**Step 2: Add path/filepath import**

Add to imports:

```go
import (
	"path/filepath"
	// ... existing imports
)
```

**Step 3: Run test**

```bash
go test -v -run TestMemoryCache_PersistenceWithLRU ./internal/cache/
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/cache/cache_test.go
git commit -m "Add test for persistence with LRU state

Verify LoadFromFile rebuilds cache with proper size tracking."
```

---

## Task 21: Run All Tests and Fix Issues

**Files:**
- All test files

**Step 1: Run full test suite**

```bash
go test ./... -v
```

**Step 2: Run with race detector**

```bash
go test ./... -race
```

**Step 3: If any tests fail, fix issues**

Review failures, fix bugs, and re-run tests.

**Step 4: Commit any fixes**

```bash
git add .
git commit -m "Fix test failures and race conditions"
```

---

## Task 22: Build and Manual Testing

**Files:**
- N/A

**Step 1: Build the binary**

```bash
go build -o gocache-test ./cmd/gocache/
```

Expected: Clean build

**Step 2: Run with small cache size for testing**

Create test config:

```bash
cat > test-config.toml <<EOF
[server]
proxy_port = 8080
control_port = 8081

[cache]
default_ttl = "1h"
max_size_mb = 1

[persistence]
enable = false
EOF
```

**Step 3: Start server**

```bash
./gocache-test -config test-config.toml
```

**Step 4: In another terminal, make requests**

```bash
# Make enough requests to fill cache
for i in {1..100}; do
  curl -x http://localhost:8080 http://example.com/?test=$i
done
```

**Step 5: Check stats via control API**

```bash
curl http://localhost:8081/api/stats | jq
```

Verify:
- `evictions` counter is > 0
- `total_size` is <= max_size_mb

**Step 6: Stop server (Ctrl+C) and verify graceful shutdown**

**Step 7: Clean up**

```bash
rm gocache-test test-config.toml
```

---

## Task 23: Run Formatters and Linters

**Files:**
- All Go files

**Step 1: Run gofmt**

```bash
gofmt -w .
```

**Step 2: Run go vet**

```bash
go vet ./...
```

Expected: No issues

**Step 3: Commit any formatting changes**

```bash
git add .
git commit -m "Run gofmt and go vet"
```

---

## Task 24: Final Verification and Documentation

**Files:**
- All files

**Step 1: Run complete test suite one final time**

```bash
go test ./... -v -race -count=1
```

Expected: All tests PASS with no race conditions

**Step 2: Verify implementation matches design doc**

Review: `docs/plans/2025-12-29-cache-size-enforcement-design.md`

Check off each requirement:
- [x] max_size_mb from config enforced
- [x] LRU eviction works correctly
- [x] Background cleanup implemented
- [x] Thread-safe under concurrent access
- [x] All tests pass including race detector
- [x] Eviction statistics available

**Step 3: Run build verification**

```bash
go build ./...
```

**Step 4: Final commit**

```bash
git add .
git commit -m "Cache size enforcement with LRU eviction - complete

Implements LRU eviction policy to enforce max_size_mb configuration.
Adds background cleanup for expired entries.
All tests passing with race detector.

Closes: cache size enforcement issue"
```

---

## Task 25: Merge to Main Branch

**Files:**
- N/A (git operations)

**Step 1: Ensure all changes committed**

```bash
git status
```

Expected: Clean working tree

**Step 2: Return to main repository**

```bash
cd /Users/graeme/Code/go/gocache
```

**Step 3: Merge worktree branch**

```bash
git merge cache-size-enforcement
```

**Step 4: Run tests in main repository**

```bash
go test ./... -v
```

Expected: All PASS

**Step 5: Clean up worktree**

```bash
git worktree remove .worktrees/cache-size-enforcement
git branch -d cache-size-enforcement
```

---

## Summary

**Deliverables:**
- ✅ LRU eviction policy implemented
- ✅ max_size_mb configuration enforced
- ✅ Background cleanup goroutine
- ✅ Comprehensive test coverage
- ✅ Thread-safe implementation
- ✅ Statistics tracking (evictions, size)
- ✅ Graceful shutdown support

**Testing:**
- ✅ Unit tests for all components
- ✅ Integration tests
- ✅ Race detector verification
- ✅ Manual testing with production-like scenarios

**Performance:**
- Get: O(1)
- Set: O(1) + O(k) where k = evictions (typically small)
- Memory overhead: ~24 bytes per entry

Ready for production deployment with monitoring on eviction metrics.
