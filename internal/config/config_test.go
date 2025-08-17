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
	})
}