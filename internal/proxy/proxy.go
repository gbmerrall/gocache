package proxy

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gbmerrall/gocache/internal/cache"
	"github.com/gbmerrall/gocache/internal/cert"
	"github.com/gbmerrall/gocache/internal/config"
	"github.com/gbmerrall/gocache/internal/logging"
)

// certNode wraps a certificate with metadata for LRU tracking.
type certNode struct {
	host string
	cert *tls.Certificate
}

// Proxy is the main proxy server struct.
type Proxy struct {
	logger    *slog.Logger
	config    *config.Config
	cache     *cache.MemoryCache
	accessLog *logging.AccessLogger
	ca        *x509.Certificate
	caPrivKey *rsa.PrivateKey
	server    *http.Server
	transport http.RoundTripper

	certCache     map[string]*list.Element // Maps hostname -> list element
	certLRUList   *list.List               // Doubly-linked list (head=recent, tail=old)
	certCacheMu   sync.RWMutex
	certEvictions atomic.Uint64 // Eviction counter for metrics
}

// NewProxy creates a new Proxy server.
func NewProxy(logger *slog.Logger, c *cache.MemoryCache, cfg *config.Config) (*Proxy, error) {
	ca, caPrivKey, err := cert.LoadCA()
	if err != nil {
		return nil, err
	}

	// Apply process detection for access log configuration
	cfg.Logging.ApplyProcessDetection(logging.IsForegroundMode())

	// Initialize access logger if needed
	var accessLog *logging.AccessLogger
	if cfg.Logging.AccessToStdout || cfg.Logging.AccessLogfile != "" {
		format := logging.FormatHuman
		if cfg.Logging.ValidateAccessFormat() == "json" {
			format = logging.FormatJSON
		}

		accessLogConfig := logging.AccessLoggerConfig{
			Format:        format,
			StdoutEnabled: cfg.Logging.AccessToStdout,
			LogFile:       cfg.Logging.AccessLogfile,
			BufferSize:    1000,
			ErrorHandler:  logging.DefaultErrorHandler,
		}

		accessLog, err = logging.NewAccessLogger(accessLogConfig)
		if err != nil {
			logger.Warn("failed to initialize access logger", "error", err)
			// Continue without access logging
		}
	}

	p := &Proxy{
		logger:      logger,
		config:      cfg,
		cache:       c,
		accessLog:   accessLog,
		ca:          ca,
		caPrivKey:   caPrivKey,
		certCache:   make(map[string]*list.Element),
		certLRUList: list.New(),
		transport:   http.DefaultTransport,
	}
	p.server = &http.Server{Handler: p}
	return p, nil
}

// GetCA returns the root CA certificate.
func (p *Proxy) GetCA() *x509.Certificate {
	return p.ca
}

// GetCertCacheStats returns the number of cached certificates.
func (p *Proxy) GetCertCacheStats() int {
	p.certCacheMu.RLock()
	defer p.certCacheMu.RUnlock()
	return len(p.certCache)
}

// GetCertCacheMetrics returns the cert cache size and eviction count.
func (p *Proxy) GetCertCacheMetrics() (int, uint64) {
	p.certCacheMu.RLock()
	size := len(p.certCache)
	p.certCacheMu.RUnlock()
	evictions := p.certEvictions.Load()
	return size, evictions
}

// SetConfig updates the proxy's configuration.
func (p *Proxy) SetConfig(cfg *config.Config) {
	p.config = cfg
}

// SetTransport sets the transport for the proxy.
func (p *Proxy) SetTransport(transport http.RoundTripper) {
	p.transport = transport
}

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

// logAccess logs an HTTP request/response to the access log if enabled
func (p *Proxy) logAccess(startTime time.Time, r *http.Request, statusCode int, responseSize int64, cacheStatus string, contentType string) {
	if p.accessLog != nil {
		duration := time.Since(startTime)
		fullURL := r.URL.String()
		if r.Host != "" && !strings.HasPrefix(fullURL, "http") {
			// For CONNECT requests or relative URLs, construct full URL
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			fullURL = scheme + "://" + r.Host + r.URL.String()
		}

		p.accessLog.LogRequest(r.Method, fullURL, cacheStatus, statusCode, responseSize, duration, contentType)
	}
}

// getCacheKey creates a normalized cache key from a request's URL.
func getCacheKey(r *http.Request) string {
	u := *r.URL
	u.Fragment = ""

	q := u.Query()
	if len(q) > 0 {
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		newValues := make(url.Values)
		for _, k := range keys {
			newValues[k] = q[k]
		}
		u.RawQuery = newValues.Encode()
	}

	return u.String()
}

// isErrorStatusCode returns true if the status code is 4xx or 5xx
func isErrorStatusCode(statusCode int) bool {
	return statusCode >= 400 && statusCode <= 599
}

// getPostCacheKey creates a cache key for a POST request.
// The key is a combination of the URL (with or without query string) and a hash of the request body.
func (p *Proxy) getPostCacheKey(r *http.Request, body []byte) string {
	// Start with the base URL, path only
	keyURL := r.URL.Scheme + "://" + r.URL.Host + r.URL.Path

	// Include query string if configured
	if p.config.Cache.PostCache.IncludeQueryString && r.URL.RawQuery != "" {
		keyURL += "?" + r.URL.RawQuery
	}

	// Add the hash of the body
	hasher := sha256.New()
	hasher.Write(body)
	bodyHash := hex.EncodeToString(hasher.Sum(nil))

	return keyURL + ":" + bodyHash
}

// shouldCacheRequest determines if a request should be cached based on HTTP method.
func (p *Proxy) shouldCacheRequest(r *http.Request) bool {
	// Only cache GET requests by default
	// Other methods can be enabled through specific configuration
	return r.Method == http.MethodGet
}

// shouldCacheResponse determines if a response should be cached.
func (p *Proxy) shouldCacheResponse(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	isCacheableType := false
	for _, t := range p.config.Cache.CacheableTypes {
		if strings.HasPrefix(contentType, t) {
			isCacheableType = true
			break
		}
	}
	if !isCacheableType {
		p.logger.Debug("skipping cache: non-cacheable content type", "contentType", contentType)
		return false
	}

	if !p.config.Cache.IgnoreNoCache {
		cacheControl := resp.Header.Get("Cache-Control")
		if strings.Contains(cacheControl, "no-cache") || strings.Contains(cacheControl, "no-store") {
			p.logger.Debug("skipping cache: cache-control header", "value", cacheControl)
			return false
		}
		pragma := resp.Header.Get("Pragma")
		if pragma == "no-cache" {
			p.logger.Debug("skipping cache: pragma header", "value", pragma)
			return false
		}
	}

	return true
}

// ServeHTTP is the main handler for all incoming proxy requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleHTTPS(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}

func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Wrap response writer for access logging
	crw := logging.NewCountingResponseWriter(w)

	p.logger.Info("http request", "method", r.Method, "url", r.URL)
	p.logger.Debug("http request details", "headers", r.Header, "contentLength", r.ContentLength)

	if r.Method == http.MethodPost && p.config.Cache.PostCache.Enable {
		p.handlePostRequest(crw, r)
		// Log access for POST requests handled separately
		contentType := crw.Header().Get("Content-Type")
		cacheStatus := crw.Header().Get("X-Cache")
		if cacheStatus == "" {
			cacheStatus = "" // POST requests may or may not be cached
		}
		p.logAccess(startTime, r, crw.StatusCode(), crw.Size(), cacheStatus, contentType)
		return
	}

	// Only check cache for cacheable request methods
	var cacheKey string
	var fromCache bool
	if p.shouldCacheRequest(r) {
		cacheKey = getCacheKey(r)
		if entry, ok := p.cache.Get(cacheKey); ok {
			p.logger.Info("cache hit", "key", cacheKey)
			p.logger.Debug("serving cached response", "statusCode", entry.StatusCode, "bodySize", len(entry.Body))
			crw.Header().Set("X-Cache", "HIT")
			for key, values := range entry.Headers {
				for _, value := range values {
					crw.Header().Add(key, value)
				}
			}
			crw.WriteHeader(entry.StatusCode)
			crw.Write(entry.Body)

			// Log access for cached response
			contentType := crw.Header().Get("Content-Type")
			p.logAccess(startTime, r, crw.StatusCode(), crw.Size(), "HIT", contentType)
			return
		}
		fromCache = false
	} else {
		p.logger.Debug("request method not cacheable", "method", r.Method)
	}

	if fromCache == false && p.shouldCacheRequest(r) {
		p.logger.Info("cache miss", "key", cacheKey)
	} else if !p.shouldCacheRequest(r) {
		p.logger.Debug("forwarding non-cacheable request to upstream", "method", r.Method, "url", r.URL.String())
	}

	r.RequestURI = ""
	r.Header.Del("Proxy-Connection")
	r.Header.Del("Proxy-Authorization")

	resp, err := p.transport.RoundTrip(r)
	if err != nil {
		p.logger.Error("failed to forward http request", "error", err)
		p.logger.Debug("upstream request failed", "url", r.URL.String(), "error", err)
		http.Error(crw, err.Error(), http.StatusServiceUnavailable)
		// Log access for error response
		p.logAccess(startTime, r, http.StatusServiceUnavailable, crw.Size(), "", "text/plain")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read http response body", "error", err)
		http.Error(crw, err.Error(), http.StatusServiceUnavailable)
		// Log access for error response
		p.logAccess(startTime, r, http.StatusServiceUnavailable, crw.Size(), "", "text/plain")
		return
	}

	// Only cache responses for cacheable request methods
	if p.shouldCacheRequest(r) && p.shouldCacheResponse(resp) {
		entry := cache.CacheEntry{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}

		// Use negative TTL for error status codes (4xx, 5xx)
		if isErrorStatusCode(resp.StatusCode) {
			negativeTTL := p.config.Cache.GetNegativeTTL()
			p.cache.SetWithTTL(cacheKey, entry, negativeTTL)
			p.logger.Info("response cached with negative TTL", "key", cacheKey, "ttl", negativeTTL)
			p.logger.Debug("cached error response details", "statusCode", resp.StatusCode, "contentType", resp.Header.Get("Content-Type"), "bodySize", len(body), "negativeTTL", negativeTTL)
		} else {
			p.cache.Set(cacheKey, entry)
			p.logger.Info("response cached", "key", cacheKey)
			p.logger.Debug("cached response details", "statusCode", resp.StatusCode, "contentType", resp.Header.Get("Content-Type"), "bodySize", len(body))
		}
	} else if p.shouldCacheRequest(r) {
		p.logger.Debug("response not cached", "key", cacheKey, "statusCode", resp.StatusCode)
	} else {
		p.logger.Debug("response not cached - method not cacheable", "method", r.Method, "statusCode", resp.StatusCode)
	}

	for key, values := range resp.Header {
		for _, value := range values {
			crw.Header().Add(key, value)
		}
	}

	// Set cache header based on whether request method is cacheable
	var cacheStatus string
	if p.shouldCacheRequest(r) {
		crw.Header().Set("X-Cache", "MISS")
		cacheStatus = "MISS"
	} else {
		cacheStatus = "" // Non-cacheable requests don't have cache status
	}

	crw.WriteHeader(resp.StatusCode)
	crw.Write(body)

	// Log access for successful response
	contentType := crw.Header().Get("Content-Type")
	p.logAccess(startTime, r, crw.StatusCode(), crw.Size(), cacheStatus, contentType)
}

func (p *Proxy) handlePostRequest(w http.ResponseWriter, r *http.Request) {
	// Enforce request body size limit
	maxSize := int64(p.config.Cache.PostCache.MaxRequestBodySizeMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		if err.Error() == "http: request body too large" {
			p.logger.Warn("POST request body too large", "limit_bytes", maxSize, "url", r.URL.String())
			http.Error(w, "Request Body Too Large", http.StatusRequestEntityTooLarge)
		} else {
			p.logger.Error("failed to read POST request body", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}
	// Restore the body so it can be sent to the upstream server
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check cache
	cacheKey := p.getPostCacheKey(r, bodyBytes)
	if entry, ok := p.cache.Get(cacheKey); ok {
		p.logger.Info("cache hit (POST)", "key", cacheKey)
		p.logger.Debug("serving cached POST response", "statusCode", entry.StatusCode, "bodySize", len(entry.Body))
		w.Header().Set("X-Cache", "HIT")
		for key, values := range entry.Headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(entry.StatusCode)
		w.Write(entry.Body)
		return
	}

	p.logger.Info("cache miss (POST)", "key", cacheKey)

	r.RequestURI = ""
	r.Header.Del("Proxy-Connection")
	r.Header.Del("Proxy-Authorization")

	resp, err := p.transport.RoundTrip(r)
	if err != nil {
		p.logger.Error("failed to forward http request", "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read http response body", "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Check response body size before caching
	maxRespSize := int64(p.config.Cache.PostCache.MaxResponseBodySizeMB) * 1024 * 1024
	if int64(len(respBody)) > maxRespSize {
		p.logger.Warn("POST response body too large to cache", "limit_bytes", maxRespSize, "actual_bytes", len(respBody), "url", r.URL.String())
	} else if p.shouldCacheResponse(resp) {
		entry := cache.CacheEntry{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       respBody,
		}

		if isErrorStatusCode(resp.StatusCode) {
			negativeTTL := p.config.Cache.GetNegativeTTL()
			p.cache.SetWithTTL(cacheKey, entry, negativeTTL)
			p.logger.Info("POST response cached with negative TTL", "key", cacheKey, "ttl", negativeTTL)
		} else {
			p.cache.Set(cacheKey, entry)
			p.logger.Info("POST response cached", "key", cacheKey)
		}
	}

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func (p *Proxy) handleHTTPS(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	p.logger.Info("https request", "host", r.Host)
	p.logger.Debug("https connect request details", "method", r.Method, "host", r.Host, "userAgent", r.Header.Get("User-Agent"))

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		p.logger.Error("hijacking not supported")
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		p.logger.Error("failed to hijack connection", "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	_, err = clientConn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	if err != nil {
		p.logger.Error("failed to write 200 OK to client", "error", err)
		return
	}

	tlsCert, err := p.getCert(r.Host)
	if err != nil {
		p.logger.Error("failed to get certificate", "host", r.Host, "error", err)
		p.logger.Debug("certificate generation failed", "host", r.Host, "error", err)
		return
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{*tlsCert}}
	tlsConn := tls.Server(clientConn, tlsConfig)
	defer tlsConn.Close()

	reader := bufio.NewReader(tlsConn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		if err != io.EOF {
			p.logger.Error("failed to read https request", "error", err)
		}
		return
	}

	req.URL.Scheme = "https"
	req.URL.Host = r.Host

	// Only check cache for cacheable request methods
	var cacheKey string
	var fromCache bool
	if p.shouldCacheRequest(req) {
		cacheKey = getCacheKey(req)
		if entry, ok := p.cache.Get(cacheKey); ok {
			p.logger.Info("cache hit (https)", "key", cacheKey)
			p.logger.Debug("serving cached https response", "statusCode", entry.StatusCode, "bodySize", len(entry.Body))
			entry.Headers.Set("X-Cache", "HIT")
			cachedResp := http.Response{
				StatusCode: entry.StatusCode,
				Header:     entry.Headers,
				Body:       io.NopCloser(bytes.NewReader(entry.Body)),
			}
			if err := cachedResp.Write(tlsConn); err != nil {
				p.logger.Error("failed to write cached https response", "error", err)
				// Log access for error
				p.logAccess(startTime, req, http.StatusInternalServerError, 0, "HIT", "")
			} else {
				// Log access for successful cached response
				contentType := entry.Headers.Get("Content-Type")
				p.logAccess(startTime, req, entry.StatusCode, int64(len(entry.Body)), "HIT", contentType)
			}
			return
		}
		fromCache = false
	} else {
		p.logger.Debug("https request method not cacheable", "method", req.Method)
	}

	if fromCache == false && p.shouldCacheRequest(req) {
		p.logger.Info("cache miss (https)", "key", cacheKey)
	} else if !p.shouldCacheRequest(req) {
		p.logger.Debug("forwarding non-cacheable https request to upstream", "method", req.Method, "url", req.URL.String())
	}

	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		p.logger.Error("failed to forward https request", "error", err)
		p.logger.Debug("upstream https request failed", "url", req.URL.String(), "error", err)
		errorResponse := &http.Response{
			StatusCode: http.StatusBadGateway,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(bytes.NewBufferString("Bad Gateway\n")),
		}
		errorResponse.Write(tlsConn)
		// Log access for error response
		p.logAccess(startTime, req, http.StatusBadGateway, 12, "", "text/plain") // "Bad Gateway\n" is 12 bytes
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read https response body", "error", err)
		// Log access for error
		p.logAccess(startTime, req, http.StatusInternalServerError, 0, "", "")
		return
	}

	// Only cache responses for cacheable request methods
	if p.shouldCacheRequest(req) && p.shouldCacheResponse(resp) {
		entry := cache.CacheEntry{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}

		// Use negative TTL for error status codes (4xx, 5xx)
		if isErrorStatusCode(resp.StatusCode) {
			negativeTTL := p.config.Cache.GetNegativeTTL()
			p.cache.SetWithTTL(cacheKey, entry, negativeTTL)
			p.logger.Info("response cached with negative TTL (https)", "key", cacheKey, "ttl", negativeTTL)
			p.logger.Debug("cached error response details (https)", "statusCode", resp.StatusCode, "contentType", resp.Header.Get("Content-Type"), "bodySize", len(body), "negativeTTL", negativeTTL)
		} else {
			p.cache.Set(cacheKey, entry)
			p.logger.Info("response cached (https)", "key", cacheKey)
			p.logger.Debug("cached response details (https)", "statusCode", resp.StatusCode, "contentType", resp.Header.Get("Content-Type"), "bodySize", len(body))
		}
	} else if p.shouldCacheRequest(req) {
		p.logger.Debug("https response not cached", "key", cacheKey, "statusCode", resp.StatusCode)
	} else {
		p.logger.Debug("https response not cached - method not cacheable", "method", req.Method, "statusCode", resp.StatusCode)
	}

	// Set cache header based on whether request method is cacheable
	var cacheStatus string
	if p.shouldCacheRequest(req) {
		resp.Header.Set("X-Cache", "MISS")
		cacheStatus = "MISS"
	} else {
		cacheStatus = "" // Non-cacheable requests don't have cache status
	}

	newResp := http.Response{
		StatusCode:    resp.StatusCode,
		Proto:         resp.Proto,
		ProtoMajor:    resp.ProtoMajor,
		ProtoMinor:    resp.ProtoMinor,
		Header:        resp.Header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}

	if err := newResp.Write(tlsConn); err != nil {
		p.logger.Error("failed to write https response", "error", err)
		// Log access for write error
		p.logAccess(startTime, req, http.StatusInternalServerError, 0, cacheStatus, "")
	} else {
		// Log access for successful response
		contentType := resp.Header.Get("Content-Type")
		p.logAccess(startTime, req, resp.StatusCode, int64(len(body)), cacheStatus, contentType)
	}
}

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

func (p *Proxy) getCert(host string) (*tls.Certificate, error) {
	// Check cache with read lock
	p.certCacheMu.RLock()
	elem, found := p.certCache[host]
	if found {
		node := elem.Value.(*certNode)
		p.certCacheMu.RUnlock()

		// Upgrade to write lock to move to front
		p.certCacheMu.Lock()
		// Re-verify element still in cache after lock upgrade
		if currentElem, stillExists := p.certCache[host]; stillExists && currentElem == elem {
			p.certLRUList.MoveToFront(elem)
		}
		p.certCacheMu.Unlock()

		p.logger.Debug("certificate cache hit", "host", host)
		return node.cert, nil
	}
	p.certCacheMu.RUnlock()
	p.logger.Debug("certificate cache miss, generating new cert", "host", host)

	p.certCacheMu.Lock()
	defer p.certCacheMu.Unlock()

	// Double-check after acquiring write lock
	if elem, ok := p.certCache[host]; ok {
		node := elem.Value.(*certNode)
		p.certLRUList.MoveToFront(elem)
		return node.cert, nil
	}

	// Generate new certificate
	hostCert, hostKey, err := cert.GenerateHostCert(p.ca, p.caPrivKey, host)
	if err != nil {
		p.logger.Debug("certificate generation failed", "host", host, "error", err)
		return nil, err
	}
	p.logger.Debug("certificate generated successfully", "host", host, "serialNumber", hostCert.SerialNumber)

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{hostCert.Raw},
		PrivateKey:  hostKey,
	}

	// Evict if at capacity
	maxEntries := p.config.Server.MaxCertCacheEntries
	if maxEntries > 0 && len(p.certCache) >= maxEntries {
		p.evictOldestCert()
	}

	// Add new cert to front (most recent)
	node := &certNode{
		host: host,
		cert: tlsCert,
	}
	elem = p.certLRUList.PushFront(node)
	p.certCache[host] = elem
	p.logger.Debug("certificate cached", "host", host, "cacheSize", len(p.certCache))
	return tlsCert, nil
}

// Start begins listening for proxy connections.
func (p *Proxy) Start(addr string) error {
	p.logger.Info("proxy server starting", "address", addr)
	p.server = &http.Server{
		Addr:    addr,
		Handler: p,
	}
	return p.server.ListenAndServe()
}

// Shutdown gracefully shuts down the proxy server.
func (p *Proxy) Shutdown(ctx context.Context) error {
	p.logger.Info("shutting down proxy server")
	return p.server.Shutdown(ctx)
}
