package cache

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMemoryCache(t *testing.T) {
	t.Run("Set and Get", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("hello"),
		}
		c.Set("key1", entry)

		got, ok := c.Get("key1")
		if !ok {
			t.Fatal("expected to find key1")
		}
		if string(got.Body) != "hello" {
			t.Errorf("got body %q, want %q", got.Body, "hello")
		}
	})

	t.Run("Get expired", func(t *testing.T) {
		c := NewMemoryCache(1*time.Millisecond, 0)
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("world"),
		}
		c.Set("key2", entry)

		time.Sleep(2 * time.Millisecond)

		_, ok := c.Get("key2")
		if ok {
			t.Fatal("expected key2 to be expired")
		}
	})

	t.Run("Get non-existent", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		_, ok := c.Get("nonexistent")
		if ok {
			t.Fatal("expected not to find non-existent key")
		}
	})

	t.Run("UpdateTTL", func(t *testing.T) {
		c := NewMemoryCache(1*time.Hour, 0)
		newTTL := 2 * time.Hour
		c.UpdateTTL(newTTL)

		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("test"),
		}
		c.Set("test-key", entry)

		// Verify the entry uses the new TTL
		got, ok := c.Get("test-key")
		if !ok {
			t.Fatal("expected to find test-key")
		}
		if got.Expiry.Before(time.Now().Add(1 * time.Hour)) {
			t.Error("entry should have new TTL")
		}
	})

	t.Run("PurgeAll", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("test"),
		}
		c.Set("key1", entry)
		c.Set("key2", entry)

		count := c.PurgeAll()
		if count != 2 {
			t.Errorf("expected to purge 2 items, got %d", count)
		}

		_, ok := c.Get("key1")
		if ok {
			t.Error("expected key1 to be purged")
		}
		_, ok = c.Get("key2")
		if ok {
			t.Error("expected key2 to be purged")
		}
	})

	t.Run("PurgeByURL", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("test"),
		}
		c.Set("https://example.com/page1", entry)
		c.Set("https://example.com/page2", entry)

		found := c.PurgeByURL("https://example.com/page1")
		if !found {
			t.Error("expected to find and purge URL")
		}

		_, ok := c.Get("https://example.com/page1")
		if ok {
			t.Error("expected page1 to be purged")
		}
		_, ok = c.Get("https://example.com/page2")
		if !ok {
			t.Error("expected page2 to remain")
		}
	})

	t.Run("PurgeByDomain", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("test"),
		}
		c.Set("https://example.com/page1", entry)
		c.Set("https://example.com/page2", entry)
		c.Set("https://other.com/page1", entry)

		count := c.PurgeByDomain("example.com")
		if count != 2 {
			t.Errorf("expected to purge 2 items, got %d", count)
		}

		_, ok := c.Get("https://example.com/page1")
		if ok {
			t.Error("expected example.com/page1 to be purged")
		}
		_, ok = c.Get("https://example.com/page2")
		if ok {
			t.Error("expected example.com/page2 to be purged")
		}
		_, ok = c.Get("https://other.com/page1")
		if !ok {
			t.Error("expected other.com/page1 to remain")
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		var wg sync.WaitGroup
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("concurrent"),
		}

		// Start multiple goroutines writing
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				c.Set(fmt.Sprintf("key%d", id), entry)
			}(i)
		}

		// Start multiple goroutines reading
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				c.Get(fmt.Sprintf("key%d", id))
			}(i)
		}

		wg.Wait()
		stats := c.GetStats()
		if stats.EntryCount != 10 {
			t.Errorf("expected 10 entries, got %d", stats.EntryCount)
		}
	})
}

func TestCachePersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cacheFile := filepath.Join(tmpDir, "cache.gob")

	t.Run("SaveToFile", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"text/plain"}},
			Body:       []byte("persistent data"),
		}
		c.Set("persistent-key", entry)

		err := c.SaveToFile(cacheFile)
		if err != nil {
			t.Fatalf("failed to save cache: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
			t.Error("cache file was not created")
		}
	})

	t.Run("LoadFromFile", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		err := c.LoadFromFile(cacheFile)
		if err != nil {
			t.Fatalf("failed to load cache: %v", err)
		}

		entry, ok := c.Get("persistent-key")
		if !ok {
			t.Fatal("expected to find persistent-key after loading")
		}
		if string(entry.Body) != "persistent data" {
			t.Errorf("got body %q, want %q", entry.Body, "persistent data")
		}
		if entry.Headers.Get("Content-Type") != "text/plain" {
			t.Errorf("got content-type %q, want %q", entry.Headers.Get("Content-Type"), "text/plain")
		}
	})

	t.Run("LoadFromNonExistentFile", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		err := c.LoadFromFile("nonexistent.gob")
		if err == nil {
			t.Error("expected error loading non-existent file")
		}
	})

	t.Run("SaveToFileWithDirectoryCreation", func(t *testing.T) {
		c := NewMemoryCache(1*time.Minute, 0)
		entry := CacheEntry{
			StatusCode: http.StatusOK,
			Body:       []byte("test"),
		}
		c.Set("test-key", entry)

		nestedFile := filepath.Join(tmpDir, "nested", "cache.gob")
		err := c.SaveToFile(nestedFile)
		if err != nil {
			t.Fatalf("failed to save cache to nested directory: %v", err)
		}

		if _, err := os.Stat(nestedFile); os.IsNotExist(err) {
			t.Error("nested cache file was not created")
		}
	})
}

func TestCacheStats(t *testing.T) {
	c := NewMemoryCache(1*time.Minute, 0)
	entry := CacheEntry{
		StatusCode: http.StatusOK,
		Body:       []byte("stats test"),
	}

	// Add some entries
	c.Set("key1", entry)
	c.Set("key2", entry)

	// Get some entries (hits)
	c.Get("key1")
	c.Get("key2")

	// Try to get non-existent (misses)
	c.Get("nonexistent1")
	c.Get("nonexistent2")

	stats := c.GetStats()
	if stats.EntryCount != 2 {
		t.Errorf("expected 2 entries, got %d", stats.EntryCount)
	}
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.TotalSize == 0 {
		t.Error("expected non-zero total size")
	}
	if stats.UptimeSeconds <= 0 {
		t.Error("expected positive uptime")
	}
}

func TestSetWithTTL(t *testing.T) {
	t.Run("SetWithTTL custom duration", func(t *testing.T) {
		c := NewMemoryCache(1*time.Hour, 0) // Default TTL of 1 hour
		entry := CacheEntry{
			StatusCode: http.StatusNotFound,
			Body:       []byte("error response"),
		}

		// Set with custom TTL (negative TTL for error)
		customTTL := 5 * time.Second
		c.SetWithTTL("error-key", entry, customTTL)

		// Should be available immediately
		got, ok := c.Get("error-key")
		if !ok {
			t.Fatal("expected to find error-key")
		}
		if string(got.Body) != "error response" {
			t.Errorf("got body %q, want %q", got.Body, "error response")
		}

		// Should expire based on custom TTL, not default
		expectedExpiry := time.Now().Add(customTTL)
		if got.Expiry.After(expectedExpiry.Add(1*time.Second)) || got.Expiry.Before(expectedExpiry.Add(-1*time.Second)) {
			t.Errorf("expiry time %v not close to expected %v", got.Expiry, expectedExpiry)
		}
	})

	t.Run("SetWithTTL expiration", func(t *testing.T) {
		c := NewMemoryCache(1*time.Hour, 0)
		entry := CacheEntry{
			StatusCode: http.StatusInternalServerError,
			Body:       []byte("server error"),
		}

		// Set with very short TTL
		c.SetWithTTL("short-ttl-key", entry, 2*time.Millisecond)

		// Wait for expiration
		time.Sleep(5 * time.Millisecond)

		// Should be expired
		_, ok := c.Get("short-ttl-key")
		if ok {
			t.Fatal("expected short-ttl-key to be expired")
		}
	})

	t.Run("SetWithTTL vs Set comparison", func(t *testing.T) {
		c := NewMemoryCache(1*time.Hour, 0)
		entry1 := CacheEntry{StatusCode: http.StatusOK, Body: []byte("normal")}
		entry2 := CacheEntry{StatusCode: http.StatusNotFound, Body: []byte("error")}

		// Set normal entry with default TTL
		c.Set("normal-key", entry1)

		// Set error entry with custom short TTL
		c.SetWithTTL("error-key", entry2, 10*time.Millisecond)

		// Both should be available initially
		_, ok1 := c.Get("normal-key")
		_, ok2 := c.Get("error-key")
		if !ok1 || !ok2 {
			t.Fatal("both entries should be available initially")
		}

		// Wait for short TTL to expire
		time.Sleep(15 * time.Millisecond)

		// Normal entry should still be available, error entry should be expired
		_, ok1 = c.Get("normal-key")
		_, ok2 = c.Get("error-key")
		if !ok1 {
			t.Error("normal entry should still be available")
		}
		if ok2 {
			t.Error("error entry should be expired")
		}
	})
}

func TestMemoryCache_LRUOrder(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 10) // 10MB limit
	defer cache.Shutdown()

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

func TestMemoryCache_SizeEnforcement(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 1) // 1MB limit
	defer cache.Shutdown()

	// Add entry that fills cache
	data := make([]byte, 600*1024) // 600KB
	cache.Set("key1", CacheEntry{Body: data})

	stats := cache.GetStats()
	if stats.TotalSize != 600*1024 {
		t.Errorf("Expected size %d, got %d", 600*1024, stats.TotalSize)
	}

	// Add another entry - should evict key1 (600KB + 600KB > 1MB)
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

func TestMemoryCache_OversizedEntryRejection(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 1) // 1MB limit
	defer cache.Shutdown()

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

func TestMemoryCache_UnlimitedCache(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 0) // Unlimited
	defer cache.Shutdown()

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

func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 10) // 10MB limit
	defer cache.Shutdown()

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

	// Wait for expiration + cleanup cycle
	// Since cleanup runs every 1 minute, we test expiration via Get() which checks expiry
	time.Sleep(200 * time.Millisecond)

	// Entries should be expired (Get will remove them)
	_, found1 := cache.Get("key1")
	_, found2 := cache.Get("key2")
	if found1 || found2 {
		t.Error("Expected entries to be expired")
	}

	stats = cache.GetStats()
	if stats.EntryCount != 0 {
		t.Errorf("Expected 0 entries after expiration, got %d", stats.EntryCount)
	}
}

func TestMemoryCache_Shutdown(t *testing.T) {
	cache := NewMemoryCache(1*time.Hour, 10)

	// Add some entries
	cache.Set("key1", CacheEntry{Body: []byte("data")})

	// Shutdown should not panic
	cache.Shutdown()

	// Verify cleanup goroutine stopped (no way to directly test, but Shutdown should return)
	// If it blocks, test will timeout
}

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
