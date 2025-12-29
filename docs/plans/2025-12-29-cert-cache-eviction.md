# Certificate Cache Eviction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Implement LRU eviction for TLS certificate cache to prevent unbounded memory growth

**Architecture:** Add LRU tracking using container/list, evict oldest certificate when cache reaches max_cert_cache_entries limit, track evictions for observability

**Tech Stack:** Go 1.21+, container/list, sync.RWMutex, atomic.Uint64, existing test framework

---

## Context for Implementation

**Current state:**
- Certificate cache in `internal/proxy/proxy.go` is unbounded `map[string]*tls.Certificate`
- `getCert()` function generates and caches certificates but never evicts
- This causes memory exhaustion when proxying many unique domains

**Design reference:** `docs/plans/2025-12-29-cert-cache-eviction-design.md`

**Pattern to follow:** Same LRU approach as `internal/cache/cache.go` (HTTP response cache)

---

## Task 1: Add LRU Data Structures

**Files:**
- Modify: `internal/proxy/proxy.go:38-39` (Proxy struct)

**Step 1: Write failing test for certNode usage**

Add to `internal/proxy/proxy_test.go`:

```go
func TestCertCacheLRUDataStructures(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 3,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)
    proxy := NewProxy(cfg, cache)

    // Verify LRU structures initialized
    if proxy.certLRUList == nil {
        t.Fatal("certLRUList not initialized")
    }
    if proxy.certCache == nil {
        t.Fatal("certCache not initialized")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy -run TestCertCacheLRUDataStructures -v`
Expected: FAIL with "proxy.certLRUList undefined" or similar

**Step 3: Add certNode struct**

In `internal/proxy/proxy.go`, after imports and before Proxy struct:

```go
// certNode wraps a certificate with metadata for LRU tracking.
type certNode struct {
    host string
    cert *tls.Certificate
}
```

**Step 4: Update Proxy struct**

In `internal/proxy/proxy.go`, replace lines 38-39:

```go
// Old:
// certCache   map[string]*tls.Certificate
// certCacheMu sync.RWMutex

// New:
certCache     map[string]*list.Element // Maps hostname -> list element
certLRUList   *list.List               // Doubly-linked list (head=recent, tail=old)
certCacheMu   sync.RWMutex
certEvictions atomic.Uint64            // Eviction counter for metrics
```

**Step 5: Add container/list import**

In `internal/proxy/proxy.go`, add to imports:

```go
"container/list"
```

**Step 6: Run test to verify it passes**

Run: `go test ./internal/proxy -run TestCertCacheLRUDataStructures -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "feat: add LRU data structures for certificate cache

Add certNode struct and update Proxy to use list.Element map
with LRU list tracking and eviction counter."
```

---

## Task 2: Update NewProxy Initialization

**Files:**
- Modify: `internal/proxy/proxy.go` (NewProxy function)

**Step 1: Write failing test for initialization**

Add to `internal/proxy/proxy_test.go`:

```go
func TestNewProxyInitializesCertLRU(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 100,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)
    proxy := NewProxy(cfg, cache)

    if proxy.certCache == nil {
        t.Fatal("certCache map not initialized")
    }
    if proxy.certLRUList == nil {
        t.Fatal("certLRUList not initialized")
    }
    if proxy.certLRUList.Len() != 0 {
        t.Fatalf("expected empty LRU list, got %d entries", proxy.certLRUList.Len())
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy -run TestNewProxyInitializesCertLRU -v`
Expected: FAIL with nil pointer or incorrect initialization

**Step 3: Update NewProxy function**

In `internal/proxy/proxy.go`, find NewProxy function and update cert cache initialization:

```go
func NewProxy(config *config.Config, cache *cache.MemoryCache) *Proxy {
    return &Proxy{
        config:      config,
        cache:       cache,
        certCache:   make(map[string]*list.Element),
        certLRUList: list.New(),
        // ... keep existing fields
    }
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/proxy -run TestNewProxyInitializesCertLRU -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "feat: initialize cert cache LRU structures in NewProxy"
```

---

## Task 3: Add Configuration Option

**Files:**
- Modify: `internal/config/config.go` (ServerConfig struct)
- Modify: `configs/gocache.toml` (example config)

**Step 1: Write failing test for config parsing**

Add to `internal/config/config_test.go`:

```go
func TestConfigMaxCertCacheEntries(t *testing.T) {
    tomlData := `
[server]
host = "localhost"
port = 8080
max_cert_cache_entries = 500
`
    tmpFile, err := os.CreateTemp("", "config-*.toml")
    if err != nil {
        t.Fatal(err)
    }
    defer os.Remove(tmpFile.Name())

    if _, err := tmpFile.WriteString(tomlData); err != nil {
        t.Fatal(err)
    }
    tmpFile.Close()

    cfg, err := Load(tmpFile.Name())
    if err != nil {
        t.Fatalf("failed to load config: %v", err)
    }

    if cfg.Server.MaxCertCacheEntries != 500 {
        t.Fatalf("expected MaxCertCacheEntries=500, got %d", cfg.Server.MaxCertCacheEntries)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run TestConfigMaxCertCacheEntries -v`
Expected: FAIL with "MaxCertCacheEntries undefined" or zero value

**Step 3: Add config field**

In `internal/config/config.go`, add to ServerConfig struct:

```go
type ServerConfig struct {
    Host                string `toml:"host"`
    Port                int    `toml:"port"`
    MaxCertCacheEntries int    `toml:"max_cert_cache_entries"`
    // ... existing fields
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config -run TestConfigMaxCertCacheEntries -v`
Expected: PASS

**Step 5: Update example config with documentation**

In `configs/gocache.toml`, add to [server] section:

```toml
# Maximum number of TLS certificates to cache. Each certificate is ~1-2KB.
# Default: 1000 certificates ≈ 1-2MB memory
# Set to 0 for unlimited (not recommended for production)
max_cert_cache_entries = 1000
```

**Step 6: Run all config tests**

Run: `go test ./internal/config -v`
Expected: All tests PASS

**Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go configs/gocache.toml
git commit -m "feat: add max_cert_cache_entries configuration option

Add configurable limit for certificate cache with default 1000 entries.
Includes memory usage guidance in example config."
```

---

## Task 4: Implement evictOldestCert Method

**Files:**
- Modify: `internal/proxy/proxy.go` (add method)

**Step 1: Write failing test for eviction**

Add to `internal/proxy/proxy_test.go`:

```go
func TestEvictOldestCert(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 3,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)
    proxy := NewProxy(cfg, cache)

    // Manually add 3 certs to cache (oldest to newest: A, B, C)
    certA := &tls.Certificate{}
    certB := &tls.Certificate{}
    certC := &tls.Certificate{}

    nodeA := &certNode{host: "a.example.com", cert: certA}
    nodeB := &certNode{host: "b.example.com", cert: certB}
    nodeC := &certNode{host: "c.example.com", cert: certC}

    elemA := proxy.certLRUList.PushBack(nodeA)
    elemB := proxy.certLRUList.PushBack(nodeB)
    elemC := proxy.certLRUList.PushBack(nodeC)

    proxy.certCache["a.example.com"] = elemA
    proxy.certCache["b.example.com"] = elemB
    proxy.certCache["c.example.com"] = elemC

    // Evict oldest (A)
    evicted := proxy.evictOldestCert()
    if !evicted {
        t.Fatal("expected eviction to succeed")
    }

    // Verify A removed, B and C remain
    if _, exists := proxy.certCache["a.example.com"]; exists {
        t.Error("a.example.com should be evicted")
    }
    if _, exists := proxy.certCache["b.example.com"]; !exists {
        t.Error("b.example.com should still exist")
    }
    if _, exists := proxy.certCache["c.example.com"]; !exists {
        t.Error("c.example.com should still exist")
    }

    // Verify eviction counter incremented
    if proxy.certEvictions.Load() != 1 {
        t.Errorf("expected 1 eviction, got %d", proxy.certEvictions.Load())
    }

    // Verify list size
    if proxy.certLRUList.Len() != 2 {
        t.Errorf("expected LRU list length 2, got %d", proxy.certLRUList.Len())
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy -run TestEvictOldestCert -v`
Expected: FAIL with "proxy.evictOldestCert undefined"

**Step 3: Implement evictOldestCert method**

In `internal/proxy/proxy.go`, add method (near other cert cache methods):

```go
// evictOldestCert removes the least recently used certificate.
// Must be called with certCacheMu write lock held.
// Returns true if an entry was evicted, false if cache was empty.
func (p *Proxy) evictOldestCert() bool {
    elem := p.certLRUList.Back() // Tail = oldest
    if elem == nil {
        return false // Empty cache
    }

    node := elem.Value.(*certNode)
    p.certLRUList.Remove(elem)
    delete(p.certCache, node.host)
    p.certEvictions.Add(1)
    return true
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/proxy -run TestEvictOldestCert -v`
Expected: PASS

**Step 5: Test evicting from empty cache**

Add to `internal/proxy/proxy_test.go`:

```go
func TestEvictOldestCertEmptyCache(t *testing.T) {
    cfg := &config.Config{}
    cache := cache.NewMemoryCache(5*time.Minute, 0)
    proxy := NewProxy(cfg, cache)

    evicted := proxy.evictOldestCert()
    if evicted {
        t.Error("should not evict from empty cache")
    }
    if proxy.certEvictions.Load() != 0 {
        t.Errorf("expected 0 evictions, got %d", proxy.certEvictions.Load())
    }
}
```

**Step 6: Run test to verify it passes**

Run: `go test ./internal/proxy -run TestEvictOldestCertEmptyCache -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "feat: implement evictOldestCert for LRU certificate eviction"
```

---

## Task 5: Update getCert to Use LRU Eviction

**Files:**
- Modify: `internal/proxy/proxy.go:613-644` (getCert function)

**Step 1: Write failing test for LRU cache hit**

Add to `internal/proxy/proxy_test.go`:

```go
func TestGetCertCacheHitMovesToFront(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 3,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)

    // Create test CA for certificate generation
    caKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatal(err)
    }
    caTemplate := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "Test CA"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(24 * time.Hour),
        KeyUsage:     x509.KeyUsageCertSign,
        IsCA:         true,
    }
    caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
    if err != nil {
        t.Fatal(err)
    }
    caCert, err := x509.ParseCertificate(caCertDER)
    if err != nil {
        t.Fatal(err)
    }

    proxy := NewProxy(cfg, cache)
    proxy.ca = caCert
    proxy.caKey = caKey

    // Get cert for host A (adds to cache)
    cert1, err := proxy.getCert("a.example.com")
    if err != nil {
        t.Fatalf("getCert failed: %v", err)
    }
    if cert1 == nil {
        t.Fatal("expected certificate, got nil")
    }

    // Verify in cache
    if _, exists := proxy.certCache["a.example.com"]; !exists {
        t.Fatal("certificate not cached")
    }

    // Get same cert again (cache hit, should move to front)
    cert2, err := proxy.getCert("a.example.com")
    if err != nil {
        t.Fatalf("getCert cache hit failed: %v", err)
    }
    if cert2 != cert1 {
        t.Error("cache hit should return same certificate")
    }

    // Verify it's at front of LRU list
    elem := proxy.certCache["a.example.com"]
    if proxy.certLRUList.Front() != elem {
        t.Error("cache hit should move entry to front of LRU list")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy -run TestGetCertCacheHitMovesToFront -v`
Expected: FAIL because getCert doesn't update LRU yet

**Step 3: Write test for eviction on capacity**

Add to `internal/proxy/proxy_test.go`:

```go
func TestGetCertEvictsWhenAtCapacity(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 3,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)

    // Setup test CA (same as previous test)
    caKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatal(err)
    }
    caTemplate := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "Test CA"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(24 * time.Hour),
        KeyUsage:     x509.KeyUsageCertSign,
        IsCA:         true,
    }
    caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
    if err != nil {
        t.Fatal(err)
    }
    caCert, err := x509.ParseCertificate(caCertDER)
    if err != nil {
        t.Fatal(err)
    }

    proxy := NewProxy(cfg, cache)
    proxy.ca = caCert
    proxy.caKey = caKey

    // Add 3 certs (A, B, C) - fills cache
    _, err = proxy.getCert("a.example.com")
    if err != nil {
        t.Fatal(err)
    }
    _, err = proxy.getCert("b.example.com")
    if err != nil {
        t.Fatal(err)
    }
    _, err = proxy.getCert("c.example.com")
    if err != nil {
        t.Fatal(err)
    }

    // Verify all 3 cached
    if len(proxy.certCache) != 3 {
        t.Fatalf("expected 3 certs, got %d", len(proxy.certCache))
    }

    // Add 4th cert (D) - should evict A
    _, err = proxy.getCert("d.example.com")
    if err != nil {
        t.Fatal(err)
    }

    // Verify A evicted, B/C/D remain
    if _, exists := proxy.certCache["a.example.com"]; exists {
        t.Error("a.example.com should be evicted")
    }
    if _, exists := proxy.certCache["b.example.com"]; !exists {
        t.Error("b.example.com should remain")
    }
    if _, exists := proxy.certCache["c.example.com"]; !exists {
        t.Error("c.example.com should remain")
    }
    if _, exists := proxy.certCache["d.example.com"]; !exists {
        t.Error("d.example.com should be added")
    }

    // Verify eviction count
    if proxy.certEvictions.Load() != 1 {
        t.Errorf("expected 1 eviction, got %d", proxy.certEvictions.Load())
    }
}
```

**Step 4: Run test to verify it fails**

Run: `go test ./internal/proxy -run TestGetCertEvictsWhenAtCapacity -v`
Expected: FAIL because getCert doesn't evict yet

**Step 5: Update getCert implementation**

In `internal/proxy/proxy.go`, replace getCert function (lines 613-644):

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

    // Generate new certificate
    serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
    serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
    if err != nil {
        return nil, fmt.Errorf("failed to generate serial number: %w", err)
    }

    template := &x509.Certificate{
        SerialNumber: serialNumber,
        Subject:      pkix.Name{CommonName: host},
        NotBefore:    time.Now().Add(-1 * time.Hour),
        NotAfter:     time.Now().Add(365 * 24 * time.Hour),
        KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        DNSNames:     []string{host},
    }

    certKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        return nil, fmt.Errorf("failed to generate key: %w", err)
    }

    certDER, err := x509.CreateCertificate(rand.Reader, template, p.ca, &certKey.PublicKey, p.caKey)
    if err != nil {
        return nil, fmt.Errorf("failed to create certificate: %w", err)
    }

    cert := &tls.Certificate{
        Certificate: [][]byte{certDER, p.ca.Raw},
        PrivateKey:  certKey,
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

**Step 6: Run tests to verify they pass**

Run: `go test ./internal/proxy -run "TestGetCert" -v`
Expected: All getCert tests PASS

**Step 7: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "feat: update getCert to use LRU eviction

- Cache hits move entry to front of LRU list
- New certs trigger eviction when at max capacity
- Lock upgrade pattern minimizes write lock contention"
```

---

## Task 6: Test Unlimited Cache Mode

**Files:**
- Modify: `internal/proxy/proxy_test.go`

**Step 1: Write test for unlimited cache (max=0)**

Add to `internal/proxy/proxy_test.go`:

```go
func TestGetCertUnlimitedCache(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 0, // Unlimited
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)

    // Setup test CA
    caKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatal(err)
    }
    caTemplate := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "Test CA"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(24 * time.Hour),
        KeyUsage:     x509.KeyUsageCertSign,
        IsCA:         true,
    }
    caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
    if err != nil {
        t.Fatal(err)
    }
    caCert, err := x509.ParseCertificate(caCertDER)
    if err != nil {
        t.Fatal(err)
    }

    proxy := NewProxy(cfg, cache)
    proxy.ca = caCert
    proxy.caKey = caKey

    // Add 100 certs (way more than typical limit)
    for i := 0; i < 100; i++ {
        host := fmt.Sprintf("host%d.example.com", i)
        _, err := proxy.getCert(host)
        if err != nil {
            t.Fatalf("getCert(%s) failed: %v", host, err)
        }
    }

    // Verify all 100 cached (no evictions)
    if len(proxy.certCache) != 100 {
        t.Errorf("expected 100 certs, got %d", len(proxy.certCache))
    }
    if proxy.certEvictions.Load() != 0 {
        t.Errorf("expected 0 evictions with unlimited cache, got %d", proxy.certEvictions.Load())
    }
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./internal/proxy -run TestGetCertUnlimitedCache -v`
Expected: PASS (unlimited mode already works with maxEntries=0 check)

**Step 3: Commit**

```bash
git add internal/proxy/proxy_test.go
git commit -m "test: verify unlimited certificate cache mode (max=0)"
```

---

## Task 7: Test Concurrent Access

**Files:**
- Modify: `internal/proxy/proxy_test.go`

**Step 1: Write concurrent access test**

Add to `internal/proxy/proxy_test.go`:

```go
func TestGetCertConcurrentAccess(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 50,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)

    // Setup test CA
    caKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatal(err)
    }
    caTemplate := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "Test CA"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(24 * time.Hour),
        KeyUsage:     x509.KeyUsageCertSign,
        IsCA:         true,
    }
    caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
    if err != nil {
        t.Fatal(err)
    }
    caCert, err := x509.ParseCertificate(caCertDER)
    if err != nil {
        t.Fatal(err)
    }

    proxy := NewProxy(cfg, cache)
    proxy.ca = caCert
    proxy.caKey = caKey

    // Spawn 10 goroutines each requesting 100 certs
    var wg sync.WaitGroup
    errs := make(chan error, 10)

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                // Mix of duplicate and unique hosts
                host := fmt.Sprintf("host%d.example.com", j%80)
                _, err := proxy.getCert(host)
                if err != nil {
                    errs <- fmt.Errorf("goroutine %d: %w", id, err)
                    return
                }
            }
        }(i)
    }

    wg.Wait()
    close(errs)

    // Check for errors
    for err := range errs {
        t.Error(err)
    }

    // Verify cache size at limit
    if len(proxy.certCache) > 50 {
        t.Errorf("cache size %d exceeds limit 50", len(proxy.certCache))
    }

    // Verify evictions occurred
    if proxy.certEvictions.Load() == 0 {
        t.Error("expected evictions with concurrent access")
    }

    t.Logf("Final cache size: %d, evictions: %d", len(proxy.certCache), proxy.certEvictions.Load())
}
```

**Step 2: Run test with race detector**

Run: `go test ./internal/proxy -run TestGetCertConcurrentAccess -race -v`
Expected: PASS with no race conditions detected

**Step 3: Commit**

```bash
git add internal/proxy/proxy_test.go
git commit -m "test: verify concurrent certificate cache access is safe"
```

---

## Task 8: Test LRU Access Updates

**Files:**
- Modify: `internal/proxy/proxy_test.go`

**Step 1: Write test for access updating LRU order**

Add to `internal/proxy/proxy_test.go`:

```go
func TestGetCertAccessUpdatesLRU(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 3,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)

    // Setup test CA
    caKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatal(err)
    }
    caTemplate := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "Test CA"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(24 * time.Hour),
        KeyUsage:     x509.KeyUsageCertSign,
        IsCA:         true,
    }
    caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
    if err != nil {
        t.Fatal(err)
    }
    caCert, err := x509.ParseCertificate(caCertDER)
    if err != nil {
        t.Fatal(err)
    }

    proxy := NewProxy(cfg, cache)
    proxy.ca = caCert
    proxy.caKey = caKey

    // Fill cache: A, B, C (A is oldest)
    _, err = proxy.getCert("a.example.com")
    if err != nil {
        t.Fatal(err)
    }
    _, err = proxy.getCert("b.example.com")
    if err != nil {
        t.Fatal(err)
    }
    _, err = proxy.getCert("c.example.com")
    if err != nil {
        t.Fatal(err)
    }

    // Access A (moves to front)
    _, err = proxy.getCert("a.example.com")
    if err != nil {
        t.Fatal(err)
    }

    // Add D (should evict B, not A)
    _, err = proxy.getCert("d.example.com")
    if err != nil {
        t.Fatal(err)
    }

    // Verify A, C, D remain; B evicted
    if _, exists := proxy.certCache["a.example.com"]; !exists {
        t.Error("a.example.com should remain (was accessed)")
    }
    if _, exists := proxy.certCache["b.example.com"]; exists {
        t.Error("b.example.com should be evicted (oldest)")
    }
    if _, exists := proxy.certCache["c.example.com"]; !exists {
        t.Error("c.example.com should remain")
    }
    if _, exists := proxy.certCache["d.example.com"]; !exists {
        t.Error("d.example.com should be added")
    }
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./internal/proxy -run TestGetCertAccessUpdatesLRU -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/proxy/proxy_test.go
git commit -m "test: verify cache access updates LRU order correctly"
```

---

## Task 9: Add Metrics to Control API

**Files:**
- Modify: `internal/control/control.go` (stats handler)

**Step 1: Write failing test for metrics**

Add to `internal/control/control_test.go`:

```go
func TestStatsIncludesCertCacheMetrics(t *testing.T) {
    cfg := &config.Config{
        Server: config.ServerConfig{
            MaxCertCacheEntries: 100,
        },
    }
    cache := cache.NewMemoryCache(5*time.Minute, 0)
    proxy := proxy.NewProxy(cfg, cache)

    server := NewControlServer(cfg, proxy, cache)

    req := httptest.NewRequest("GET", "/stats", nil)
    rec := httptest.NewRecorder()

    server.handleStats(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("expected status 200, got %d", rec.Code)
    }

    var stats map[string]interface{}
    if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
        t.Fatalf("failed to decode response: %v", err)
    }

    // Check cert cache metrics present
    if _, ok := stats["cert_cache_size"]; !ok {
        t.Error("missing cert_cache_size metric")
    }
    if _, ok := stats["cert_cache_evictions"]; !ok {
        t.Error("missing cert_cache_evictions metric")
    }
    if _, ok := stats["cert_cache_max_entries"]; !ok {
        t.Error("missing cert_cache_max_entries metric")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/control -run TestStatsIncludesCertCacheMetrics -v`
Expected: FAIL with missing metrics

**Step 3: Find stats handler in control.go**

Run: `grep -n "func.*handleStats" internal/control/control.go`
Note the line number.

**Step 4: Add cert cache metrics to stats handler**

In `internal/control/control.go`, find the handleStats function and add cert cache metrics:

```go
func (s *ControlServer) handleStats(w http.ResponseWriter, r *http.Request) {
    cacheStats := s.cache.GetStats()

    // Get cert cache stats
    s.proxy.certCacheMu.RLock()
    certCacheSize := len(s.proxy.certCache)
    s.proxy.certCacheMu.RUnlock()
    certEvictions := s.proxy.certEvictions.Load()
    certMaxEntries := s.config.Server.MaxCertCacheEntries

    stats := map[string]interface{}{
        "cache_hits":             cacheStats.Hits,
        "cache_misses":           cacheStats.Misses,
        "cache_evictions":        cacheStats.Evictions,
        "cache_size":             cacheStats.EntryCount,
        "cache_total_size_bytes": cacheStats.TotalSize,
        "cache_max_size_bytes":   cacheStats.MaxSize,
        "cert_cache_size":        certCacheSize,
        "cert_cache_evictions":   certEvictions,
        "cert_cache_max_entries": certMaxEntries,
        "uptime_seconds":         cacheStats.UptimeSeconds,
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(stats)
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/control -run TestStatsIncludesCertCacheMetrics -v`
Expected: PASS

**Step 6: Run all control tests**

Run: `go test ./internal/control -v`
Expected: All tests PASS

**Step 7: Commit**

```bash
git add internal/control/control.go internal/control/control_test.go
git commit -m "feat: add certificate cache metrics to stats endpoint

Expose cert_cache_size, cert_cache_evictions, and cert_cache_max_entries
in control API stats response for observability."
```

---

## Task 10: Integration Test

**Files:**
- Modify: `internal/proxy/proxy_integration_test.go`

**Step 1: Write integration test with many domains**

Add to `internal/proxy/proxy_integration_test.go`:

```go
func TestProxyCertCacheEvictionIntegration(t *testing.T) {
    // Create config with small cert cache limit
    cfg := &config.Config{
        Server: config.ServerConfig{
            Host:                "localhost",
            Port:                0, // Random port
            MaxCertCacheEntries: 5,
        },
        Cache: config.CacheConfig{
            DefaultTTL: 300,
            MaxSizeMB:  10,
        },
    }

    cache := cache.NewMemoryCache(5*time.Minute, 10)
    proxy := NewProxy(cfg, cache)

    // Setup CA for proxy
    if err := proxy.setupCA(""); err != nil {
        t.Fatalf("failed to setup CA: %v", err)
    }

    // Test getCert directly for 20 unique domains
    domains := []string{}
    for i := 0; i < 20; i++ {
        domains = append(domains, fmt.Sprintf("test%d.example.com", i))
    }

    for _, domain := range domains {
        cert, err := proxy.getCert(domain)
        if err != nil {
            t.Fatalf("getCert(%s) failed: %v", domain, err)
        }
        if cert == nil {
            t.Fatalf("getCert(%s) returned nil cert", domain)
        }
    }

    // Verify cache size capped at 5
    proxy.certCacheMu.RLock()
    cacheSize := len(proxy.certCache)
    proxy.certCacheMu.RUnlock()

    if cacheSize != 5 {
        t.Errorf("expected cache size 5, got %d", cacheSize)
    }

    // Verify evictions occurred
    evictions := proxy.certEvictions.Load()
    expectedEvictions := uint64(20 - 5)
    if evictions != expectedEvictions {
        t.Errorf("expected %d evictions, got %d", expectedEvictions, evictions)
    }

    // Access a cached domain repeatedly
    cachedDomain := fmt.Sprintf("test%d.example.com", 19) // Most recent
    for i := 0; i < 5; i++ {
        _, err := proxy.getCert(cachedDomain)
        if err != nil {
            t.Fatalf("accessing cached cert failed: %v", err)
        }
    }

    // Verify still cached (frequent access keeps it)
    proxy.certCacheMu.RLock()
    _, exists := proxy.certCache[cachedDomain]
    proxy.certCacheMu.RUnlock()

    if !exists {
        t.Errorf("%s should still be cached after repeated access", cachedDomain)
    }
}
```

**Step 2: Run integration test**

Run: `go test ./internal/proxy -run TestProxyCertCacheEvictionIntegration -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/proxy/proxy_integration_test.go
git commit -m "test: add integration test for certificate cache eviction

Verify eviction works correctly with realistic domain access patterns
and cache limit enforcement."
```

---

## Task 11: Run Full Test Suite

**Files:**
- None (verification only)

**Step 1: Run all proxy tests**

Run: `go test ./internal/proxy -v`
Expected: All tests PASS

**Step 2: Run all tests with race detector**

Run: `go test ./... -race`
Expected: All tests PASS, no race conditions

**Step 3: Check test coverage**

Run: `go test ./internal/proxy -coverprofile=coverage.out && go tool cover -func=coverage.out | grep total`
Expected: Coverage >70% (target 80%+)

**Step 4: Run go vet**

Run: `go vet ./...`
Expected: No issues

**Step 5: Check formatting**

Run: `gofmt -l .`
Expected: No output (all files formatted)

**Step 6: If any issues, fix and re-test**

Fix any failures, then re-run tests.

**Step 7: Commit if fixes were needed**

```bash
git add .
git commit -m "fix: address test failures and formatting issues"
```

---

## Task 12: Update TODO.md

**Files:**
- Modify: `docs/TODO.md`

**Step 1: Mark certificate cache eviction as complete**

In `docs/TODO.md`, update line 23:

```markdown
# Old:
- [ ] **Implement Certificate Cache Eviction**

# New:
- [x] **Implement Certificate Cache Eviction**
    - **Status:** ✅ Completed - Full LRU implementation with entry count limit, comprehensive tests
```

**Step 2: Verify all Priority 0 items complete**

Check that both cache eviction items are marked complete.

**Step 3: Commit**

```bash
git add docs/TODO.md
git commit -m "docs: mark certificate cache eviction as complete

All Priority 0 critical issues now resolved."
```

---

## Verification Checklist

Before marking complete, verify:

- [ ] All tests pass: `go test ./...`
- [ ] No race conditions: `go test ./... -race`
- [ ] Coverage >70%: `go test ./internal/proxy -cover`
- [ ] No vet issues: `go vet ./...`
- [ ] Properly formatted: `gofmt -l .` returns nothing
- [ ] Config option documented in `configs/gocache.toml`
- [ ] Metrics visible in `/stats` endpoint
- [ ] LRU eviction working correctly (tests verify)
- [ ] Concurrent access safe (race detector confirms)
- [ ] TODO.md updated

**Expected outcome:** Certificate cache has bounded memory usage with configurable LRU eviction, preventing memory exhaustion when proxying many unique domains.
