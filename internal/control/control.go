package control

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/gbmerrall/gocache/internal/cache"
	"github.com/gbmerrall/gocache/internal/config"
	"github.com/gbmerrall/gocache/internal/proxy"
)

// ControlAPI provides an HTTP interface for managing the cache and proxy.
type ControlAPI struct {
	logger    *slog.Logger
	config    *config.Config
	cache     *cache.MemoryCache
	proxy     *proxy.Proxy
	startTime time.Time
	server    *http.Server
	shutdown  func() // Function to trigger graceful shutdown
}

// NewControlAPI creates a new ControlAPI instance.
func NewControlAPI(logger *slog.Logger, cfg *config.Config, c *cache.MemoryCache, p *proxy.Proxy, shutdown func()) *ControlAPI {
	api := &ControlAPI{
		logger:    logger,
		config:    cfg,
		cache:     c,
		proxy:     p,
		startTime: time.Now(),
		shutdown:  shutdown,
	}
	return api
}

// Start runs the Control API server.
func (a *ControlAPI) Start() error {
	addr := fmt.Sprintf("%s:%d", a.config.Server.BindAddress, a.config.Server.ControlPort)
	a.logger.Info("starting control API", "address", addr)

	if a.config.Server.BindAddress != "127.0.0.1" && a.config.Server.BindAddress != "localhost" {
		return fmt.Errorf("security risk: control API cannot bind to non-localhost address: %s", a.config.Server.BindAddress)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/stats", a.handleStats)
	mux.HandleFunc("/purge/all", a.handlePurgeAll)
	mux.HandleFunc("/purge/url", a.handlePurgeURL)
	mux.HandleFunc("/purge/domain/", a.handlePurgeDomain)
	mux.HandleFunc("/ca", a.handleCA)
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/shutdown", a.handleShutdown)
	mux.HandleFunc("/reload", a.handleReload)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "GoCache Control API")
	})

	a.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return a.server.ListenAndServe()
}

// Shutdown gracefully shuts down the control API server.
func (a *ControlAPI) Shutdown(ctx context.Context) error {
	a.logger.Info("shutting down control API")
	return a.server.Shutdown(ctx)
}

// ReloadConfig reloads the configuration from disk and applies the changes.
func (a *ControlAPI) ReloadConfig() error {
	newCfg, err := config.LoadConfig(a.config.LoadedPath)
	if err != nil {
		return fmt.Errorf("failed to reload config file: %w", err)
	}

	a.config = newCfg
	a.cache.UpdateTTL(newCfg.Cache.GetDefaultTTL())
	a.proxy.SetConfig(newCfg)

	a.logger.Info("configuration reloaded successfully")
	return nil
}

func (a *ControlAPI) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := a.ReloadConfig(); err != nil {
		a.logger.Error("failed to reload config via API", "error", err)
		http.Error(w, "Failed to reload config", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Configuration reloaded")
}

func (a *ControlAPI) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.logger.Info("shutdown request received via API")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Shutdown initiated...")

	go a.shutdown()
}
// ... (rest of the handlers remain the same)
func (a *ControlAPI) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.logger.Debug("stats endpoint accessed", "remoteAddr", r.RemoteAddr)
	stats := a.cache.GetStats()
	totalRequests := stats.Hits + stats.Misses
	var hitRate float64
	if totalRequests > 0 {
		hitRate = (float64(stats.Hits) / float64(totalRequests)) * 100
	}
	response := map[string]interface{}{
		"hit_count":           stats.Hits,
		"miss_count":          stats.Misses,
		"hit_rate_percent":    fmt.Sprintf("%.2f", hitRate),
		"entry_count":         stats.EntryCount,
		"uptime_seconds":      fmt.Sprintf("%.2f", stats.UptimeSeconds),
		"cache_size_bytes":    stats.TotalSize,
		"cert_cache_count": a.proxy.GetCertCacheStats(),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error("failed to encode stats response", "error", err)
	}
}

func (a *ControlAPI) handlePurgeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.logger.Debug("purge all endpoint accessed", "remoteAddr", r.RemoteAddr)
	count := a.cache.PurgeAll()
	a.logger.Info("purged all cache entries", "count", count)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"purged_count": count,
	}); err != nil {
		a.logger.Error("failed to encode purge all response", "error", err)
	}
}

type purgeURLRequest struct {
	URL string `json:"url"`
}

func (a *ControlAPI) handlePurgeURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req purgeURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}
	found := a.cache.PurgeByURL(req.URL)
	a.logger.Info("purge request by URL", "url", req.URL, "found", found)
	a.logger.Debug("purge by URL details", "url", req.URL, "found", found, "remoteAddr", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"url":    req.URL,
		"purged": found,
	}); err != nil {
		a.logger.Error("failed to encode purge url response", "error", err)
	}
}

func (a *ControlAPI) handlePurgeDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	domain := strings.TrimPrefix(r.URL.Path, "/purge/domain/")
	if domain == "" {
		http.Error(w, "Domain is required", http.StatusBadRequest)
		return
	}
	count := a.cache.PurgeByDomain(domain)
	a.logger.Info("purged cache entries by domain", "domain", domain, "count", count)
	a.logger.Debug("purge by domain details", "domain", domain, "count", count, "remoteAddr", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"domain":       domain,
		"purged_count": count,
	}); err != nil {
		a.logger.Error("failed to encode purge domain response", "error", err)
	}
}

func (a *ControlAPI) handleCA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	caCert := a.proxy.GetCA()
	if caCert == nil {
		http.Error(w, "CA Certificate not generated", http.StatusInternalServerError)
		return
	}
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCert.Raw,
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=\"gocache-ca.crt\"")
	if err := pem.Encode(w, pemBlock); err != nil {
		a.logger.Error("failed to encode CA certificate", "error", err)
	}
}

func (a *ControlAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response := map[string]interface{}{
		"status":      "ok",
		"go_version":  runtime.Version(),
		"uptime":      time.Since(a.startTime).String(),
		"config_file": a.config.LoadedPath,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error("failed to encode health response", "error", err)
	}
}
