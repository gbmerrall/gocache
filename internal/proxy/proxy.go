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
	cacheKey := getCacheKey(r)

	if entry, ok := p.cache.Get(cacheKey); ok {
		p.logger.Info("cache hit", "key", cacheKey)
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

	p.logger.Info("cache miss", "key", cacheKey)

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read http response body", "error", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	if p.shouldCacheResponse(resp) {
		entry := cache.CacheEntry{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
		p.cache.Set(cacheKey, entry)
		p.logger.Info("response cached", "key", cacheKey)
	}

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (p *Proxy) handleHTTPS(w http.ResponseWriter, r *http.Request) {
	p.logger.Info("https request", "host", r.Host)

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
	cacheKey := getCacheKey(req)

	if entry, ok := p.cache.Get(cacheKey); ok {
		p.logger.Info("cache hit (https)", "key", cacheKey)
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

	p.logger.Info("cache miss (https)", "key", cacheKey)

	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		p.logger.Error("failed to forward https request", "error", err)
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

	if p.shouldCacheResponse(resp) {
		entry := cache.CacheEntry{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       body,
		}
		p.cache.Set(cacheKey, entry)
		p.logger.Info("response cached (https)", "key", cacheKey)
	}

	resp.Header.Set("X-Cache", "MISS")
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
		return cert, nil
	}
	p.certCacheMu.RUnlock()

	p.certCacheMu.Lock()
	defer p.certCacheMu.Unlock()
	if cert, ok := p.certCache[host]; ok {
		return cert, nil
	}

	hostCert, hostKey, err := cert.GenerateHostCert(p.ca, p.caPrivKey, host)
	if err != nil {
		return nil, err
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{hostCert.Raw},
		PrivateKey:  hostKey,
	}
	p.certCache[host] = tlsCert
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
