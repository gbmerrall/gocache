# GoCache Consolidated Action Plan

This document combines the findings from multiple code analysis reports into a single prioritized roadmap for codebase improvement.

**Progress Tracking:**
- [ ] Not Started
- [/] In Progress
- [x] Completed

---

## 🚨 Priority 0: Critical (Must Fix Before Production)

These issues represent memory leaks or critical stability risks that must be addressed immediately.

- [x] **Implement HTTP Cache Eviction & Size Enforcement**
    - **Issue:** `max_size_mb` configuration is parsed but ignored. The cache grows unbounded until OOM.
    - **Action:**
        - Add LRU (Least Recently Used) eviction to `internal/cache`.
        - Enforce strict size limits in `Set()` operations.
        - Track current size incrementally to ensure `GetStats` is O(1).
    - **Status:** ✅ Completed - Full LRU implementation with size enforcement, background cleanup, and comprehensive tests
- [x] **Implement Certificate Cache Eviction**
    - **Status:** ✅ Completed - Full LRU implementation with entry count limit, comprehensive tests

## 🔥 Priority 1: High (Immediate Technical Debt)

These issues significantly impact maintainability, test reliability, or correct logic.

- [ ] **Deduplicate Proxy Handler Logic**
    - **Issue:** `handleHTTP` and `handleHTTPS` in `internal/proxy/proxy.go` share ~70% identical logic (cache lookup, response caching, logging).
    - **Action:** Extract common logic into a helper struct/methods (e.g., `cacheHandler`).
- [ ] **Fix Flaky Tests (`time.Sleep`)**
    - **Issue:** Tests use `time.Sleep` for synchronization (27 occurrences), leading to flakiness or slow execution.
    - **Action:** Replace sleeps with channels or `sync.WaitGroup` in `access_test.go`, `proxy_integration_test.go`, and `resilience_test.go`.

## 🛠 Priority 2: Medium (Refactoring & Robustness)

These improvements target code quality, safety, and specific edge cases.

- [ ] **Add Cache Rejection Observability**
    - **Issue:** Oversized cache entries are silently rejected with no logging or metrics (`internal/cache/cache.go:148-150`).
    - **Impact:** Operators can't detect misconfigured `max_size_mb` or unexpectedly large responses.
    - **Action:** Add `rejections atomic.Uint64` counter to `MemoryCache`, include in `CacheStats`, increment on rejection.
- [ ] **Fix LoadFromFile LRU Order**
    - **Issue:** When loading persisted cache, entries are added in random order due to Go map iteration (`internal/cache/cache.go:268`).
    - **Impact:** After restart, initial evictions remove random entries instead of least-recently-used. LRU becomes correct after cache usage.
    - **Action:** Either document as known limitation, or persist/restore last-access timestamps to maintain LRU order across restarts.
- [ ] **Refactor `internal/cert` Global State**
    - **Issue:** Global `certDir` variable makes testing difficult and prevents parallel execution.
    - **Action:** encapsulate state in a `CertStore` struct; remove global variables.
- [ ] **Fix Async Delete Goroutine Leak**
    - **Issue:** `go c.delete(key)` spawns unbounded goroutines.
    - **Action:** Switch to synchronous deletion or use a bounded worker pool.
- [ ] **Type-Safe Control API Responses**
    - **Issue:** Extensive use of `map[string]interface{}`.
    - **Action:** Define concrete structs for JSON responses in `internal/control`.
- [ ] **IPv6 Localhost Support**
    - **Issue:** Control API security check hardcodes IPv4 loopback, failing for `::1`.
    - **Action:** Update `internal/control/control.go` to accept IPv6 loopback addresses.
- [ ] **Apply `gofmt` to All Files**
    - **Issue:** 19 files are not formatted according to standard Go style.
    - **Action:** Run `gofmt -w ./...`.
- [ ] **Improve Domain Purge Efficiency**
    - **Issue:** `PurgeByDomain` performs O(N) URL parsing.
    - **Action:** Store parsed domain in `CacheEntry` or maintain a domain index.

## 📉 Priority 3: Low / Future (Enhancements)

These are desirable improvements for the long term but not blocking.

- [ ] **Refactor Large Functions**
    - **Issue:** `handleHTTP`, `handleHTTPS`, and `startServer` are too long.
    - **Action:** Break down into smaller, testable units.
- [ ] **Add Performance Benchmarks**
    - **Action:** Add `Benchmark*` tests for hot paths (cache get/set, key generation).
- [ ] **Add Missing Godocs**
    - **Action:** Add comments to exported symbols in `internal/proxy`.
- [ ] **Enhance Config Validation**
    - **Action:** Add `Validate()` method to verify port ranges, etc.
- [ ] **Implement Rate Limiting**
    - **Action:** Add middleware to protect against DoS.
- [ ] **Prometheus Metrics**
    - **Action:** Implement `/metrics` endpoint.
