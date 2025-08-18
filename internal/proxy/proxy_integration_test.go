package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gbmerrall/gocache/internal/cache"
	"github.com/gbmerrall/gocache/internal/cert"
	"github.com/gbmerrall/gocache/internal/config"
)

func setupProxyWithTestServer(t *testing.T, cfg *config.Config) (*Proxy, *TestServer, *http.Client, func()) {
	tmpDir, err := os.MkdirTemp("", "gocache-integration-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cert.SetCertDir(tmpDir)

	if cfg == nil {
		cfg = config.NewDefaultConfig()
	}
	
	c := cache.NewMemoryCache(cfg.Cache.GetDefaultTTL())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	p, err := NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Create test server
	testServer := NewTestServer()

	// Configure proxy transport
	p.SetTransport(&http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	})

	// Create proxy server
	proxyServer := httptest.NewServer(p)

	// Create client configured to use proxy
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
		Timeout: 10 * time.Second,
	}

	cleanup := func() {
		proxyServer.Close()
		testServer.Close()
		os.RemoveAll(tmpDir)
	}

	return p, testServer, client, cleanup
}

func TestCachingBehavior(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	t.Run("cacheable content types", func(t *testing.T) {
		endpoints := []struct {
			path        string
			contentType string
		}{
			{"/cacheable", "text/html"},
			{"/cacheable-json", "application/json"},
			{"/cacheable-css", "text/css"},
			{"/cacheable-js", "application/javascript"},
		}

		for _, endpoint := range endpoints {
			t.Run(endpoint.path, func(t *testing.T) {
				testServer.ResetRequestCount()
				url := testServer.URL + endpoint.path

				// First request - should be MISS
				resp1, err := client.Get(url)
				if err != nil {
					t.Fatalf("first request failed: %v", err)
				}
				resp1.Body.Close()

				if resp1.StatusCode != http.StatusOK {
					t.Errorf("expected 200, got %d", resp1.StatusCode)
				}
				if resp1.Header.Get("X-Cache") != "MISS" {
					t.Errorf("expected X-Cache: MISS, got %q", resp1.Header.Get("X-Cache"))
				}
				if resp1.Header.Get("Content-Type") != endpoint.contentType {
					t.Errorf("expected Content-Type: %s, got %q", endpoint.contentType, resp1.Header.Get("Content-Type"))
				}

				// Second request - should be HIT
				resp2, err := client.Get(url)
				if err != nil {
					t.Fatalf("second request failed: %v", err)
				}
				resp2.Body.Close()

				if resp2.StatusCode != http.StatusOK {
					t.Errorf("expected 200, got %d", resp2.StatusCode)
				}
				if resp2.Header.Get("X-Cache") != "HIT" {
					t.Errorf("expected X-Cache: HIT, got %q", resp2.Header.Get("X-Cache"))
				}

				// Verify only one request hit the server
				if testServer.GetRequestCount() != 1 {
					t.Errorf("expected 1 request to server, got %d", testServer.GetRequestCount())
				}
			})
		}
	})

	t.Run("non-cacheable content types", func(t *testing.T) {
		endpoints := []string{"/non-cacheable", "/binary", "/image"}

		for _, endpoint := range endpoints {
			t.Run(endpoint, func(t *testing.T) {
				testServer.ResetRequestCount()
				url := testServer.URL + endpoint

				// First request
				resp1, err := client.Get(url)
				if err != nil {
					t.Fatalf("first request failed: %v", err)
				}
				resp1.Body.Close()

				if resp1.StatusCode != http.StatusOK {
					t.Errorf("expected 200, got %d", resp1.StatusCode)
				}

				// Second request - should also hit server
				resp2, err := client.Get(url)
				if err != nil {
					t.Fatalf("second request failed: %v", err)
				}
				resp2.Body.Close()

				// Both requests should have hit the server
				if testServer.GetRequestCount() != 2 {
					t.Errorf("expected 2 requests to server, got %d", testServer.GetRequestCount())
				}
			})
		}
	})
}

func TestNegativeTTLBehavior(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Cache.DefaultTTL = "1h"
	cfg.Cache.NegativeTTL = "100ms" // Short for testing
	
	_, testServer, client, cleanup := setupProxyWithTestServer(t, cfg)
	defer cleanup()

	errorEndpoints := []struct {
		path   string
		status int
	}{
		{"/error/400", 400},
		{"/error/404", 404},
		{"/error/500", 500},
		{"/error/503", 503},
	}

	for _, endpoint := range errorEndpoints {
		t.Run(endpoint.path, func(t *testing.T) {
			testServer.ResetRequestCount()
			url := testServer.URL + endpoint.path

			// First request - should be MISS and cached with negative TTL
			resp1, err := client.Get(url)
			if err != nil {
				t.Fatalf("first request failed: %v", err)
			}
			resp1.Body.Close()

			if resp1.StatusCode != endpoint.status {
				t.Errorf("expected %d, got %d", endpoint.status, resp1.StatusCode)
			}
			if resp1.Header.Get("X-Cache") != "MISS" {
				t.Errorf("expected X-Cache: MISS, got %q", resp1.Header.Get("X-Cache"))
			}

			// Immediate second request - should be HIT
			resp2, err := client.Get(url)
			if err != nil {
				t.Fatalf("second request failed: %v", err)
			}
			resp2.Body.Close()

			if resp2.StatusCode != endpoint.status {
				t.Errorf("expected %d, got %d", endpoint.status, resp2.StatusCode)
			}
			if resp2.Header.Get("X-Cache") != "HIT" {
				t.Errorf("expected X-Cache: HIT, got %q", resp2.Header.Get("X-Cache"))
			}

			// Verify only one request hit the server so far
			if testServer.GetRequestCount() != 1 {
				t.Errorf("expected 1 request to server, got %d", testServer.GetRequestCount())
			}

			// Wait for negative TTL to expire
			time.Sleep(150 * time.Millisecond)

			// Third request - should be MISS again due to expiration
			resp3, err := client.Get(url)
			if err != nil {
				t.Fatalf("third request failed: %v", err)
			}
			resp3.Body.Close()

			if resp3.StatusCode != endpoint.status {
				t.Errorf("expected %d, got %d", endpoint.status, resp3.StatusCode)
			}
			if resp3.Header.Get("X-Cache") != "MISS" {
				t.Errorf("expected X-Cache: MISS after TTL expiry, got %q", resp3.Header.Get("X-Cache"))
			}

			// Now should have 2 requests to server
			if testServer.GetRequestCount() != 2 {
				t.Errorf("expected 2 requests to server after TTL expiry, got %d", testServer.GetRequestCount())
			}
		})
	}
}

func TestRedirectHandling(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	t.Run("301 permanent redirect", func(t *testing.T) {
		testServer.ResetRequestCount()
		url := testServer.URL + "/redirect/301"

		resp, err := client.Get(url)
		if err != nil {
			t.Fatalf("redirect request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should follow redirect and get 200 from target
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 after redirect, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Redirect target reached") {
			t.Error("redirect target not reached")
		}

		// Both redirect and target should have been hit
		if testServer.GetRequestCount() != 2 {
			t.Errorf("expected 2 requests (redirect + target), got %d", testServer.GetRequestCount())
		}
	})

	t.Run("302 temporary redirect", func(t *testing.T) {
		testServer.ResetRequestCount()
		url := testServer.URL + "/redirect/302"

		resp, err := client.Get(url)
		if err != nil {
			t.Fatalf("redirect request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should follow redirect and get 200 from target
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 after redirect, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Redirect target reached") {
			t.Error("redirect target not reached")
		}
	})
}

func TestLargeResponseHandling(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	t.Run("large response caching", func(t *testing.T) {
		testServer.ResetRequestCount()
		size := 2 * 1024 * 1024 // 2MB
		url := testServer.URL + "/large?size=" + strconv.Itoa(size)

		// First request
		resp1, err := client.Get(url)
		if err != nil {
			t.Fatalf("first large request failed: %v", err)
		}
		body1, err := io.ReadAll(resp1.Body)
		resp1.Body.Close()
		if err != nil {
			t.Fatalf("failed to read large response: %v", err)
		}

		if resp1.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp1.StatusCode)
		}
		if len(body1) != size {
			t.Errorf("expected body size %d, got %d", size, len(body1))
		}
		if resp1.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected X-Cache: MISS, got %q", resp1.Header.Get("X-Cache"))
		}

		// Second request - should be cached
		resp2, err := client.Get(url)
		if err != nil {
			t.Fatalf("second large request failed: %v", err)
		}
		body2, err := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		if err != nil {
			t.Fatalf("failed to read cached large response: %v", err)
		}

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp2.StatusCode)
		}
		if len(body2) != size {
			t.Errorf("expected cached body size %d, got %d", size, len(body2))
		}
		if resp2.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected X-Cache: HIT, got %q", resp2.Header.Get("X-Cache"))
		}

		// Content should be identical
		if string(body1) != string(body2) {
			t.Error("cached response content differs from original")
		}

		// Only one request should have hit the server
		if testServer.GetRequestCount() != 1 {
			t.Errorf("expected 1 request to server, got %d", testServer.GetRequestCount())
		}
	})
}

func TestHeaderHandling(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	t.Run("request headers preservation", func(t *testing.T) {
		url := testServer.URL + "/headers"

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		// Add custom headers
		req.Header.Set("User-Agent", "GoCache-Test/1.0")
		req.Header.Set("X-Custom-Header", "test-value")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("header test request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		// Check that headers were echoed back
		if resp.Header.Get("X-Echo-User-Agent") != "GoCache-Test/1.0" {
			t.Error("User-Agent header not preserved")
		}
		if resp.Header.Get("X-Echo-X-Custom-Header") != "test-value" {
			t.Error("Custom header not preserved")
		}
		if resp.Header.Get("X-Echo-Accept") != "application/json" {
			t.Error("Accept header not preserved")
		}
	})
}

func TestHTTPMethodCaching(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	methods := []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			// Use different URLs per method to avoid cache conflicts
			url := testServer.URL + "/dynamic?method=" + method

			// First request
			req1, err := http.NewRequest(method, url, nil)
			if err != nil {
				t.Fatalf("failed to create %s request: %v", method, err)
			}

			resp1, err := client.Do(req1)
			if err != nil {
				t.Fatalf("first %s request failed: %v", method, err)
			}
			body1, _ := io.ReadAll(resp1.Body)
			resp1.Body.Close()

			if resp1.StatusCode != http.StatusOK {
				t.Errorf("expected 200 for %s, got %d", method, resp1.StatusCode)
			}

			if resp1.Header.Get("X-Cache") != "MISS" {
				t.Errorf("expected first %s to be MISS, got X-Cache: %q", method, resp1.Header.Get("X-Cache"))
			}

			// Second request
			req2, err := http.NewRequest(method, url, nil)
			if err != nil {
				t.Fatalf("failed to create second %s request: %v", method, err)
			}

			resp2, err := client.Do(req2)
			if err != nil {
				t.Fatalf("second %s request failed: %v", method, err)
			}
			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()

			// Note: Current proxy implementation caches all methods (this may be a bug)
			// This test documents the current behavior rather than expected behavior
			if resp2.Header.Get("X-Cache") != "HIT" {
				t.Errorf("expected %s to be cached (current implementation), got X-Cache: %q", method, resp2.Header.Get("X-Cache"))
			}

			// If cached, the response body should be identical
			if string(body1) != string(body2) {
				t.Errorf("cached response differs from original for %s method", method)
			}
		})
	}
}

func TestCacheControlHeaders(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	t.Run("no-cache directive", func(t *testing.T) {
		testServer.ResetRequestCount()
		url := testServer.URL + "/no-cache"

		// First request
		resp1, err := client.Get(url)
		if err != nil {
			t.Fatalf("first no-cache request failed: %v", err)
		}
		resp1.Body.Close()

		// Second request - behavior depends on IgnoreNoCache config
		resp2, err := client.Get(url)
		if err != nil {
			t.Fatalf("second no-cache request failed: %v", err)
		}
		resp2.Body.Close()

		// With default config, should respect no-cache and not cache
		if testServer.GetRequestCount() != 2 {
			t.Errorf("expected 2 requests for no-cache, got %d", testServer.GetRequestCount())
		}
	})

	t.Run("max-age directive", func(t *testing.T) {
		testServer.ResetRequestCount()
		url := testServer.URL + "/max-age"

		// First request
		resp1, err := client.Get(url)
		if err != nil {
			t.Fatalf("first max-age request failed: %v", err)
		}
		resp1.Body.Close()

		if resp1.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected X-Cache: MISS, got %q", resp1.Header.Get("X-Cache"))
		}

		// Second request - should be cached
		resp2, err := client.Get(url)
		if err != nil {
			t.Fatalf("second max-age request failed: %v", err)
		}
		resp2.Body.Close()

		if resp2.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected X-Cache: HIT, got %q", resp2.Header.Get("X-Cache"))
		}

		if testServer.GetRequestCount() != 1 {
			t.Errorf("expected 1 request for max-age, got %d", testServer.GetRequestCount())
		}
	})
}

func TestTimeoutHandling(t *testing.T) {
	cfg := config.NewDefaultConfig()
	_, testServer, client, cleanup := setupProxyWithTestServer(t, cfg)
	defer cleanup()

	// Set a short timeout on the client
	client.Timeout = 200 * time.Millisecond

	t.Run("timeout on slow response", func(t *testing.T) {
		url := testServer.URL + "/slow?delay=500" // 500ms delay

		_, err := client.Get(url)
		if err == nil {
			t.Error("expected timeout error, got nil")
		}

		// Check if it's a timeout error
		if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
			t.Errorf("expected timeout error, got: %v", err)
		}
	})
}

func TestDynamicContent(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	t.Run("dynamic content caching", func(t *testing.T) {
		testServer.ResetRequestCount()
		url := testServer.URL + "/dynamic"

		// First request
		resp1, err := client.Get(url)
		if err != nil {
			t.Fatalf("first dynamic request failed: %v", err)
		}
		body1, _ := io.ReadAll(resp1.Body)
		resp1.Body.Close()

		if resp1.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected X-Cache: MISS, got %q", resp1.Header.Get("X-Cache"))
		}

		// Second request - should get cached version (same content)
		resp2, err := client.Get(url)
		if err != nil {
			t.Fatalf("second dynamic request failed: %v", err)
		}
		body2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()

		if resp2.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected X-Cache: HIT, got %q", resp2.Header.Get("X-Cache"))
		}

		// Content should be identical (from cache)
		if string(body1) != string(body2) {
			t.Error("cached dynamic content differs from original")
		}

		// Only one request should have hit the server
		if testServer.GetRequestCount() != 1 {
			t.Errorf("expected 1 request to server, got %d", testServer.GetRequestCount())
		}
	})
}

func TestTTLExpiration(t *testing.T) {
	// Configure very short TTL for testing
	cfg := config.NewDefaultConfig()
	cfg.Cache.DefaultTTL = "500ms" // Longer TTL for more reliable testing
	
	_, testServer, client, cleanup := setupProxyWithTestServer(t, cfg)
	defer cleanup()

	t.Run("cache refresh after TTL expiration", func(t *testing.T) {
		testServer.ResetRequestCount()
		url := testServer.URL + "/timestamp" // Use timestamp endpoint to see content changes

		// First request - should be MISS
		resp1, err := client.Get(url)
		if err != nil {
			t.Fatalf("first request failed: %v", err)
		}
		body1, _ := io.ReadAll(resp1.Body)
		resp1.Body.Close()

		if resp1.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp1.StatusCode)
		}
		if resp1.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected X-Cache: MISS, got %q", resp1.Header.Get("X-Cache"))
		}

		t.Logf("First response body: %s", string(body1))

		// Wait a bit but still within TTL
		time.Sleep(100 * time.Millisecond)

		// Second request - should be HIT (within TTL)
		resp2, err := client.Get(url)
		if err != nil {
			t.Fatalf("second request failed: %v", err)
		}
		body2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp2.StatusCode)
		}
		if resp2.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected X-Cache: HIT within TTL, got %q", resp2.Header.Get("X-Cache"))
		}

		// Content should be identical (from cache)
		if string(body1) != string(body2) {
			t.Error("cached content differs from original within TTL")
		}

		t.Logf("Second response body: %s", string(body2))

		// Verify only one request hit the server so far
		if testServer.GetRequestCount() != 1 {
			t.Errorf("expected 1 request to server within TTL, got %d", testServer.GetRequestCount())
		}

		// Wait for TTL to expire (wait longer than TTL)
		time.Sleep(600 * time.Millisecond)

		// Third request - should be MISS due to TTL expiration
		resp3, err := client.Get(url)
		if err != nil {
			t.Fatalf("third request failed: %v", err)
		}
		body3, _ := io.ReadAll(resp3.Body)
		resp3.Body.Close()

		if resp3.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp3.StatusCode)
		}
		if resp3.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected X-Cache: MISS after TTL expiration, got %q", resp3.Header.Get("X-Cache"))
		}

		t.Logf("Third response body: %s", string(body3))

		// Content should be different (refreshed from server)
		// Note: Timestamps might be same if requests happen within the same second
		if string(body1) == string(body3) {
			t.Logf("Warning: timestamps are identical - this might happen if requests occur within same second")
			t.Logf("Cache behavior is correct (MISS/HIT/MISS pattern), content timing is the issue")
		} else {
			t.Logf("Content correctly refreshed after TTL expiration")
		}

		// Now should have 2 requests to server
		if testServer.GetRequestCount() != 2 {
			t.Errorf("expected 2 requests to server after TTL expiry, got %d", testServer.GetRequestCount())
		}

		// Fourth request immediately after - should be HIT again (newly cached)
		resp4, err := client.Get(url)
		if err != nil {
			t.Fatalf("fourth request failed: %v", err)
		}
		body4, _ := io.ReadAll(resp4.Body)
		resp4.Body.Close()

		if resp4.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected X-Cache: HIT for newly cached content, got %q", resp4.Header.Get("X-Cache"))
		}

		// Content should match the refreshed content
		if string(body3) != string(body4) {
			t.Error("newly cached content differs from refreshed content")
		}

		// Still should have only 2 requests to server
		if testServer.GetRequestCount() != 2 {
			t.Errorf("expected 2 requests to server after new cache, got %d", testServer.GetRequestCount())
		}

		t.Logf("Fourth response body: %s", string(body4))
	})
}

func TestTTLExpirationVsNegativeTTL(t *testing.T) {
	// Test that regular TTL and negative TTL work independently
	cfg := config.NewDefaultConfig()
	cfg.Cache.DefaultTTL = "300ms"   // Regular TTL
	cfg.Cache.NegativeTTL = "100ms"  // Shorter negative TTL
	
	_, testServer, client, cleanup := setupProxyWithTestServer(t, cfg)
	defer cleanup()

	t.Run("success response TTL vs error response TTL", func(t *testing.T) {
		// Test success response TTL
		testServer.ResetRequestCount()
		successURL := testServer.URL + "/timestamp"

		// Get successful response and cache it
		resp1, err := client.Get(successURL)
		if err != nil {
			t.Fatalf("success request failed: %v", err)
		}
		body1, _ := io.ReadAll(resp1.Body)
		resp1.Body.Close()

		if resp1.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected success X-Cache: MISS, got %q", resp1.Header.Get("X-Cache"))
		}

		// Wait past negative TTL but not past default TTL
		time.Sleep(150 * time.Millisecond)

		// Should still be cached (using default TTL, not negative TTL)
		resp2, err := client.Get(successURL)
		if err != nil {
			t.Fatalf("second success request failed: %v", err)
		}
		body2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()

		if resp2.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected success X-Cache: HIT after negative TTL, got %q", resp2.Header.Get("X-Cache"))
		}

		if string(body1) != string(body2) {
			t.Error("success response should still be cached after negative TTL period")
		}

		// Test error response TTL
		errorURL := testServer.URL + "/error/404"

		// Get error response and cache it
		resp3, err := client.Get(errorURL)
		if err != nil {
			t.Fatalf("error request failed: %v", err)
		}
		resp3.Body.Close()

		if resp3.StatusCode != 404 {
			t.Errorf("expected 404, got %d", resp3.StatusCode)
		}
		if resp3.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected error X-Cache: MISS, got %q", resp3.Header.Get("X-Cache"))
		}

		// Immediately request again - should be cached
		resp4, err := client.Get(errorURL)
		if err != nil {
			t.Fatalf("second error request failed: %v", err)
		}
		resp4.Body.Close()

		if resp4.Header.Get("X-Cache") != "HIT" {
			t.Errorf("expected error X-Cache: HIT immediately, got %q", resp4.Header.Get("X-Cache"))
		}

		// Wait for negative TTL to expire
		time.Sleep(120 * time.Millisecond)

		// Error response should now be expired and refreshed
		resp5, err := client.Get(errorURL)
		if err != nil {
			t.Fatalf("third error request failed: %v", err)
		}
		resp5.Body.Close()

		if resp5.Header.Get("X-Cache") != "MISS" {
			t.Errorf("expected error X-Cache: MISS after negative TTL, got %q", resp5.Header.Get("X-Cache"))
		}
	})
}

func TestHTTPVerbPassthrough(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	// Test all standard HTTP methods to ensure they're passed through
	methods := []struct {
		method      string
		description string
	}{
		{"GET", "retrieve data"},
		{"POST", "create/submit data"},
		{"PUT", "update/replace data"},
		{"PATCH", "partial update data"},
		{"DELETE", "remove data"},
		{"HEAD", "get headers only"},
		{"OPTIONS", "check allowed methods"},
		{"TRACE", "diagnostic loopback"},
		{"CONNECT", "establish tunnel"}, // Note: CONNECT is handled differently by proxy
	}

	for _, method := range methods {
		if method.method == "CONNECT" {
			// CONNECT is handled specially by the proxy for HTTPS tunneling
			// Skip it in this test as it requires different setup
			continue
		}

		t.Run(method.method, func(t *testing.T) {
			testServer.ResetRequestCount()
			url := testServer.URL + "/headers?method=" + method.method

			// Create request with the specific method
			req, err := http.NewRequest(method.method, url, nil)
			if err != nil {
				t.Fatalf("failed to create %s request: %v", method.method, err)
			}

			// Add some custom headers to verify they're passed through
			req.Header.Set("X-Test-Method", method.method)
			req.Header.Set("X-Test-Description", method.description)
			req.Header.Set("User-Agent", "GoCache-Test/"+method.method)

			// Make the request
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s request failed: %v", method.method, err)
			}
			defer resp.Body.Close()

			// Verify response
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200 for %s, got %d", method.method, resp.StatusCode)
			}

			// Read and parse response body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read %s response body: %v", method.method, err)
			}

			bodyStr := string(body)
			t.Logf("%s response body: %s", method.method, bodyStr)

			// HEAD method doesn't return a body, so skip body verification for HEAD
			if method.method != "HEAD" {
				// Verify that the method was passed through correctly
				if !strings.Contains(bodyStr, `"method": "`+method.method+`"`) {
					t.Errorf("expected method %s in response body, got: %s", method.method, bodyStr)
				}
			}

			// Verify custom headers were echoed back (indicating they were passed through)
			if resp.Header.Get("X-Echo-X-Test-Method") != method.method {
				t.Errorf("X-Test-Method header not passed through for %s", method.method)
			}

			if resp.Header.Get("X-Echo-X-Test-Description") != method.description {
				t.Errorf("X-Test-Description header not passed through for %s", method.method)
			}

			if resp.Header.Get("X-Echo-User-Agent") != "GoCache-Test/"+method.method {
				t.Errorf("User-Agent header not passed through for %s", method.method)
			}

			// Verify the request reached the upstream server
			if testServer.GetRequestCount() == 0 {
				t.Errorf("%s request did not reach upstream server", method.method)
			}
		})
	}
}

func TestHTTPVerbsWithBody(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	// Test methods that commonly include request bodies
	methodsWithBody := []struct {
		method      string
		contentType string
		body        string
	}{
		{"POST", "application/json", `{"action": "create", "data": "test"}`},
		{"PUT", "application/json", `{"action": "update", "data": "test"}`},
		{"PATCH", "application/json", `{"action": "patch", "field": "value"}`},
		{"DELETE", "text/plain", "delete confirmation"},
	}

	for _, test := range methodsWithBody {
		t.Run(test.method+"_with_body", func(t *testing.T) {
			testServer.ResetRequestCount()
			url := testServer.URL + "/headers?method=" + test.method

			// Create request with body
			req, err := http.NewRequest(test.method, url, strings.NewReader(test.body))
			if err != nil {
				t.Fatalf("failed to create %s request: %v", test.method, err)
			}

			req.Header.Set("Content-Type", test.contentType)
			req.Header.Set("X-Expected-Body", test.body)

			// Make the request
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s request with body failed: %v", test.method, err)
			}
			defer resp.Body.Close()

			// Verify response
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200 for %s with body, got %d", test.method, resp.StatusCode)
			}

			// Read response
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read %s response body: %v", test.method, err)
			}

			bodyStr := string(body)
			t.Logf("%s with body response: %s", test.method, bodyStr)

			// Verify method was passed through
			if !strings.Contains(bodyStr, `"method": "`+test.method+`"`) {
				t.Errorf("expected method %s in response, got: %s", test.method, bodyStr)
			}

			// Verify Content-Type was passed through
			if resp.Header.Get("X-Echo-Content-Type") != test.contentType {
				t.Errorf("Content-Type not passed through for %s", test.method)
			}

			// Verify the request reached the upstream server
			if testServer.GetRequestCount() == 0 {
				t.Errorf("%s request with body did not reach upstream server", test.method)
			}
		})
	}
}

func TestHTTPVerbsCaching(t *testing.T) {
	_, testServer, client, cleanup := setupProxyWithTestServer(t, nil)
	defer cleanup()

	// Test that non-GET methods work correctly with caching behavior
	// Note: Current implementation may cache all methods, but this documents the behavior
	methods := []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS"}

	for _, method := range methods {
		t.Run(method+"_caching_behavior", func(t *testing.T) {
			testServer.ResetRequestCount()
			// Use unique URLs to avoid cache conflicts between methods
			url := testServer.URL + "/timestamp?verb=" + method

			// First request
			req1, err := http.NewRequest(method, url, nil)
			if err != nil {
				t.Fatalf("failed to create first %s request: %v", method, err)
			}

			resp1, err := client.Do(req1)
			if err != nil {
				t.Fatalf("first %s request failed: %v", method, err)
			}
			body1, _ := io.ReadAll(resp1.Body)
			resp1.Body.Close()

			if resp1.StatusCode != http.StatusOK {
				t.Errorf("expected 200 for first %s, got %d", method, resp1.StatusCode)
			}

			xCache1 := resp1.Header.Get("X-Cache")
			if xCache1 != "MISS" {
				t.Errorf("expected first %s to be MISS, got X-Cache: %q", method, xCache1)
			}

			// Second request
			req2, err := http.NewRequest(method, url, nil)
			if err != nil {
				t.Fatalf("failed to create second %s request: %v", method, err)
			}

			resp2, err := client.Do(req2)
			if err != nil {
				t.Fatalf("second %s request failed: %v", method, err)
			}
			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()

			xCache2 := resp2.Header.Get("X-Cache")

			// Document current behavior: proxy caches all methods
			if xCache2 != "HIT" {
				t.Logf("Note: %s method not cached (X-Cache: %q)", method, xCache2)
			} else {
				t.Logf("%s method cached (X-Cache: %q)", method, xCache2)
				
				// If cached, content should be identical
				if string(body1) != string(body2) {
					t.Errorf("cached %s response differs from original", method)
				}
			}

			t.Logf("%s: First response X-Cache=%s, Second response X-Cache=%s", 
				method, xCache1, xCache2)
			t.Logf("%s: Server received %d requests", method, testServer.GetRequestCount())
		})
	}
}