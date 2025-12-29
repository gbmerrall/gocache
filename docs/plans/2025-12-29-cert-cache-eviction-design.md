# Certificate Cache Eviction Design

## Problem Statement

The TLS certificate cache in `internal/proxy/proxy.go` grows unbounded as the proxy encounters unique hostnames. Each HTTPS connection triggers certificate generation, and certificates are never evicted from the map. This leads to memory exhaustion during web scraping or when proxying traffic to many domains.

**Current implementation:**
```go
type Proxy struct {
    certCache   map[string]*tls.Certificate  // Unbounded growth!
    certCacheMu sync.RWMutex
}
```

**Root cause:** No eviction policy - the map grows indefinitely with every unique hostname.

---

## Solution: LRU Eviction

Implement Least Recently Used (LRU) eviction with configurable entry count limit, consistent with the HTTP response cache implementation.

**Design principles:**
- **Entry-based limit** (not memory-based) - certificates are uniform ~1-2KB each
- **LRU eviction** - remove least recently accessed certificates when capacity reached
- **Thread-safe** - maintain existing concurrent access safety
- **Observable** - track eviction metrics for monitoring
- **Configurable** - allow tuning via `max_cert_cache_entries` setting

---

## Architecture

### Data Structures

**certNode - Wrapper for LRU tracking:**
```go
type certNode struct {
    host string
    cert *tls.Certificate
}
```

**Proxy modifications:**
```go
type Proxy struct {
    certCache     map[string]*list.Element  // Maps hostname -> list element
    certLRUList   *list.List                // Doubly-linked list (head=recent, tail=old)
    certCacheMu   sync.RWMutex
    certEvictions atomic.Uint64             // Eviction counter for metrics
    // ... existing fields
}
```

**Configuration:**
```go
type ServerConfig struct {
    MaxCertCacheEntries int `toml:"max_cert_cache_entries"`
    // ... existing fields
}
```

**Default value:** 1000 certificates ≈ 1-2MB memory (will add guidance comment in config)

**Special case:** `max_cert_cache_entries = 0` means unlimited (current behavior, no eviction)

---

## Implementation Changes

### 1. Eviction Logic

**evictOldestCert() - Remove LRU certificate:**
```go
// evictOldestCert removes the least recently used certificate.
// Must be called with certCacheMu write lock held.
// Returns true if eviction occurred, false if cache empty.
func (p *Proxy) evictOldestCert() bool {
    elem := p.certLRUList.Back()  // Tail = oldest
    if elem == nil {
        return false  // Empty cache
    }

    node := elem.Value.(*certNode)
    p.certLRUList.Remove(elem)
    delete(p.certCache, node.host)
    p.certEvictions.Add(1)
    return true
}
```

### 2. getCert() Modifications

**Update cache access to track LRU:**
```go
func (p *Proxy) getCert(host string) (*tls.Certificate, error) {
    // Check cache with read lock
    p.certCacheMu.RLock()
    elem, found := p.certCache[host]
    if found {
        node := elem.Value.(*certNode)
        p.certCacheMu.RUnlock()

        // Upgrade to write lock to move to front
        p.certCacheMu.Lock()
        p.certLRUList.MoveToFront(elem)
        p.certCacheMu.Unlock()

        return node.cert, nil
    }
    p.certCacheMu.RUnlock()

    // Generate certificate (existing logic)
    cert, err := generateCert(host, p.ca)
    if err != nil {
        return nil, err
    }

    // Add to cache with write lock
    p.certCacheMu.Lock()
    defer p.certCacheMu.Unlock()

    // Evict if at capacity
    maxEntries := p.config.Server.MaxCertCacheEntries
    if maxEntries > 0 && len(p.certCache) >= maxEntries {
        p.evictOldestCert()
    }

    // Add new cert to front (most recent)
    node := &certNode{host: host, cert: cert}
    elem = p.certLRUList.PushFront(node)
    p.certCache[host] = elem

    return cert, nil
}
```

**Key implementation details:**
- **Lock upgrade pattern:** Read lock for cache hit check, upgrade to write lock only to update LRU order
- **Minimized write lock contention:** Only hold write lock when modifying LRU list
- **No background cleanup:** Unlike HTTP cache, certificates don't expire - eviction only on capacity
- **Evict-before-add:** Ensure space before adding new entry

### 3. Initialization

**Update NewProxy() to initialize LRU structures:**
```go
func NewProxy(config *config.Config, cache *cache.MemoryCache) *Proxy {
    return &Proxy{
        config:      config,
        cache:       cache,
        certCache:   make(map[string]*list.Element),
        certLRUList: list.New(),
        // ... existing initialization
    }
}
```

### 4. Metrics Integration

**Add eviction count to existing metrics endpoints:**

In control API stats response:
```go
stats := map[string]interface{}{
    "cert_cache_evictions": p.certEvictions.Load(),
    "cert_cache_size":      len(p.certCache),
    // ... existing metrics
}
```

---

## Differences from HTTP Cache

| Aspect | HTTP Cache | Certificate Cache |
|--------|-----------|-------------------|
| **Eviction trigger** | Size in bytes | Entry count |
| **TTL/Expiry** | Yes (configurable) | No (certs valid indefinitely) |
| **Background cleanup** | Yes (1-minute ticker) | No (not needed) |
| **Eviction timing** | On capacity + periodic | On capacity only |
| **Lock pattern** | Single lock type | Lock upgrade (read→write) |
| **Memory tracking** | Incremental byte tracking | Simple entry count |

**Rationale for differences:**
- **Entry count limit:** Certificates are uniform size (~1-2KB), byte tracking adds complexity without benefit
- **No TTL:** Certificates remain valid, no security benefit to expiring them
- **No background cleanup:** Without TTL, no expired entries to clean up
- **Lock upgrade:** Cache hits are common, minimize write lock contention by upgrading only when needed

---

## Testing Strategy

### Unit Tests

**1. LRU eviction ordering:**
```go
func TestCertCacheLRUEviction(t *testing.T) {
    // Create proxy with max 3 certs
    // Request certs for hosts A, B, C (fills cache)
    // Request cert for host D (should evict A)
    // Verify A no longer in cache, B/C/D present
}
```

**2. Access updates recency:**
```go
func TestCertCacheAccessUpdatesLRU(t *testing.T) {
    // Fill cache with A, B, C
    // Access A (moves to front)
    // Add D (should evict B, not A)
    // Verify A/C/D present, B evicted
}
```

**3. Concurrent access safety:**
```go
func TestCertCacheConcurrentAccess(t *testing.T) {
    // Run with -race flag
    // Spawn 10 goroutines each requesting 100 certs
    // Mix of duplicate and unique hostnames
    // Verify no panics, correct eviction count
}
```

**4. Eviction metrics:**
```go
func TestCertCacheEvictionMetrics(t *testing.T) {
    // Create proxy with max 5 certs
    // Request 20 unique certs
    // Verify evictions counter = 15
    // Verify final cache size = 5
}
```

**5. Unlimited cache (max=0):**
```go
func TestCertCacheUnlimited(t *testing.T) {
    // Set max_cert_cache_entries = 0
    // Add 1000 certs
    // Verify all cached, no evictions
}
```

### Integration Tests

**Use existing proxy integration tests:**
- Add test scenario with many unique hostnames (e.g., 100+ domains)
- Trigger certificate eviction during test execution
- Verify TLS handshakes succeed even with active eviction
- Confirm no memory leaks or goroutine leaks

---

## Configuration

**New config option in `config.toml`:**
```toml
[server]
# Maximum number of TLS certificates to cache. Each certificate is ~1-2KB.
# Default: 1000 certificates ≈ 1-2MB memory
# Set to 0 for unlimited (not recommended for production)
max_cert_cache_entries = 1000
```

**Memory guidance:**
- 100 certificates ≈ 100-200KB
- 1000 certificates ≈ 1-2MB (default)
- 10000 certificates ≈ 10-20MB

**Recommendation:** Default of 1000 is sufficient for most use cases. Increase only if proxying to thousands of unique domains simultaneously.

---

## Observability

**Metrics exposed via control API:**

```bash
# GET /stats response
{
  "cert_cache_size": 847,
  "cert_cache_evictions": 153,
  "cert_cache_max_entries": 1000
}
```

**Monitoring recommendations:**
- High eviction rate + full cache = consider increasing `max_cert_cache_entries`
- Low eviction rate = current limit is sufficient
- Cache size never reaching max = can reduce limit to save memory

---

## Migration & Compatibility

**Backwards compatibility:**
- Default value (1000) provides eviction protection immediately
- Existing deployments without config get safe default
- Users can set to 0 to restore unbounded behavior (not recommended)

**No breaking changes:**
- getCert() signature unchanged
- External API unchanged
- Metrics additive only

---

## Security Considerations

**No security impact:**
- Evicted certificates can be regenerated on demand
- Certificate generation is already in hot path
- LRU ensures frequently accessed domains stay cached
- No certificate reuse across different hostnames

**Performance impact:**
- Minimal overhead for LRU tracking
- Lock upgrade pattern optimized for cache hit case
- Eviction is O(1) operation

---

## Summary

**Changes required:**
- Update `Proxy` struct with LRU data structures
- Implement `evictOldestCert()` helper method
- Modify `getCert()` to track LRU and evict on capacity
- Add `max_cert_cache_entries` configuration option
- Add eviction metrics to stats endpoint
- Comprehensive unit and integration tests

**Consistency with codebase:**
- Same LRU strategy as HTTP response cache
- Same entry-based eviction approach
- Consistent metrics naming and exposure
- Follows existing locking patterns

**Solves Priority 0 issue:**
- Prevents unbounded certificate cache growth
- Protects against memory exhaustion
- Maintains performance with bounded memory usage
