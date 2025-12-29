package logging

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccessLogEntry(t *testing.T) {
	entry := AccessLogEntry{
		Timestamp:   time.Date(2024, 8, 18, 14, 30, 45, 0, time.UTC),
		CacheStatus: "HIT",
		Status:      200,
		Method:      "GET",
		Size:        1024,
		Duration:    15,
		URL:         "https://example.com/api/data?id=123",
		ContentType: "application/json",
	}

	if entry.Status != 200 {
		t.Errorf("expected status 200, got %d", entry.Status)
	}
	if entry.CacheStatus != "HIT" {
		t.Errorf("expected cache status HIT, got %s", entry.CacheStatus)
	}
}

func TestAccessLoggerHumanFormat(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	config := AccessLoggerConfig{
		Format:        FormatHuman,
		StdoutEnabled: false,
		LogFile:       logFile,
		BufferSize:    10,
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Log an entry
	entry := AccessLogEntry{
		Timestamp:   time.Date(2024, 8, 18, 14, 30, 45, 0, time.UTC),
		CacheStatus: "HIT",
		Status:      200,
		Method:      "GET",
		Size:        1024,
		Duration:    15,
		URL:         "https://example.com/api/data",
		ContentType: "application/json",
	}

	logger.Log(entry)

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Read the log file
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	expected := "2024-08-18T14:30:45Z HIT 200 GET 1024 15 https://example.com/api/data application/json"
	line := strings.TrimSpace(string(content))
	if line != expected {
		t.Errorf("expected %q, got %q", expected, line)
	}
}

func TestAccessLoggerJSONFormat(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	config := AccessLoggerConfig{
		Format:        FormatJSON,
		StdoutEnabled: false,
		LogFile:       logFile,
		BufferSize:    10,
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Log an entry
	entry := AccessLogEntry{
		Timestamp:   time.Date(2024, 8, 18, 14, 30, 45, 0, time.UTC),
		CacheStatus: "MISS",
		Status:      404,
		Method:      "GET",
		Size:        512,
		Duration:    8,
		URL:         "https://example.com/missing.html",
		ContentType: "text/html",
	}

	logger.Log(entry)

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Read and parse the log file
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	var parsed struct {
		Timestamp   string `json:"timestamp"`
		CacheStatus string `json:"cache_status"`
		Status      int    `json:"status"`
		Method      string `json:"method"`
		Size        int64  `json:"size"`
		DurationMs  int64  `json:"duration_ms"`
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	}

	line := strings.TrimSpace(string(content))
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.CacheStatus != "MISS" {
		t.Errorf("expected cache status MISS, got %s", parsed.CacheStatus)
	}
	if parsed.Status != 404 {
		t.Errorf("expected status 404, got %d", parsed.Status)
	}
	if parsed.Size != 512 {
		t.Errorf("expected size 512, got %d", parsed.Size)
	}
}

func TestAccessLoggerEmptyFields(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	config := AccessLoggerConfig{
		Format:        FormatHuman,
		StdoutEnabled: false,
		LogFile:       logFile,
		BufferSize:    10,
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Log entry with empty cache status and content type
	entry := AccessLogEntry{
		Timestamp:   time.Date(2024, 8, 18, 14, 30, 47, 0, time.UTC),
		CacheStatus: "", // Non-cacheable request
		Status:      201,
		Method:      "POST",
		Size:        256,
		Duration:    45,
		URL:         "https://example.com/api/submit",
		ContentType: "", // No content type
	}

	logger.Log(entry)

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Read the log file
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	expected := `2024-08-18T14:30:47Z "" 201 POST 256 45 https://example.com/api/submit ""`
	line := strings.TrimSpace(string(content))
	if line != expected {
		t.Errorf("expected %q, got %q", expected, line)
	}
}

func TestAccessLoggerLogRequest(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	config := AccessLoggerConfig{
		Format:        FormatHuman,
		StdoutEnabled: false,
		LogFile:       logFile,
		BufferSize:    10,
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Use convenience method
	logger.LogRequest("GET", "https://example.com/test", "HIT", 200, 1024, 25*time.Millisecond, "text/html")

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Read the log file
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	line := strings.TrimSpace(string(content))
	if !strings.Contains(line, "HIT 200 GET 1024 25 https://example.com/test text/html") {
		t.Errorf("log line doesn't contain expected fields: %s", line)
	}
}

func TestCountingResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	crw := NewCountingResponseWriter(rec)

	// Test default status code
	if crw.StatusCode() != 200 {
		t.Errorf("expected default status 200, got %d", crw.StatusCode())
	}

	// Set status code first (before writing)
	crw.WriteHeader(404)
	if crw.StatusCode() != 404 {
		t.Errorf("expected status 404, got %d", crw.StatusCode())
	}

	// Write some data
	data1 := []byte("Hello")
	n1, err := crw.Write(data1)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	if n1 != len(data1) {
		t.Errorf("expected to write %d bytes, got %d", len(data1), n1)
	}

	// Write more data
	data2 := []byte(" World!")
	_, err = crw.Write(data2)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Check total size
	expectedSize := int64(len(data1) + len(data2))
	if crw.Size() != expectedSize {
		t.Errorf("expected size %d, got %d", expectedSize, crw.Size())
	}

	// Check that the underlying recorder got the data
	if rec.Body.String() != "Hello World!" {
		t.Errorf("expected body 'Hello World!', got %q", rec.Body.String())
	}
	if rec.Code != 404 {
		t.Errorf("expected recorder status 404, got %d", rec.Code)
	}
}

func TestAccessLoggerErrorHandling(t *testing.T) {
	var errorCalled bool
	var errorMsg string

	config := AccessLoggerConfig{
		Format:        FormatHuman,
		StdoutEnabled: false,
		LogFile:       "",
		BufferSize:    1, // Small buffer to test overflow
		ErrorHandler: func(err error) {
			errorCalled = true
			errorMsg = err.Error()
		},
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Fill the buffer and overflow it
	for i := 0; i < 10; i++ {
		entry := AccessLogEntry{
			Timestamp: time.Now(),
			Status:    200,
			Method:    "GET",
			URL:       "https://example.com/test",
		}
		logger.Log(entry)
	}

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	if !errorCalled {
		t.Error("expected error handler to be called for buffer overflow")
	}
	if !strings.Contains(errorMsg, "buffer full") {
		t.Errorf("expected buffer full error, got: %s", errorMsg)
	}
}

func TestAccessLoggerInvalidFile(t *testing.T) {
	var receivedError error
	errorHandler := func(err error) {
		receivedError = err
	}

	config := AccessLoggerConfig{
		Format:        FormatHuman,
		StdoutEnabled: false,
		LogFile:       "/invalid/path/access.log",
		BufferSize:    10,
		ErrorHandler:  errorHandler,
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Errorf("NewAccessLogger should not return error for invalid file path, got: %v", err)
	}
	defer logger.Close()

	// Verify that an error was reported through the error handler
	if receivedError == nil {
		t.Error("expected error to be reported through error handler for invalid log file path")
	} else if !strings.Contains(receivedError.Error(), "failed to open access log file") {
		t.Errorf("expected file open error, got: %v", receivedError)
	}
}

func TestAccessLoggerClose(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	config := AccessLoggerConfig{
		Format:        FormatHuman,
		StdoutEnabled: false,
		LogFile:       logFile,
		BufferSize:    10,
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Log some entries
	for i := 0; i < 5; i++ {
		entry := AccessLogEntry{
			Timestamp: time.Now(),
			Status:    200,
			Method:    "GET",
			URL:       "https://example.com/test",
		}
		logger.Log(entry)
	}

	// Close should wait for all entries to be processed
	err = logger.Close()
	if err != nil {
		t.Fatalf("failed to close logger: %v", err)
	}

	// Should be able to close multiple times
	err = logger.Close()
	if err != nil {
		t.Fatalf("failed to close logger second time: %v", err)
	}
}

func TestAccessLoggerMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "access.log")

	config := AccessLoggerConfig{
		Format:        FormatHuman,
		StdoutEnabled: false,
		LogFile:       logFile,
		BufferSize:    5, // Small buffer to test overflow
		ErrorHandler:  DefaultErrorHandler,
	}

	logger, err := NewAccessLogger(config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Check initial metrics
	metrics := logger.GetMetrics()
	if metrics.EntriesLogged != 0 || metrics.EntriesDropped != 0 || metrics.WriteErrors != 0 {
		t.Errorf("expected zero metrics initially, got %+v", metrics)
	}

	// Log some entries
	for i := 0; i < 3; i++ {
		entry := AccessLogEntry{
			Timestamp: time.Now(),
			Status:    200,
			Method:    "GET",
			URL:       "https://example.com/test",
		}
		logger.Log(entry)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check metrics
	metrics = logger.GetMetrics()
	if metrics.EntriesLogged != 3 {
		t.Errorf("expected 3 entries logged, got %d", metrics.EntriesLogged)
	}
	if metrics.EntriesDropped != 0 {
		t.Errorf("expected 0 entries dropped, got %d", metrics.EntriesDropped)
	}

	// Overflow the buffer
	for i := 0; i < 10; i++ {
		entry := AccessLogEntry{
			Timestamp: time.Now(),
			Status:    200,
			Method:    "GET",
			URL:       "https://example.com/test",
		}
		logger.Log(entry)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check that some entries were dropped
	metrics = logger.GetMetrics()
	if metrics.EntriesDropped == 0 {
		t.Error("expected some entries to be dropped due to buffer overflow")
	}

	t.Logf("Final metrics: %+v", metrics)

	// Test reset
	logger.ResetMetrics()
	metrics = logger.GetMetrics()
	if metrics.EntriesLogged != 0 || metrics.EntriesDropped != 0 || metrics.WriteErrors != 0 {
		t.Errorf("expected zero metrics after reset, got %+v", metrics)
	}
}
