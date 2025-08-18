package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/gbmerrall/gocache/internal/cache"
	"github.com/gbmerrall/gocache/internal/cert"
	"github.com/gbmerrall/gocache/internal/config"
)

// Proxy is the main proxy server struct.
type Proxy struct {
	logger    *slog.Logger
	config    *config.Config
	cache     *cache.MemoryCache
	ca        *x509.Certificate
	caPrivKey *rsa.PrivateKey
	server    *http.Server
	transport http.RoundTripper

	certCache   map[string]*tls.Certificate
	certCacheMu sync.RWMutex
}

// NewProxy creates a new Proxy server.
func NewProxy(logger *slog.Logger, c *cache.MemoryCache, cfg *config.Config) (*Proxy, error) {
	ca, caPrivKey, err := cert.LoadCA()
	if err != nil {
		return nil, err
	}

	p := &Proxy{
		logger:    logger,
		config:    cfg,
		cache:     c,
		ca:        ca,
		caPrivKey: caPrivKey,
		certCache: make(map[string]*tls.Certificate),
		transport: http.DefaultTransport,
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

// SetConfig updates the proxy's configuration.
func (p *Proxy) SetConfig(cfg *config.Config) {
	p.config = cfg
}

// SetTransport sets the transport for the proxy.
func (p *Proxy) SetTransport(transport http.RoundTripper) {
	p.transport = transport
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
	p.logger.Info("http request", "method", r.Method, "url", r.URL)
	p.logger.Debug("http request details", "headers", r.Header, "contentLength", r.ContentLength)
	
	// Only check cache for cacheable request methods
	var cacheKey string
	var fromCache bool
	if p.shouldCacheRequest(r) {
		cacheKey = getCacheKey(r)
		if entry, ok := p.cache.Get(cacheKey); ok {
			p.logger.Info("cache hit", "key", cacheKey)
			p.logger.Debug("serving cached response", "statusCode", entry.StatusCode, "bodySize", len(entry.Body))
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
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read http response body", "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
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
			w.Header().Add(key, value)
		}
	}
	
	// Set cache header based on whether request method is cacheable
	if p.shouldCacheRequest(r) {
		w.Header().Set("X-Cache", "MISS")
	}
	
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (p *Proxy) handleHTTPS(w http.ResponseWriter, r *http.Request) {
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
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read https response body", "error", err)
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
	if p.shouldCacheRequest(req) {
		resp.Header.Set("X-Cache", "MISS")
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
	}
}

func (p *Proxy) getCert(host string) (*tls.Certificate, error) {
	p.certCacheMu.RLock()
	if cert, ok := p.certCache[host]; ok {
		p.certCacheMu.RUnlock()
		p.logger.Debug("certificate cache hit", "host", host)
		return cert, nil
	}
	p.certCacheMu.RUnlock()
	p.logger.Debug("certificate cache miss, generating new cert", "host", host)

	p.certCacheMu.Lock()
	defer p.certCacheMu.Unlock()
	if cert, ok := p.certCache[host]; ok {
		return cert, nil
	}

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
	p.certCache[host] = tlsCert
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
