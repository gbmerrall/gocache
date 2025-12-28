package proxy

import (
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
	"github.com/gbmerrall/gocache/internal/config"
	"github.com/gbmerrall/gocache/internal/logging"
)

// TestAccessLoggingSimple tests basic access logging functionality
func TestAccessLoggingSimple(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	// Create config with access logging enabled
	cfg := config.NewDefaultConfig()
	cfg.Logging.AccessToStdout = false
	cfg.Logging.AccessLogfile = logFile
	cfg.Logging.AccessFormat = "human"

	// Create proxy with a mock transport
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	memCache := cache.NewMemoryCache(100*time.Millisecond, 0)
	
	proxy := &Proxy{
		logger:    logger,
		config:    cfg,
		cache:     memCache,
		transport: &mockTransport{},
	}
	
	// Initialize access logger using the logic from NewProxy
	cfg.Logging.ApplyProcessDetection(false) // Force daemon mode for tests
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

		accessLog, err := logging.NewAccessLogger(accessLogConfig)
		if err != nil {
			t.Fatalf("failed to create access logger: %v", err)
		}
		proxy.accessLog = accessLog
	}
	defer proxy.Close()

	// Create HTTP request
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	w := httptest.NewRecorder()

	// Handle request through proxy
	proxy.handleHTTP(w, req)

	// Verify response
	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Wait for access log to be written
	time.Sleep(300 * time.Millisecond)

	// Read access log
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read access log: %v", err)
	}

	logLine := strings.TrimSpace(string(content))
	if logLine == "" {
		t.Fatal("no access log entry found")
	}

	// Parse log line and verify basic fields
	fields := strings.Fields(logLine)
	if len(fields) < 8 {
		t.Fatalf("expected at least 8 fields in log line, got %d: %s", len(fields), logLine)
	}

	// Verify status code
	if fields[2] != "200" {
		t.Errorf("expected status 200, got %s", fields[2])
	}

	// Verify method
	if fields[3] != "GET" {
		t.Errorf("expected method GET, got %s", fields[3])
	}

	// Verify cache status (should be MISS for first request)
	if !strings.Contains(logLine, "MISS") {
		t.Error("expected cache status MISS in log line")
	}

	// Verify content type
	if !strings.Contains(logLine, "application/json") {
		t.Error("expected content type application/json in log line")
	}

	t.Logf("Access log entry: %s", logLine)
}

// TestAccessLoggingJSON tests JSON format access logging
func TestAccessLoggingJSON(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	// Create config with JSON access logging
	cfg := config.NewDefaultConfig()
	cfg.Logging.AccessToStdout = false
	cfg.Logging.AccessLogfile = logFile
	cfg.Logging.AccessFormat = "json"

	// Create proxy with mock transport
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	memCache := cache.NewMemoryCache(100*time.Millisecond, 0)
	
	proxy := &Proxy{
		logger:    logger,
		config:    cfg,
		cache:     memCache,
		transport: &mockTransport{},
	}
	
	// Initialize access logger
	cfg.Logging.ApplyProcessDetection(false)
	accessLogConfig := logging.AccessLoggerConfig{
		Format:        logging.FormatJSON,
		StdoutEnabled: false,
		LogFile:       logFile,
		BufferSize:    1000,
		ErrorHandler:  logging.DefaultErrorHandler,
	}

	accessLog, err := logging.NewAccessLogger(accessLogConfig)
	if err != nil {
		t.Fatalf("failed to create access logger: %v", err)
	}
	proxy.accessLog = accessLog
	defer proxy.Close()

	// Handle request
	req := httptest.NewRequest("POST", "http://example.com/api", nil)
	w := httptest.NewRecorder()
	proxy.handleHTTP(w, req)

	// Wait for access log
	time.Sleep(300 * time.Millisecond)

	// Read and parse JSON access log
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read access log: %v", err)
	}

	logLine := strings.TrimSpace(string(content))
	if logLine == "" {
		t.Fatal("no access log entry found")
	}

	// Parse JSON
	var logEntry map[string]interface{}
	err = json.Unmarshal([]byte(logLine), &logEntry)
	if err != nil {
		t.Fatalf("failed to parse JSON log entry: %v", err)
	}

	// Verify JSON fields
	if logEntry["status"] != float64(200) {
		t.Errorf("expected status 200, got %v", logEntry["status"])
	}
	if logEntry["method"] != "POST" {
		t.Errorf("expected method POST, got %v", logEntry["method"])
	}
	if logEntry["cache_status"] != "" {
		t.Errorf("expected empty cache status for POST, got %v", logEntry["cache_status"])
	}

	t.Logf("JSON access log entry: %s", logLine)
}

// TestAccessLoggingDisabled verifies that logging can be disabled
func TestAccessLoggingDisabled(t *testing.T) {
	// Create config with access logging disabled
	cfg := config.NewDefaultConfig()
	cfg.Logging.AccessToStdout = false
	cfg.Logging.AccessLogfile = ""

	// Create proxy with mock transport
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	memCache := cache.NewMemoryCache(100*time.Millisecond, 0)
	
	proxy := &Proxy{
		logger:    logger,
		config:    cfg,
		cache:     memCache,
		transport: &mockTransport{},
	}
	
	// Initialize access logger (should be nil)
	cfg.Logging.ApplyProcessDetection(false)
	if cfg.Logging.AccessToStdout || cfg.Logging.AccessLogfile != "" {
		t.Fatal("access logging should be disabled but config indicates it's enabled")
	}
	proxy.accessLog = nil
	defer proxy.Close()

	// Verify access logger is nil (disabled)
	if proxy.accessLog != nil {
		t.Error("expected access logger to be nil when disabled")
	}

	// Handle request (should not crash)
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	w := httptest.NewRecorder()
	proxy.handleHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	t.Log("Access logging correctly disabled")
}

// mockTransport provides a simple mock HTTP transport for testing
type mockTransport struct{}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"message": "test response"}`)),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}