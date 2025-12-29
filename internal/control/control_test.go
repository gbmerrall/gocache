package control

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gbmerrall/gocache/internal/cache"
	"github.com/gbmerrall/gocache/internal/cert"
	"github.com/gbmerrall/gocache/internal/config"
	"github.com/gbmerrall/gocache/internal/proxy"
)

func setupTestAPI(t *testing.T) (*ControlAPI, func()) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-control")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	cert.SetCertDir(tmpDir)

	cfg := config.NewDefaultConfig()
	c := cache.NewMemoryCache(1*time.Minute, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := proxy.NewProxy(logger, c, cfg)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	api := NewControlAPI(logger, cfg, c, p, func() {})

	return api, func() {
		os.RemoveAll(tmpDir)
	}
}

func TestControlAPI(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/purge/domain/") {
			api.handlePurgeDomain(w, r)
		} else {
			switch r.URL.Path {
			case "/stats":
				api.handleStats(w, r)
			case "/purge/all":
				api.handlePurgeAll(w, r)
			case "/purge/url":
				api.handlePurgeURL(w, r)
			case "/ca":
				api.handleCA(w, r)
			case "/health":
				api.handleHealth(w, r)
			case "/reload":
				api.handleReload(w, r)
			case "/shutdown":
				api.handleShutdown(w, r)
			default:
				http.NotFound(w, r)
			}
		}
	}))
	defer ts.Close()

	t.Run("Stats endpoint", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/stats")
		if err != nil {
			t.Fatalf("failed to get /stats: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("Purge All endpoint", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/purge/all", "application/json", nil)
		if err != nil {
			t.Fatalf("failed to post /purge/all: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("Purge URL endpoint", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"url": "http://example.com"})
		resp, err := http.Post(ts.URL+"/purge/url", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("failed to post /purge/url: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("Purge URL endpoint with bad json", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/purge/url", "application/json", strings.NewReader("{"))
		if err != nil {
			t.Fatalf("failed to post /purge/url: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("Purge URL endpoint with no url", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"url": ""})
		resp, err := http.Post(ts.URL+"/purge/url", "application/json", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("failed to post /purge/url: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("Purge Domain endpoint", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/purge/domain/example.com", "application/json", nil)
		if err != nil {
			t.Fatalf("failed to post /purge/domain: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("CA endpoint", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/ca")
		if err != nil {
			t.Fatalf("failed to get /ca: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("Health endpoint", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("failed to get /health: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("Reload endpoint", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/reload", "application/json", nil)
		if err != nil {
			t.Fatalf("failed to post /reload: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("Shutdown endpoint", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/shutdown", "application/json", nil)
		if err != nil {
			t.Fatalf("failed to post /shutdown: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})
}

func TestReloadConfig(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "gocache-test-config")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `
[cache]
default_ttl = "2h"
`
	configFile := filepath.Join(tmpDir, "test.toml")
	err = os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Set the config path
	api.config.LoadedPath = configFile

	// Test reload
	err = api.ReloadConfig()
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	// Verify the config was updated
	if api.config.Cache.DefaultTTL != "2h" {
		t.Errorf("expected TTL 2h, got %s", api.config.Cache.DefaultTTL)
	}
}

func TestReloadConfigError(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test reload with non-existent file
	api.config.LoadedPath = "nonexistent.toml"

	err := api.ReloadConfig()
	if err == nil {
		t.Fatal("expected error for non-existent config file")
	}
}

func TestHandleStatsMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test POST request (should be method not allowed)
	req, err := http.NewRequest(http.MethodPost, "/stats", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleStats(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandlePurgeAllMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test GET request (should be method not allowed)
	req, err := http.NewRequest(http.MethodGet, "/purge/all", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handlePurgeAll(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandlePurgeURLMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test GET request (should be method not allowed)
	req, err := http.NewRequest(http.MethodGet, "/purge/url", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handlePurgeURL(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandlePurgeDomainMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test GET request (should be method not allowed)
	req, err := http.NewRequest(http.MethodGet, "/purge/domain/example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handlePurgeDomain(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleCAMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test POST request (should be method not allowed)
	req, err := http.NewRequest(http.MethodPost, "/ca", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleCA(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleHealthMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test POST request (should be method not allowed)
	req, err := http.NewRequest(http.MethodPost, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleHealth(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleReloadMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test GET request (should be method not allowed)
	req, err := http.NewRequest(http.MethodGet, "/reload", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleReload(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleShutdownMethodNotAllowed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test GET request (should be method not allowed)
	req, err := http.NewRequest(http.MethodGet, "/shutdown", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleShutdown(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleReloadError(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test reload with invalid config path
	api.config.LoadedPath = "nonexistent.toml"

	req, err := http.NewRequest(http.MethodPost, "/reload", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleReload(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandlePurgeURLInvalidJSON(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test with malformed JSON
	req, err := http.NewRequest(http.MethodPost, "/purge/url", strings.NewReader("{"))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	api.handlePurgeURL(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandlePurgeURLMissingURL(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test with missing URL field
	body := `{"other_field": "value"}`
	req, err := http.NewRequest(http.MethodPost, "/purge/url", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	api.handlePurgeURL(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandlePurgeDomainInvalidDomain(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test with invalid domain in URL
	req, err := http.NewRequest(http.MethodPost, "/purge/domain/", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handlePurgeDomain(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCAError(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test CA endpoint with potential error
	req, err := http.NewRequest(http.MethodGet, "/ca", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleCA(w, req)

	// Should still return 200 even if there are issues
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleHealthDetailed(t *testing.T) {
	api, cleanup := setupTestAPI(t)
	defer cleanup()

	// Test health endpoint
	req, err := http.NewRequest(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	api.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check response body
	body := w.Body.String()
	if !strings.Contains(body, "status") {
		t.Error("health response should contain status field")
	}
}
