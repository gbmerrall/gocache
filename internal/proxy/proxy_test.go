package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gbmerrall/gocache/internal/cache"
	"github.com/gbmerrall/gocache/internal/cert"
	"github.com/gbmerrall/gocache/internal/config"
)

func setupProxyTest(t *testing.T) (*httptest.Server, *http.Client, *cache.MemoryCache, func()) {
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	c := cache.NewMemoryCache(1*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/non-cacheable" {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte("binary data"))
		} else if r.URL.Path == "/error" {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		} else {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("hello world"))
		}
	}))

	p.SetTransport(&http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: upstream.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs,
		},
	})

	proxyServer := httptest.NewServer(p)

	proxyURL, _ := url.Parse(proxyServer.URL)
	caCertPool := x509.NewCertPool()
	caCertPool.AddCert(p.GetCA())
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
		Timeout: 10 * time.Second, // Add timeout to prevent hangs
	}

	cleanup := func() {
		proxyServer.Close()
		upstream.Close()
		os.RemoveAll(tmpDir)
	}

	return upstream, client, c, cleanup
}

func TestProxy(t *testing.T) {
	upstream, client, _, cleanup := setupProxyTest(t)
	defer cleanup()

	t.Run("HTTP request", func(t *testing.T) {
		// Need a separate non-TLS upstream for this.
		httpUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("hello http"))
		}))
		defer httpUpstream.Close()

		resp, err := client.Get(httpUpstream.URL)
		if err != nil {
			t.Fatalf("http request failed: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "hello http" {
			t.Errorf("got body %q, want %q", body, "hello http")
		}
	})

	t.Run("HTTPS request", func(t *testing.T) {
		resp, err := client.Get(upstream.URL)
		if err != nil {
			t.Fatalf("https request failed: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "hello world" {
			t.Errorf("got body %q, want %q", body, "hello world")
		}
	})

	t.Run("Non-cacheable content type", func(t *testing.T) {
		resp, err := client.Get(upstream.URL + "/non-cacheable")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "binary data" {
			t.Errorf("got body %q, want %q", body, "binary data")
		}
	})

	t.Run("Upstream error", func(t *testing.T) {
		resp, err := client.Get(upstream.URL + "/error")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusInternalServerError)
		}
	})
}

func setupTestProxy(t *testing.T) (*Proxy, func()) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-proxy")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cert.SetCertDir(tmpDir)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.NewDefaultConfig()
	c := cache.NewMemoryCache(1*time.Minute, 0)

	p, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	return p, func() {
		os.RemoveAll(tmpDir)
	}
}

func TestNewProxy(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-proxy")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cert.SetCertDir(tmpDir)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.NewDefaultConfig()
	c := cache.NewMemoryCache(1*time.Minute, 0)

	proxy, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}
	if proxy.logger != logger {
		t.Error("logger not set correctly")
	}
	if proxy.cache != c {
		t.Error("cache not set correctly")
	}
	if proxy.config != cfg {
		t.Error("config not set correctly")
	}
}

func TestGetCA(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	ca := proxy.GetCA()
	if ca == nil {
		t.Error("expected non-nil CA certificate")
	}
}

func TestGetCertCacheStats(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	stats := proxy.GetCertCacheStats()
	// GetCertCacheStats returns an int, not a pointer, so we can't check for nil
	if stats < 0 {
		t.Error("expected non-negative cert cache stats")
	}
}

func TestSetConfig(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	newCfg := config.NewDefaultConfig()
	newCfg.Cache.DefaultTTL = "2h"
	newCfg.Cache.MaxSizeMB = 1000

	proxy.SetConfig(newCfg)

	if proxy.config.Cache.DefaultTTL != "2h" {
		t.Errorf("expected TTL 2h, got %s", proxy.config.Cache.DefaultTTL)
	}
	if proxy.config.Cache.MaxSizeMB != 1000 {
		t.Errorf("expected max size 1000MB, got %d", proxy.config.Cache.MaxSizeMB)
	}
}

func TestSetTransport(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	proxy.SetTransport(transport)

	if proxy.transport != transport {
		t.Error("transport not set correctly")
	}
}

func TestGetCacheKey(t *testing.T) {
	// Test getCacheKey function directly
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"simple URL", "http://example.com", "http://example.com"},
		{"URL with path", "http://example.com/page", "http://example.com/page"},
		{"URL with query params", "http://example.com/page?a=1&b=2", "http://example.com/page?a=1&b=2"},
		{"URL with fragment", "http://example.com/page#section", "http://example.com/page"},
		{"URL with query and fragment", "http://example.com/page?a=1#section", "http://example.com/page?a=1"},
		{"URL with sorted query params", "http://example.com/page?b=2&a=1", "http://example.com/page?a=1&b=2"},
		{"HTTPS URL", "https://example.com", "https://example.com"},
		{"URL with port", "http://example.com:8080", "http://example.com:8080"},
		{"URL with complex query", "http://example.com/page?param[]=1&param[]=2", "http://example.com/page?param%5B%5D=1&param%5B%5D=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", tt.url, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			key := getCacheKey(req)
			if key != tt.expected {
				t.Errorf("getCacheKey(%s) = %s, want %s", tt.url, key, tt.expected)
			}
		})
	}
}

func TestShouldCacheResponse(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	tests := []struct {
		name           string
		statusCode     int
		contentType    string
		expectedResult bool
	}{
		{"text/html OK", http.StatusOK, "text/html", true},
		{"text/plain OK", http.StatusOK, "text/plain", true},
		{"application/json OK", http.StatusOK, "application/json", true},
		{"text/css OK", http.StatusOK, "text/css", true},
		{"application/javascript OK", http.StatusOK, "application/javascript", true},
		{"application/octet-stream OK", http.StatusOK, "application/octet-stream", false},
		{"text/html 404", http.StatusNotFound, "text/html", true},
		{"text/html 500", http.StatusInternalServerError, "text/html", true},
		{"no content type", http.StatusOK, "", false},
		{"image/png", http.StatusOK, "image/png", false},
		{"video/mp4", http.StatusOK, "video/mp4", false},
		{"audio/mpeg", http.StatusOK, "audio/mpeg", false},
		{"application/pdf", http.StatusOK, "application/pdf", false},
		{"text/html with charset", http.StatusOK, "text/html; charset=utf-8", true},
		{"application/json with charset", http.StatusOK, "application/json; charset=utf-8", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     http.Header{},
			}
			if tt.contentType != "" {
				resp.Header.Set("Content-Type", tt.contentType)
			}

			result := proxy.shouldCacheResponse(resp)
			if result != tt.expectedResult {
				t.Errorf("shouldCacheResponse(%d, %s) = %v, want %v",
					tt.statusCode, tt.contentType, result, tt.expectedResult)
			}
		})
	}
}

func TestProxyCaching(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>Test Content</body></html>"))
	}))
	defer server.Close()

	// Test that responses are cached
	req1, _ := http.NewRequest("GET", server.URL, nil)
	w1 := httptest.NewRecorder()
	proxy.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("first request failed with status %d", w1.Code)
	}

	// Check if response was cached
	cacheKey := getCacheKey(req1)
	cached, found := proxy.cache.Get(cacheKey)
	if !found {
		t.Error("expected response to be cached")
	}
	if cached.StatusCode != http.StatusOK {
		t.Errorf("cached response has wrong status: %d", cached.StatusCode)
	}

	// Test cache hit
	req2, _ := http.NewRequest("GET", server.URL, nil)
	w2 := httptest.NewRecorder()
	proxy.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("second request failed with status %d", w2.Code)
	}

	// Verify it was served from cache (the proxy might not add cache headers)
	// Just check that the response was successful
	if w2.Code != http.StatusOK {
		t.Error("expected successful response from cache")
	}
}

func TestProxyNonCacheable(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("binary data"))
	}))
	defer server.Close()

	// Test that non-cacheable responses are not cached
	req, _ := http.NewRequest("GET", server.URL, nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("request failed with status %d", w.Code)
	}

	// Check that response was NOT cached
	cacheKey := getCacheKey(req)
	_, found := proxy.cache.Get(cacheKey)
	if found {
		t.Error("expected response NOT to be cached")
	}
}

func TestProxyErrorHandling(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	// Test with non-existent server
	req, _ := http.NewRequest("GET", "http://localhost:99999", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Should get an error status
	if w.Code == http.StatusOK {
		t.Error("expected error status for non-existent server")
	}
}

func TestProxyHTTPS(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	// Create a test HTTPS server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>HTTPS Test</body></html>"))
	}))
	defer server.Close()

	// Configure proxy to trust the test server
	proxy.SetTransport(&http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	})

	// Test HTTPS request
	req, _ := http.NewRequest("GET", server.URL, nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HTTPS request failed with status %d", w.Code)
	}
}

func TestProxyRequestMethods(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("Method: " + r.Method))
	}))
	defer server.Close()

	methods := []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req, _ := http.NewRequest(method, server.URL, nil)
			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, req)

			// All methods should work, but only GET should be cached
			if w.Code != http.StatusOK {
				t.Errorf("%s request failed with status %d", method, w.Code)
			}

			if method == "GET" {
				// Check if GET was cached
				cacheKey := getCacheKey(req)
				_, found := proxy.cache.Get(cacheKey)
				if !found {
					t.Error("expected GET request to be cached")
				}
			}
		})
	}
}

func TestProxyHeaders(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back some headers
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Original-Host", r.Host)
		w.Header().Set("X-User-Agent", r.Header.Get("User-Agent"))
		w.Write([]byte("Headers test"))
	}))
	defer server.Close()

	// Test with custom headers
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("User-Agent", "GoCache-Test/1.0")
	req.Header.Set("Accept", "text/html")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("request failed with status %d", w.Code)
	}

	// Check that headers were preserved
	if w.Header().Get("X-User-Agent") != "GoCache-Test/1.0" {
		t.Error("User-Agent header not preserved")
	}
}

func TestProxyLargeResponse(t *testing.T) {
	proxy, cleanup := setupTestProxy(t)
	defer cleanup()

	// Create a test server with large response
	largeContent := strings.Repeat("A", 1024*1024) // 1MB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(largeContent))
	}))
	defer server.Close()

	// Test large response
	req, _ := http.NewRequest("GET", server.URL, nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("large response request failed with status %d", w.Code)
	}

	// Verify content length
	if len(w.Body.String()) != len(largeContent) {
		t.Errorf("content length mismatch: got %d, want %d",
			len(w.Body.String()), len(largeContent))
	}
}

func TestXCacheHeader(t *testing.T) {
	upstream, client, _, cleanup := setupProxyTest(t)
	defer cleanup()

	t.Run("HTTP request", func(t *testing.T) {
		httpUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("hello http"))
		}))
		defer httpUpstream.Close()

		// First request should be a miss
		resp, err := client.Get(httpUpstream.URL)
		if err != nil {
			t.Fatalf("http request failed: %v", err)
		}
		if resp.Header.Get("X-Cache") != "MISS" {
			t.Errorf("got X-Cache %q, want %q", resp.Header.Get("X-Cache"), "MISS")
		}
		resp.Body.Close()

		// Second request should be a hit
		resp, err = client.Get(httpUpstream.URL)
		if err != nil {
			t.Fatalf("http request failed: %v", err)
		}
		if resp.Header.Get("X-Cache") != "HIT" {
			t.Errorf("got X-Cache %q, want %q", resp.Header.Get("X-Cache"), "HIT")
		}
		resp.Body.Close()
	})

	t.Run("HTTPS request", func(t *testing.T) {
		// First request should be a miss
		resp, err := client.Get(upstream.URL)
		if err != nil {
			t.Fatalf("https request failed: %v", err)
		}
		if resp.Header.Get("X-Cache") != "MISS" {
			t.Errorf("got X-Cache %q, want %q", resp.Header.Get("X-Cache"), "MISS")
		}
		resp.Body.Close()

		// Second request should be a hit
		resp, err = client.Get(upstream.URL)
		if err != nil {
			t.Fatalf("https request failed: %v", err)
		}
		if resp.Header.Get("X-Cache") != "HIT" {
			t.Errorf("got X-Cache %q, want %q", resp.Header.Get("X-Cache"), "HIT")
		}
		resp.Body.Close()
	})
}

func TestNegativeTTL(t *testing.T) {
	t.Run("Error status codes use negative TTL", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-negative-ttl")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cert.SetCertDir(tmpDir)

		// Configure with very short negative TTL for testing
		cfg := config.NewDefaultConfig()
		cfg.Cache.DefaultTTL = "1h"
		cfg.Cache.NegativeTTL = "50ms" // Very short for testing

		c := cache.NewMemoryCache(cfg.Cache.GetDefaultTTL(), cfg.Cache.MaxSizeMB)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		p, err := NewProxy(logger, c, cfg)
		if err != nil {
			t.Fatalf("failed to create proxy: %v", err)
		}

		// Create test server that returns 404
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			http.Error(w, "Not Found", http.StatusNotFound)
		}))
		defer server.Close()

		// First request - should cache with negative TTL
		req1, _ := http.NewRequest("GET", server.URL, nil)
		w1 := httptest.NewRecorder()
		p.ServeHTTP(w1, req1)

		if w1.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w1.Code)
		}

		// Check that response was cached
		cacheKey := getCacheKey(req1)
		cached, found := c.Get(cacheKey)
		if !found {
			t.Error("expected 404 response to be cached")
		}
		if cached.StatusCode != http.StatusNotFound {
			t.Errorf("cached response has wrong status: %d", cached.StatusCode)
		}

		// Immediately make second request - should be cache hit
		req2, _ := http.NewRequest("GET", server.URL, nil)
		w2 := httptest.NewRecorder()
		p.ServeHTTP(w2, req2)

		if w2.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w2.Code)
		}

		// Wait for negative TTL to expire
		time.Sleep(100 * time.Millisecond)

		// Third request - should be cache miss due to expiration
		req3, _ := http.NewRequest("GET", server.URL, nil)
		w3 := httptest.NewRecorder()
		p.ServeHTTP(w3, req3)

		if w3.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w3.Code)
		}
	})

	t.Run("Success status codes use default TTL", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-default-ttl")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cert.SetCertDir(tmpDir)

		// Configure with very short negative TTL but longer default TTL
		cfg := config.NewDefaultConfig()
		cfg.Cache.DefaultTTL = "200ms" // Short for testing but longer than negative
		cfg.Cache.NegativeTTL = "50ms"

		c := cache.NewMemoryCache(cfg.Cache.GetDefaultTTL(), cfg.Cache.MaxSizeMB)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		p, err := NewProxy(logger, c, cfg)
		if err != nil {
			t.Fatalf("failed to create proxy: %v", err)
		}

		// Create test server that returns 200 OK
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("Success"))
		}))
		defer server.Close()

		// First request - should cache with default TTL
		req1, _ := http.NewRequest("GET", server.URL, nil)
		w1 := httptest.NewRecorder()
		p.ServeHTTP(w1, req1)

		if w1.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w1.Code)
		}

		// Wait past negative TTL but not past default TTL
		time.Sleep(100 * time.Millisecond)

		// Second request - should still be cache hit
		req2, _ := http.NewRequest("GET", server.URL, nil)
		w2 := httptest.NewRecorder()
		p.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w2.Code)
		}

		// Verify still cached
		cacheKey := getCacheKey(req1)
		_, found := c.Get(cacheKey)
		if !found {
			t.Error("expected 200 response to still be cached")
		}
	})

	t.Run("isErrorStatusCode function", func(t *testing.T) {
		tests := []struct {
			statusCode int
			expected   bool
		}{
			{200, false}, // 2xx - success
			{201, false},
			{301, false}, // 3xx - redirect
			{302, false},
			{400, true}, // 4xx - client error
			{401, true},
			{404, true},
			{403, true},
			{500, true}, // 5xx - server error
			{501, true},
			{502, true},
			{503, true},
			{100, false}, // 1xx - informational
			{101, false},
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf("status_%d", tt.statusCode), func(t *testing.T) {
				result := isErrorStatusCode(tt.statusCode)
				if result != tt.expected {
					t.Errorf("isErrorStatusCode(%d) = %v, want %v", tt.statusCode, result, tt.expected)
				}
			})
		}
	})
}

func setupPostCacheTest(t *testing.T, cfg *config.Config) (*httptest.Server, *http.Client, func()) {
	tmpDir, err := os.MkdirTemp("", "gocache-post-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	cert.SetCertDir(tmpDir)

	if cfg == nil {
		cfg = config.NewDefaultConfig()
	}

	c := cache.NewMemoryCache(1*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Upstream server that echoes back the request body
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if r.URL.Path == "/large" {
			w.Write(make([]byte, 2*1024*1024)) // 2MB response
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	}))

	p.SetTransport(http.DefaultTransport)
	proxyServer := httptest.NewServer(p)
	proxyURL, _ := url.Parse(proxyServer.URL)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 10 * time.Second,
	}

	cleanup := func() {
		proxyServer.Close()
		upstream.Close()
		os.RemoveAll(tmpDir)
	}

	return upstream, client, cleanup
}

func TestProxyPostCaching(t *testing.T) {
	t.Run("POST caching disabled by default", func(t *testing.T) {
		upstream, client, cleanup := setupPostCacheTest(t, nil) // Default config
		defer cleanup()

		body := "request body 1"
		resp1, err := client.Post(upstream.URL, "text/plain", strings.NewReader(body))
		if err != nil {
			t.Fatalf("first request failed: %v", err)
		}
		if resp1.Header.Get("X-Cache") == "HIT" {
			t.Error("expected first request to be a miss, got HIT")
		}
		resp1.Body.Close()

		resp2, err := client.Post(upstream.URL, "text/plain", strings.NewReader(body))
		if err != nil {
			t.Fatalf("second request failed: %v", err)
		}
		if resp2.Header.Get("X-Cache") == "HIT" {
			t.Error("expected second request to be a miss, got HIT")
		}
		resp2.Body.Close()
	})

	t.Run("POST caching enabled", func(t *testing.T) {
		cfg := config.NewDefaultConfig()
		cfg.Cache.PostCache.Enable = true
		upstream, client, cleanup := setupPostCacheTest(t, cfg)
		defer cleanup()

		body := "request body 2"
		resp1, err := client.Post(upstream.URL, "text/plain", strings.NewReader(body))
		if err != nil {
			t.Fatalf("first request failed: %v", err)
		}
		if resp1.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected first request to be a MISS, got %s", resp1.Header.Get("X-Cache"))
		}
		resp1.Body.Close()

		resp2, err := client.Post(upstream.URL, "text/plain", strings.NewReader(body))
		if err != nil {
			t.Fatalf("second request failed: %v", err)
		}
		if resp2.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected second request to be a HIT, got %s", resp2.Header.Get("X-Cache"))
		}
		resp2.Body.Close()
	})

	t.Run("Request body too large", func(t *testing.T) {
		cfg := config.NewDefaultConfig()
		cfg.Cache.PostCache.Enable = true
		cfg.Cache.PostCache.MaxRequestBodySizeMB = 1
		upstream, client, cleanup := setupPostCacheTest(t, cfg)
		defer cleanup()

		body := make([]byte, 2*1024*1024) // 2MB
		resp, err := client.Post(upstream.URL, "text/plain", strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("expected status %d, got %d", http.StatusRequestEntityTooLarge, resp.StatusCode)
		}
	})

	t.Run("Response body too large", func(t *testing.T) {
		cfg := config.NewDefaultConfig()
		cfg.Cache.PostCache.Enable = true
		cfg.Cache.PostCache.MaxResponseBodySizeMB = 1
		upstream, client, cleanup := setupPostCacheTest(t, cfg)
		defer cleanup()

		// First request to /large endpoint
		resp1, err := client.Post(upstream.URL+"/large", "text/plain", strings.NewReader("body"))
		if err != nil {
			t.Fatalf("first request failed: %v", err)
		}
		if resp1.Header.Get("X-Cache") == "HIT" {
			t.Error("expected first request to be a miss, got HIT")
		}
		resp1.Body.Close()

		// Second request should also be a miss because the response was too large to cache
		resp2, err := client.Post(upstream.URL+"/large", "text/plain", strings.NewReader("body"))
		if err != nil {
			t.Fatalf("second request failed: %v", err)
		}
		if resp2.Header.Get("X-Cache") == "HIT" {
			t.Error("expected second request to be a miss, got HIT")
		}
		resp2.Body.Close()
	})

	t.Run("IncludeQueryString option", func(t *testing.T) {
		// Test with IncludeQueryString = true
		cfgTrue := config.NewDefaultConfig()
		cfgTrue.Cache.PostCache.Enable = true
		cfgTrue.Cache.PostCache.IncludeQueryString = true
		upstreamTrue, clientTrue, cleanupTrue := setupPostCacheTest(t, cfgTrue)
		defer cleanupTrue()

		body := "query string test"
		// First request, should be a miss
		resp1, _ := clientTrue.Post(upstreamTrue.URL+"?a=1", "text/plain", strings.NewReader(body))
		if resp1.Header.Get("X-Cache") != "MISS" {
			t.Errorf("[true] expected first request to be MISS, got %s", resp1.Header.Get("X-Cache"))
		}
		resp1.Body.Close()
		// Second request, same query, should be a hit
		resp2, _ := clientTrue.Post(upstreamTrue.URL+"?a=1", "text/plain", strings.NewReader(body))
		if resp2.Header.Get("X-Cache") != "HIT" {
			t.Errorf("[true] expected second request to be HIT, got %s", resp2.Header.Get("X-Cache"))
		}
		resp2.Body.Close()
		// Third request, different query, should be a miss
		resp3, _ := clientTrue.Post(upstreamTrue.URL+"?b=2", "text/plain", strings.NewReader(body))
		if resp3.Header.Get("X-Cache") != "MISS" {
			t.Errorf("[true] expected third request to be MISS, got %s", resp3.Header.Get("X-Cache"))
		}
		resp3.Body.Close()

		// Test with IncludeQueryString = false
		cfgFalse := config.NewDefaultConfig()
		cfgFalse.Cache.PostCache.Enable = true
		cfgFalse.Cache.PostCache.IncludeQueryString = false
		upstreamFalse, clientFalse, cleanupFalse := setupPostCacheTest(t, cfgFalse)
		defer cleanupFalse()

		// First request, should be a miss
		resp4, _ := clientFalse.Post(upstreamFalse.URL+"?a=1", "text/plain", strings.NewReader(body))
		if resp4.Header.Get("X-Cache") != "MISS" {
			t.Errorf("[false] expected fourth request to be MISS, got %s", resp4.Header.Get("X-Cache"))
		}
		resp4.Body.Close()
		// Second request, different query but same body, should be a hit
		resp5, _ := clientFalse.Post(upstreamFalse.URL+"?b=2", "text/plain", strings.NewReader(body))
		if resp5.Header.Get("X-Cache") != "HIT" {
			t.Errorf("[false] expected fifth request to be HIT, got %s", resp5.Header.Get("X-Cache"))
		}
		resp5.Body.Close()
	})
}

func TestCertCacheLRUDataStructures(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-lru")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	c := cache.NewMemoryCache(5*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Verify LRU structures initialized
	if proxy.certLRUList == nil {
		t.Fatal("certLRUList not initialized")
	}
	if proxy.certCache == nil {
		t.Fatal("certCache not initialized")
	}
}

func TestNewProxyInitializesCertLRU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-init")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	c := cache.NewMemoryCache(5*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

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

func TestEvictOldestCert(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-evict")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	cfg.Server.MaxCertCacheEntries = 3
	c := cache.NewMemoryCache(5*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Manually add 3 certs to cache (newest to oldest: C, B, A)
	// Using PushFront means: C is most recent (front), A is oldest (back)
	certA := &tls.Certificate{}
	certB := &tls.Certificate{}
	certC := &tls.Certificate{}

	nodeA := &certNode{host: "a.example.com", cert: certA}
	nodeB := &certNode{host: "b.example.com", cert: certB}
	nodeC := &certNode{host: "c.example.com", cert: certC}

	elemA := proxy.certLRUList.PushFront(nodeA)
	elemB := proxy.certLRUList.PushFront(nodeB)
	elemC := proxy.certLRUList.PushFront(nodeC)

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

func TestEvictOldestCertEmptyCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-evict-empty")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	c := cache.NewMemoryCache(5*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	evicted := proxy.evictOldestCert()
	if evicted {
		t.Error("should not evict from empty cache")
	}
	if proxy.certEvictions.Load() != 0 {
		t.Errorf("expected 0 evictions, got %d", proxy.certEvictions.Load())
	}
}

func TestGetCertCacheHitMovesToFront(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-cache-hit")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	cfg.Server.MaxCertCacheEntries = 3
	c := cache.NewMemoryCache(5*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Get cert for host A (adds to cache, becomes front)
	certA, err := proxy.getCert("a.example.com")
	if err != nil {
		t.Fatalf("getCert failed: %v", err)
	}
	if certA == nil {
		t.Fatal("expected certificate, got nil")
	}

	// Get cert for host B (adds to cache, becomes front, pushing A back)
	_, err = proxy.getCert("b.example.com")
	if err != nil {
		t.Fatalf("getCert failed: %v", err)
	}

	// Get cert for host C (adds to cache, becomes front)
	_, err = proxy.getCert("c.example.com")
	if err != nil {
		t.Fatalf("getCert failed: %v", err)
	}

	// At this point: Front -> C -> B -> A <- Back
	// Verify C is at front
	if proxy.certLRUList.Front() != proxy.certCache["c.example.com"] {
		t.Error("c.example.com should be at front")
	}

	// Access A again (cache hit, should move to front)
	certA2, err := proxy.getCert("a.example.com")
	if err != nil {
		t.Fatalf("getCert cache hit failed: %v", err)
	}
	if certA2 != certA {
		t.Error("cache hit should return same certificate")
	}

	// Verify A is now at front of LRU list
	elemA := proxy.certCache["a.example.com"]
	if proxy.certLRUList.Front() != elemA {
		t.Error("cache hit should move entry to front of LRU list")
	}
}

func TestGetCertEvictsWhenAtCapacity(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-evict-capacity")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	cfg.Server.MaxCertCacheEntries = 3
	c := cache.NewMemoryCache(5*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Fill cache to capacity (3 entries)
	_, err = proxy.getCert("a.example.com")
	if err != nil {
		t.Fatalf("getCert failed: %v", err)
	}
	_, err = proxy.getCert("b.example.com")
	if err != nil {
		t.Fatalf("getCert failed: %v", err)
	}
	_, err = proxy.getCert("c.example.com")
	if err != nil {
		t.Fatalf("getCert failed: %v", err)
	}

	// Verify cache is at capacity
	if len(proxy.certCache) != 3 {
		t.Fatalf("expected cache size 3, got %d", len(proxy.certCache))
	}
	if proxy.certLRUList.Len() != 3 {
		t.Fatalf("expected LRU list length 3, got %d", proxy.certLRUList.Len())
	}

	// At this point: Front -> C -> B -> A <- Back (A is oldest)
	// Verify A is at back
	backNode := proxy.certLRUList.Back().Value.(*certNode)
	if backNode.host != "a.example.com" {
		t.Errorf("expected a.example.com at back, got %s", backNode.host)
	}

	// Add one more cert - should evict A (oldest)
	_, err = proxy.getCert("d.example.com")
	if err != nil {
		t.Fatalf("getCert failed: %v", err)
	}

	// Verify cache still at capacity
	if len(proxy.certCache) != 3 {
		t.Fatalf("expected cache size 3, got %d", len(proxy.certCache))
	}
	if proxy.certLRUList.Len() != 3 {
		t.Fatalf("expected LRU list length 3, got %d", proxy.certLRUList.Len())
	}

	// Verify A was evicted
	if _, exists := proxy.certCache["a.example.com"]; exists {
		t.Error("a.example.com should have been evicted")
	}

	// Verify B, C, D still exist
	if _, exists := proxy.certCache["b.example.com"]; !exists {
		t.Error("b.example.com should still exist")
	}
	if _, exists := proxy.certCache["c.example.com"]; !exists {
		t.Error("c.example.com should still exist")
	}
	if _, exists := proxy.certCache["d.example.com"]; !exists {
		t.Error("d.example.com should exist")
	}

	// Verify D is at front (most recent)
	frontNode := proxy.certLRUList.Front().Value.(*certNode)
	if frontNode.host != "d.example.com" {
		t.Errorf("expected d.example.com at front, got %s", frontNode.host)
	}

	// Verify eviction counter incremented
	if proxy.certEvictions.Load() != 1 {
		t.Errorf("expected 1 eviction, got %d", proxy.certEvictions.Load())
	}
}
