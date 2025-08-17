package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient(8081)
	if client.baseURL != "http://127.0.0.1:8081" {
		t.Errorf("expected base URL http://127.0.0.1:8081, got %s", client.baseURL)
	}
	if client.httpClient == nil {
		t.Error("expected http client to be initialized")
	}
}

func TestRun(t *testing.T) {
	t.Run("No command provided", func(t *testing.T) {
		err := Run(8081, []string{})
		if err == nil {
			t.Error("expected error when no command provided")
		}
		if err.Error() != "no command provided" {
			t.Errorf("expected 'no command provided' error, got %v", err)
		}
	})

	t.Run("Unknown command", func(t *testing.T) {
		err := Run(8081, []string{"unknown"})
		if err == nil {
			t.Error("expected error for unknown command")
		}
		if err.Error() != "unknown command: unknown" {
			t.Errorf("expected 'unknown command' error, got %v", err)
		}
	})

	t.Run("Purge command without domain", func(t *testing.T) {
		err := Run(8081, []string{"purge"})
		if err == nil {
			t.Error("expected error for purge without domain")
		}
		if err.Error() != "domain required for purge command" {
			t.Errorf("expected 'domain required' error, got %v", err)
		}
	})

	t.Run("PurgeURL command without URL", func(t *testing.T) {
		err := Run(8081, []string{"purge-url"})
		if err == nil {
			t.Error("expected error for purge-url without URL")
		}
		if err.Error() != "url required for purge-url command" {
			t.Errorf("expected 'url required' error, got %v", err)
		}
	})
}

func TestGetStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		response := map[string]interface{}{
			"hit_count":        100,
			"miss_count":       50,
			"hit_rate_percent": "66.67",
			"entry_count":      25,
			"uptime_seconds":   "3600.00",
			"cache_size_bytes": 1024000,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	err := client.GetStatus()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	err := client.GetStatus()
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestPurgeAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/purge/all" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		response := map[string]interface{}{
			"message": "Cache purged successfully",
			"count":   10,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	err := client.PurgeAll()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPurgeURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/purge/url" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var reqBody map[string]string
		json.NewDecoder(r.Body).Decode(&reqBody)

		if reqBody["url"] != "https://example.com/test" {
			http.Error(w, "Invalid URL", http.StatusBadRequest)
			return
		}

		response := map[string]interface{}{
			"purged": true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	err := client.PurgeURL("https://example.com/test")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPurgeDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/purge/domain/example.com" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		response := map[string]interface{}{
			"message": "Domain purged successfully",
			"count":   5,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	err := client.PurgeDomain("example.com")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExportCA(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-ca")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ca" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", "attachment; filename=\"gocache-ca.crt\"")
		w.Write([]byte("-----BEGIN CERTIFICATE-----\nMII...\n-----END CERTIFICATE-----\n"))
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	// Test with default filename
	err = client.ExportCA(filepath.Join(tmpDir, "gocache-ca.crt"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Test with custom filename
	err = client.ExportCA(filepath.Join(tmpDir, "custom-ca.crt"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExportCAError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Certificate not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	err := client.ExportCA("test.crt")
	if err == nil {
		t.Error("expected error for certificate not found")
	}
}

func TestStopDaemon(t *testing.T) {
	// This is a simplified test that only checks the error path.
	// A more comprehensive test would require mocking the os and syscall packages.
	err := stopDaemon()
	if err == nil {
		t.Error("expected an error when pidfile does not exist")
	}
}

func TestExportCAFileWriteError(t *testing.T) {
	// Create a mock server that returns CA data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write([]byte("-----BEGIN CERTIFICATE-----\nMOCK_CA_DATA\n-----END CERTIFICATE-----"))
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Test export-ca with invalid file path (should fail to write)
	err := client.ExportCA("/invalid/path/ca.crt")
	if err == nil {
		t.Error("expected error for invalid file path")
	}
}
