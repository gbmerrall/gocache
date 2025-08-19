package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAccessLoggerResilience tests various failure scenarios
func TestAccessLoggerResilience(t *testing.T) {
	t.Run("readonly_directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		
		// Make directory readonly
		err := os.Chmod(tmpDir, 0555)
		if err != nil {
			t.Fatalf("failed to make directory readonly: %v", err)
		}
		defer os.Chmod(tmpDir, 0755) // Restore permissions for cleanup

		logFile := filepath.Join(tmpDir, "access.log")

		config := AccessLoggerConfig{
			Format:        FormatHuman,
			StdoutEnabled: false,
			LogFile:       logFile,
			BufferSize:    10,
			ErrorHandler:  DefaultErrorHandler,
		}

		var receivedError error
		config.ErrorHandler = func(err error) {
			receivedError = err
		}

		// Should succeed in creating logger but report error through error handler
		logger, err := NewAccessLogger(config)
		if err != nil {
			t.Errorf("NewAccessLogger should not return error for readonly directory, got: %v", err)
		}
		defer logger.Close()
		
		// Verify that an error was reported through the error handler
		if receivedError == nil {
			t.Error("expected error to be reported through error handler for readonly directory")
		} else if !strings.Contains(receivedError.Error(), "failed to open access log file") {
			t.Errorf("expected file open error, got: %v", receivedError)
		} else {
			t.Logf("Correctly reported file error: %v", receivedError)
		}
	})

	t.Run("directory_removed", func(t *testing.T) {
		tmpDir := t.TempDir()
		logDir := filepath.Join(tmpDir, "logs")
		err := os.Mkdir(logDir, 0755)
		if err != nil {
			t.Fatalf("failed to create log directory: %v", err)
		}

		logFile := filepath.Join(logDir, "access.log")

		var errors []string
		config := AccessLoggerConfig{
			Format:        FormatHuman,
			StdoutEnabled: false,
			LogFile:       logFile,
			BufferSize:    10,
			ErrorHandler: func(err error) {
				errors = append(errors, err.Error())
			},
		}

		logger, err := NewAccessLogger(config)
		if err != nil {
			t.Fatalf("failed to create logger: %v", err)
		}
		defer logger.Close()

		// Log something first (should work)
		entry := AccessLogEntry{
			Timestamp: time.Now(),
			Status:    200,
			Method:    "GET",
			URL:       "https://example.com/test",
		}
		logger.Log(entry)
		time.Sleep(50 * time.Millisecond)

		// Close the file handle and remove directory
		logger.Close()
		os.RemoveAll(logDir)

		var receivedError error
		config.ErrorHandler = func(err error) {
			receivedError = err
		}

		// Try to create a new logger with the same path (should succeed but report error)
		logger2, err := NewAccessLogger(config)
		if err != nil {
			t.Errorf("NewAccessLogger should not return error for removed directory, got: %v", err)
		}
		defer logger2.Close()
		
		// Verify that an error was reported through the error handler
		if receivedError == nil {
			t.Error("expected error to be reported through error handler for removed directory")
		} else if !strings.Contains(receivedError.Error(), "failed to open access log file") {
			t.Errorf("expected file open error, got: %v", receivedError)
		}
	})

	t.Run("high_volume_stress", func(t *testing.T) {
		tmpDir := t.TempDir()
		logFile := filepath.Join(tmpDir, "access.log")

		var errorCount int
		config := AccessLoggerConfig{
			Format:        FormatHuman,
			StdoutEnabled: false,
			LogFile:       logFile,
			BufferSize:    100, // Small buffer to force overflow
			ErrorHandler: func(err error) {
				errorCount++
			},
		}

		logger, err := NewAccessLogger(config)
		if err != nil {
			t.Fatalf("failed to create logger: %v", err)
		}
		defer logger.Close()

		// Generate high volume of log entries
		for i := 0; i < 1000; i++ {
			entry := AccessLogEntry{
				Timestamp: time.Now(),
				Status:    200,
				Method:    "GET",
				URL:       "https://example.com/test",
				Size:      1024,
			}
			logger.Log(entry)
		}

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Some entries might be dropped due to buffer overflow
		// But the logger should handle it gracefully
		t.Logf("Errors during high volume test: %d", errorCount)

		// Check that some data was written
		content, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
			t.Error("no log entries were written")
		}

		t.Logf("Successfully wrote %d log entries", len(lines))
	})

	t.Run("concurrent_access", func(t *testing.T) {
		tmpDir := t.TempDir()
		logFile := filepath.Join(tmpDir, "access.log")

		config := AccessLoggerConfig{
			Format:        FormatHuman,
			StdoutEnabled: false,
			LogFile:       logFile,
			BufferSize:    1000,
			ErrorHandler:  DefaultErrorHandler,
		}

		logger, err := NewAccessLogger(config)
		if err != nil {
			t.Fatalf("failed to create logger: %v", err)
		}
		defer logger.Close()

		// Start multiple goroutines writing concurrently
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func(id int) {
				for j := 0; j < 100; j++ {
					entry := AccessLogEntry{
						Timestamp: time.Now(),
						Status:    200,
						Method:    "GET",
						URL:       "https://example.com/test",
						Size:      int64(id*100 + j),
					}
					logger.Log(entry)
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		// Give time for async processing
		time.Sleep(300 * time.Millisecond)

		// Check that data was written
		content, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) < 500 {
			t.Errorf("expected at least 500 log entries, got %d", len(lines))
		}

		t.Logf("Successfully wrote %d log entries from concurrent access", len(lines))
	})
}

// TestErrorRecovery tests that the proxy can handle access logging errors gracefully
func TestErrorRecovery(t *testing.T) {
	t.Run("invalid_log_format", func(t *testing.T) {
		config := AccessLoggerConfig{
			Format:        "invalid",
			StdoutEnabled: false,
			LogFile:       "",
			BufferSize:    10,
		}

		// Should create logger but format will be corrected to default
		logger, err := NewAccessLogger(config)
		if err != nil {
			t.Fatalf("failed to create logger: %v", err)
		}
		defer logger.Close()

		// The logger should still work with default format
		entry := AccessLogEntry{
			Timestamp: time.Now(),
			Status:    200,
			Method:    "GET",
			URL:       "https://example.com/test",
		}
		logger.Log(entry)
	})

	t.Run("nil_error_handler", func(t *testing.T) {
		tmpDir := t.TempDir()
		logFile := filepath.Join(tmpDir, "access.log")

		config := AccessLoggerConfig{
			Format:        FormatHuman,
			StdoutEnabled: false,
			LogFile:       logFile,
			BufferSize:    1,
			ErrorHandler:  nil, // No error handler
		}

		logger, err := NewAccessLogger(config)
		if err != nil {
			t.Fatalf("failed to create logger: %v", err)
		}
		defer logger.Close()

		// Fill buffer to trigger overflow (with no error handler)
		for i := 0; i < 10; i++ {
			entry := AccessLogEntry{
				Timestamp: time.Now(),
				Status:    200,
				Method:    "GET",
				URL:       "https://example.com/test",
			}
			logger.Log(entry)
		}

		// Should not panic
		time.Sleep(100 * time.Millisecond)
	})
}