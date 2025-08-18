package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	t.Run("Load with no path", func(t *testing.T) {
		// Should return defaults without error if no path is given
		// and no standard locations exist.
		cfg, err := LoadConfig("")
		if err != nil {
			t.Fatalf("expected no error when no path is provided, got %v", err)
		}
		if cfg.Server.ProxyPort != 8080 {
			t.Errorf("got port %d, want default 8080", cfg.Server.ProxyPort)
		}
	})

	t.Run("Load non-existent explicit path", func(t *testing.T) {
		_, err := LoadConfig("/non/existent/path")
		if err == nil {
			t.Fatal("expected an error for non-existent explicit file, got nil")
		}
	})

	t.Run("Load from file", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-config")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		configFile := filepath.Join(tmpDir, "gocache.toml")
		content := `
[server]
proxy_port = 9999

[cache]
default_ttl = "10m"

[persistence]
auto_save_interval = "1m"
`
		if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := LoadConfig(configFile)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		if cfg.Server.ProxyPort != 9999 {
			t.Errorf("got port %d, want 9999", cfg.Server.ProxyPort)
		}
		if cfg.Cache.GetDefaultTTL() != 10*time.Minute {
			t.Errorf("got ttl %v, want 10m", cfg.Cache.GetDefaultTTL())
		}
		// Test default negative TTL since not specified in config
		if cfg.Cache.GetNegativeTTL() != 10*time.Second {
			t.Errorf("got negative ttl %v, want default 10s", cfg.Cache.GetNegativeTTL())
		}
		if cfg.Persistence.GetAutoSaveInterval() != 1*time.Minute {
			t.Errorf("got auto_save_interval %v, want 1m", cfg.Persistence.GetAutoSaveInterval())
		}
	})

	t.Run("Invalid durations", func(t *testing.T) {
		cfg := NewDefaultConfig()
		cfg.Cache.DefaultTTL = "invalid"
		cfg.Persistence.AutoSaveInterval = "invalid"

		if cfg.Cache.GetDefaultTTL() != 1*time.Hour {
			t.Errorf("got ttl %v, want default 1h on parse error", cfg.Cache.GetDefaultTTL())
		}
		if cfg.Persistence.GetAutoSaveInterval() != 5*time.Minute {
			t.Errorf("got auto_save_interval %v, want default 5m on parse error", cfg.Persistence.GetAutoSaveInterval())
		}
		if cfg.Cache.GetNegativeTTL() != 10*time.Second {
			t.Errorf("got negative ttl %v, want default 10s on parse error", cfg.Cache.GetNegativeTTL())
		}
	})

	t.Run("Negative TTL configuration", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-negative-ttl")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		configFile := filepath.Join(tmpDir, "gocache.toml")
		content := `
[cache]
default_ttl = "2h"
negative_ttl = "30s"
`
		if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := LoadConfig(configFile)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		if cfg.Cache.GetDefaultTTL() != 2*time.Hour {
			t.Errorf("got default ttl %v, want 2h", cfg.Cache.GetDefaultTTL())
		}
		if cfg.Cache.GetNegativeTTL() != 30*time.Second {
			t.Errorf("got negative ttl %v, want 30s", cfg.Cache.GetNegativeTTL())
		}
	})

	t.Run("Invalid negative TTL", func(t *testing.T) {
		cfg := NewDefaultConfig()
		cfg.Cache.NegativeTTL = "invalid"

		if cfg.Cache.GetNegativeTTL() != 10*time.Second {
			t.Errorf("got negative ttl %v, want default 10s on parse error", cfg.Cache.GetNegativeTTL())
		}
	})
}

func TestPostCacheConfig(t *testing.T) {
	t.Run("Defaults", func(t *testing.T) {
		cfg := NewDefaultConfig()
		if cfg.Cache.PostCache.Enable != false {
			t.Error("expected PostCache.Enable to be false by default")
		}
		if cfg.Cache.PostCache.IncludeQueryString != false {
			t.Error("expected PostCache.IncludeQueryString to be false by default")
		}
		if cfg.Cache.PostCache.MaxRequestBodySizeMB != 10 {
			t.Errorf("got MaxRequestBodySizeMB %d, want 10", cfg.Cache.PostCache.MaxRequestBodySizeMB)
		}
		if cfg.Cache.PostCache.MaxResponseBodySizeMB != 10 {
			t.Errorf("got MaxResponseBodySizeMB %d, want 10", cfg.Cache.PostCache.MaxResponseBodySizeMB)
		}
	})

	t.Run("Load from file", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-postcache")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		configFile := filepath.Join(tmpDir, "gocache.toml")
		content := `
[cache.post_cache]
enable = true
include_query_string = true
max_request_body_size_mb = 20
max_response_body_size_mb = 25
`
		if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := LoadConfig(configFile)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		if !cfg.Cache.PostCache.Enable {
			t.Error("expected PostCache.Enable to be true")
		}
		if !cfg.Cache.PostCache.IncludeQueryString {
			t.Error("expected PostCache.IncludeQueryString to be true")
		}
		if cfg.Cache.PostCache.MaxRequestBodySizeMB != 20 {
			t.Errorf("got MaxRequestBodySizeMB %d, want 20", cfg.Cache.PostCache.MaxRequestBodySizeMB)
		}
		if cfg.Cache.PostCache.MaxResponseBodySizeMB != 25 {
			t.Errorf("got MaxResponseBodySizeMB %d, want 25", cfg.Cache.PostCache.MaxResponseBodySizeMB)
		}
	})

	t.Run("Size limits capped", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-postcache-limits")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		configFile := filepath.Join(tmpDir, "gocache.toml")
		content := `
[cache.post_cache]
max_request_body_size_mb = 100
max_response_body_size_mb = 200
`
		if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := LoadConfig(configFile)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		if cfg.Cache.PostCache.MaxRequestBodySizeMB != MaxPostCacheBodySizeMB {
			t.Errorf("expected MaxRequestBodySizeMB to be capped at %d, got %d", MaxPostCacheBodySizeMB, cfg.Cache.PostCache.MaxRequestBodySizeMB)
		}
		if cfg.Cache.PostCache.MaxResponseBodySizeMB != MaxPostCacheBodySizeMB {
			t.Errorf("expected MaxResponseBodySizeMB to be capped at %d, got %d", MaxPostCacheBodySizeMB, cfg.Cache.PostCache.MaxResponseBodySizeMB)
		}
	})
}
