# Cache Size Enforcement with LRU Eviction

**Date:** 2025-12-29
**Status:** Approved

## Problem Statement

The cache has a `max_size_mb` configuration option that exists but is not enforced. The current implementation uses an unbounded `map[string]CacheEntry` with no eviction policy. This will cause memory exhaustion in long-running production processes.

**Current issues:**
1. `max_size_mb` exists in config but isn't used
2. Memory cache has no eviction policy
3. No tracking of current cache size
4. Long-running processes will exhaust memory

## Solution Overview

Implement an LRU (Least Recently Used) eviction policy with hybrid eviction strategy:
- **Synchronous eviction** during `Set()` operations to enforce hard size limit
- **Background cleanup** to remove expired entries proactively

## Design Decisions

**Eviction Strategy:** Pure LRU
- Evict least recently used entries when size limit reached
- Simple, predictable, well-understood behavior
- Tracks access order via doubly-linked list

**Size Calculation:** Body only
- Count only response body bytes (`len(entry.Body)`)
- Consistent with current `GetStats()` implementation
- Headers are typically small and add minimal overhead

**Eviction Timing:** Hybrid approach
- Check and evict during `Set()`/`SetWithTTL()` before adding new entry
- Background goroutine cleans up expired entries every 1 minute
- Prevents both unbounded growth and memory buildup from expired entries

## Architecture

### Data Structures

```go
type cacheNode struct {
    key   string
    entry CacheEntry
    size  int64  // Body size for this entry
}

type MemoryCache struct {
    mu         sync.RWMutex
    items      map[string]*list.Element  // Maps key -> list element
    lruList    *list.List                // Doubly-linked list for LRU order
    currentSize int64                     // Total size of all cached bodies
    maxSize     int64                     // Maximum cache size in bytes
    defaultTTL  time.Duration
    startTime   time.Time
    hits        atomic.Uint64
    misses      atomic.Uint64
    evictions   atomic.Uint64             // NEW: Track eviction count
    stopCleanup chan struct{}             // NEW: Signal to stop background goroutine
}
```

**LRU List Order:** Head = most recently used, Tail = least recently used

### Operations

#### Get(key) - O(1)
1. Lock for write (need to modify list)
2. Check if key exists in map
3. If found and not expired:
   - Move element to front of LRU list
   - Increment hits
   - Return entry
4. If expired or not found:
   - Remove if expired
   - Increment misses
   - Return not found

#### Set(key, entry) / SetWithTTL(key, entry, ttl) - O(1) + O(k evictions)
1. Calculate new entry size: `len(entry.Body)`
2. Check if single entry exceeds maxSize:
   - If yes: log warning, reject entry
3. Lock for write
4. If key already exists: remove old entry, update currentSize
5. While `currentSize + newEntrySize > maxSize`:
   - Call `evictLRU()` to remove tail entries
6. Add new entry to front of list and map
7. Update currentSize

#### evictLRU() - Internal helper
1. Remove tail element from list (least recently used)
2. Delete from map
3. Decrement currentSize by entry size
4. Increment evictions counter
5. Log at DEBUG level

#### cleanupExpired() - Background goroutine
1. Run every 1 minute
2. Lock for write
3. Iterate through all entries
4. Remove expired entries (even if not accessed)
5. Update currentSize
6. Check for stop signal

### Statistics

Enhanced `CacheStats`:
```go
type CacheStats struct {
    Hits          uint64
    Misses        uint64
    Evictions     uint64  // NEW: LRU evictions due to size
    EntryCount    int
    TotalSize     int64
    MaxSize       int64   // NEW: Configured limit
    UptimeSeconds float64
}
```

### Constructor Changes

```go
// OLD: NewMemoryCache(defaultTTL time.Duration)
// NEW: NewMemoryCache(defaultTTL time.Duration, maxSizeMB int)

func NewMemoryCache(defaultTTL time.Duration, maxSizeMB int) *MemoryCache {
    c := &MemoryCache{
        items:       make(map[string]*list.Element),
        lruList:     list.New(),
        maxSize:     int64(maxSizeMB) * 1024 * 1024,
        defaultTTL:  defaultTTL,
        startTime:   time.Now(),
        stopCleanup: make(chan struct{}),
    }
    go c.cleanupExpired() // Start background cleanup
    return c
}
```

## Integration Points

### Files to Modify

**internal/cache/cache.go:**
- Modify `MemoryCache` struct
- Update `Get()`, `Set()`, `SetWithTTL()`
- Update `delete()`, `LoadFromFile()`, `PurgeAll()`, `PurgeByURL()`, `PurgeByDomain()`
- Add `evictLRU()`, `evictUntilSize()`, `cleanupExpired()`, `Shutdown()`
- Update `GetStats()`

**internal/proxy/proxy.go:**
- Update `NewProxy()` to pass `cfg.Cache.MaxSizeMB` to cache constructor
- Call `cache.Shutdown()` in proxy `Close()` method

**cmd/gocache/main.go:**
- Update cache initialization to pass maxSize from config

**internal/cache/cache_test.go:**
- Add comprehensive tests for LRU behavior and size enforcement

### Backward Compatibility

- If `maxSizeMB = 0`, treat as unlimited (no eviction)
- Existing cache files load normally - rebuild LRU list on load
- No breaking changes to public API beyond constructor signature

### Logging

- **DEBUG:** Evictions, background cleanup activity
- **WARN:** Rejected oversized entries
- **INFO:** Cache initialization with size limit

## Testing Strategy

### Unit Tests

1. **Basic LRU behavior:**
   - Get() moves entries to front
   - Set() adds entries to front
   - LRU order maintained correctly

2. **Size enforcement:**
   - Fill to maxSize, verify eviction on next Set()
   - Entry larger than maxSize is rejected
   - Multiple small entries trigger correct evictions
   - currentSize tracking is accurate

3. **Eviction correctness:**
   - Least recently used entry is evicted first
   - Evictions counter increments
   - Multiple evictions work correctly

4. **Thread safety:**
   - Concurrent Get() and Set() operations
   - Run with `-race` flag
   - No data races

5. **Background cleanup:**
   - Expired entries removed automatically
   - Graceful shutdown works
   - Cleanup doesn't interfere with normal operations

6. **Edge cases:**
   - Empty cache
   - maxSize = 0 (unlimited)
   - Single entry fills entire cache
   - LoadFromFile() with existing data

## Performance Characteristics

- **Get:** O(1) map lookup + O(1) list move = O(1)
- **Set:** O(1) + O(k) where k = number of evictions (typically small)
- **Memory overhead:** ~24 bytes per entry for list.Element pointer
- **Space complexity:** O(n) where n = number of cached entries

## Migration Path

1. Update cache constructor signature
2. Update all cache initialization points
3. Add comprehensive tests
4. Deploy with monitoring on eviction metrics
5. Tune maxSize based on production metrics

## Success Criteria

✓ max_size_mb from config is enforced
✓ Cache does not grow unbounded
✓ LRU eviction works correctly
✓ Background cleanup prevents expired entry buildup
✓ Thread-safe under concurrent access
✓ All tests pass including race detector
✓ Eviction statistics available for monitoring
