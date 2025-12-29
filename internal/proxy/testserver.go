package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// TestServer provides a comprehensive test server for proxy testing
type TestServer struct {
	*httptest.Server
	requestCount int64
	delayMS      int64
}

// NewTestServer creates a new test server with predefined endpoints for testing various scenarios
func NewTestServer() *TestServer {
	ts := &TestServer{}

	mux := http.NewServeMux()

	// Basic cacheable content
	mux.HandleFunc("/cacheable", ts.handleCacheable)
	mux.HandleFunc("/cacheable-json", ts.handleCacheableJSON)
	mux.HandleFunc("/cacheable-css", ts.handleCacheableCSS)
	mux.HandleFunc("/cacheable-js", ts.handleCacheableJS)

	// Non-cacheable content
	mux.HandleFunc("/non-cacheable", ts.handleNonCacheable)
	mux.HandleFunc("/binary", ts.handleBinary)
	mux.HandleFunc("/image", ts.handleImage)

	// Error responses
	mux.HandleFunc("/error/400", ts.handleError400)
	mux.HandleFunc("/error/404", ts.handleError404)
	mux.HandleFunc("/error/500", ts.handleError500)
	mux.HandleFunc("/error/503", ts.handleError503)

	// Redirects
	mux.HandleFunc("/redirect/301", ts.handleRedirect301)
	mux.HandleFunc("/redirect/302", ts.handleRedirect302)
	mux.HandleFunc("/redirect/target", ts.handleRedirectTarget)

	// Cache control headers
	mux.HandleFunc("/no-cache", ts.handleNoCache)
	mux.HandleFunc("/max-age", ts.handleMaxAge)
	mux.HandleFunc("/expires", ts.handleExpires)

	// Dynamic content (changes on each request)
	mux.HandleFunc("/dynamic", ts.handleDynamic)
	mux.HandleFunc("/timestamp", ts.handleTimestamp)

	// Slow responses for timeout testing
	mux.HandleFunc("/slow", ts.handleSlow)

	// Large responses
	mux.HandleFunc("/large", ts.handleLarge)

	// Custom headers testing
	mux.HandleFunc("/headers", ts.handleHeaders)

	// Request counting
	mux.HandleFunc("/counter", ts.handleCounter)

	ts.Server = httptest.NewServer(mux)
	return ts
}

// NewTestTLSServer creates a new TLS test server
func NewTestTLSServer() *TestServer {
	ts := &TestServer{}

	mux := http.NewServeMux()

	// Same handlers as HTTP version
	mux.HandleFunc("/cacheable", ts.handleCacheable)
	mux.HandleFunc("/cacheable-json", ts.handleCacheableJSON)
	mux.HandleFunc("/non-cacheable", ts.handleNonCacheable)
	mux.HandleFunc("/error/404", ts.handleError404)
	mux.HandleFunc("/error/500", ts.handleError500)
	mux.HandleFunc("/redirect/301", ts.handleRedirect301)
	mux.HandleFunc("/dynamic", ts.handleDynamic)
	mux.HandleFunc("/slow", ts.handleSlow)
	mux.HandleFunc("/counter", ts.handleCounter)

	ts.Server = httptest.NewTLSServer(mux)
	return ts
}

// SetDelay sets an artificial delay for responses (useful for timeout testing)
func (ts *TestServer) SetDelay(ms int) {
	atomic.StoreInt64(&ts.delayMS, int64(ms))
}

// GetRequestCount returns the total number of requests received
func (ts *TestServer) GetRequestCount() int64 {
	return atomic.LoadInt64(&ts.requestCount)
}

// ResetRequestCount resets the request counter
func (ts *TestServer) ResetRequestCount() {
	atomic.StoreInt64(&ts.requestCount, 0)
}

func (ts *TestServer) applyDelay() {
	if delay := atomic.LoadInt64(&ts.delayMS); delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

func (ts *TestServer) incrementCounter() {
	atomic.AddInt64(&ts.requestCount, 1)
}

// Cacheable HTML content
func (ts *TestServer) handleCacheable(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<html><body>Cacheable HTML content</body></html>"))
}

// Cacheable JSON content
func (ts *TestServer) handleCacheableJSON(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "cacheable json", "timestamp": "static"}`))
}

// Cacheable CSS content
func (ts *TestServer) handleCacheableCSS(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/css")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("body { color: blue; }"))
}

// Cacheable JavaScript content
func (ts *TestServer) handleCacheableJS(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "application/javascript")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("console.log('cacheable js');"))
}

// Non-cacheable binary content
func (ts *TestServer) handleNonCacheable(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("binary data that should not be cached"))
}

// Binary data
func (ts *TestServer) handleBinary(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	// Generate some binary data
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	w.Write(data)
}

// Image content (PNG)
func (ts *TestServer) handleImage(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	// Minimal PNG header for testing
	w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
}

// 400 Bad Request
func (ts *TestServer) handleError400(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	http.Error(w, "Bad Request", http.StatusBadRequest)
}

// 404 Not Found
func (ts *TestServer) handleError404(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	http.Error(w, "Not Found", http.StatusNotFound)
}

// 500 Internal Server Error
func (ts *TestServer) handleError500(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

// 503 Service Unavailable
func (ts *TestServer) handleError503(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
}

// 301 Permanent Redirect
func (ts *TestServer) handleRedirect301(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	http.Redirect(w, r, "/redirect/target", http.StatusMovedPermanently)
}

// 302 Temporary Redirect
func (ts *TestServer) handleRedirect302(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	http.Redirect(w, r, "/redirect/target", http.StatusFound)
}

// Redirect target
func (ts *TestServer) handleRedirectTarget(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<html><body>Redirect target reached</body></html>"))
}

// No-cache headers
func (ts *TestServer) handleNoCache(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<html><body>This should not be cached</body></html>"))
}

// Max-age cache control
func (ts *TestServer) handleMaxAge(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "max-age=300")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<html><body>Cacheable for 5 minutes</body></html>"))
}

// Expires header
func (ts *TestServer) handleExpires(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "text/html")
	expires := time.Now().Add(5 * time.Minute).Format(http.TimeFormat)
	w.Header().Set("Expires", expires)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<html><body>Expires in 5 minutes</body></html>"))
}

// Dynamic content that changes each request
func (ts *TestServer) handleDynamic(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	count := atomic.AddInt64(&ts.requestCount, 1)
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("<html><body>Request #%d at %d</body></html>",
		count, time.Now().Unix())))
}

// Timestamp endpoint
func (ts *TestServer) handleTimestamp(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"timestamp": %d}`, time.Now().Unix())))
}

// Slow response for timeout testing
func (ts *TestServer) handleSlow(w http.ResponseWriter, r *http.Request) {
	ts.incrementCounter()
	// Extract delay from query parameter or use default
	delayStr := r.URL.Query().Get("delay")
	delay := 1000 // default 1 second
	if delayStr != "" {
		if d, err := strconv.Atoi(delayStr); err == nil {
			delay = d
		}
	}
	time.Sleep(time.Duration(delay) * time.Millisecond)

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("<html><body>Slow response after %dms</body></html>", delay)))
}

// Large response
func (ts *TestServer) handleLarge(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()

	// Extract size from query parameter or use default
	sizeStr := r.URL.Query().Get("size")
	size := 1024 * 1024 // default 1MB
	if sizeStr != "" {
		if s, err := strconv.Atoi(sizeStr); err == nil {
			size = s
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	// Generate large content
	chunk := strings.Repeat("A", 1024) // 1KB chunks
	remaining := size
	for remaining > 0 {
		if remaining < len(chunk) {
			w.Write([]byte(chunk[:remaining]))
			break
		}
		w.Write([]byte(chunk))
		remaining -= len(chunk)
	}
}

// Header echo/testing
func (ts *TestServer) handleHeaders(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	ts.incrementCounter()

	w.Header().Set("Content-Type", "application/json")

	// Echo back request headers
	for key, values := range r.Header {
		for _, value := range values {
			w.Header().Add("X-Echo-"+key, value)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"method": "%s", "path": "%s", "host": "%s"}`,
		r.Method, r.URL.Path, r.Host)))
}

// Request counter endpoint
func (ts *TestServer) handleCounter(w http.ResponseWriter, r *http.Request) {
	ts.applyDelay()
	count := atomic.AddInt64(&ts.requestCount, 1)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"request_count": %d}`, count)))
}
