package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// AccessLogEntry represents a single access log entry
type AccessLogEntry struct {
	Timestamp   time.Time
	CacheStatus string // "HIT", "MISS", or "" for non-cacheable
	Status      int
	Method      string
	Size        int64  // Response size in bytes
	Duration    int64  // Response time in milliseconds
	URL         string
	ContentType string
}

// AccessLogFormat represents the output format for access logs
type AccessLogFormat string

const (
	FormatHuman AccessLogFormat = "human"
	FormatJSON  AccessLogFormat = "json"
)

// AccessLogger handles async access logging to multiple outputs
type AccessLogger struct {
	mu      sync.RWMutex
	entries chan AccessLogEntry
	done    chan struct{}
	wg      sync.WaitGroup
	closed  bool
	
	// Configuration
	format      AccessLogFormat
	stdoutEnabled bool
	fileWriter   io.WriteCloser
	
	// Error handling
	errorHandler func(error)
	
	// Metrics (protected by mu)
	entriesLogged  uint64
	entriesDropped uint64
	writeErrors    uint64
}

// AccessLoggerConfig configures an AccessLogger
type AccessLoggerConfig struct {
	Format        AccessLogFormat
	StdoutEnabled bool
	LogFile       string
	BufferSize    int // Channel buffer size, default 1000
	ErrorHandler  func(error) // Optional error handler
}

// NewAccessLogger creates a new access logger with the given configuration
// This function is designed to be resilient - it will always return a logger,
// even if file operations fail. File errors are handled gracefully by continuing
// operation without file logging and reporting errors through the error handler.
func NewAccessLogger(config AccessLoggerConfig) (*AccessLogger, error) {
	if config.BufferSize <= 0 {
		config.BufferSize = 1000
	}
	
	logger := &AccessLogger{
		entries:       make(chan AccessLogEntry, config.BufferSize),
		done:          make(chan struct{}),
		format:        config.Format,
		stdoutEnabled: config.StdoutEnabled,
		errorHandler:  config.ErrorHandler,
	}
	
	// Set up file writer if specified - handle errors gracefully
	if config.LogFile != "" {
		file, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// Don't fail initialization - log the error and continue without file logging
			if logger.errorHandler != nil {
				logger.errorHandler(fmt.Errorf("failed to open access log file %s, continuing without file logging: %w", config.LogFile, err))
			} else {
				// Fallback to default error handling if no handler provided
				log.Printf("access log error: failed to open log file %s, continuing without file logging: %v", config.LogFile, err)
			}
		} else {
			logger.fileWriter = file
		}
	}
	
	// Start background goroutine
	logger.wg.Add(1)
	go logger.worker()
	
	return logger, nil
}

// Log adds an entry to the access log (non-blocking)
func (al *AccessLogger) Log(entry AccessLogEntry) {
	select {
	case al.entries <- entry:
		// Entry queued successfully
		al.mu.Lock()
		al.entriesLogged++
		al.mu.Unlock()
	default:
		// Channel full - handle error
		al.mu.Lock()
		al.entriesDropped++
		al.mu.Unlock()
		if al.errorHandler != nil {
			al.errorHandler(fmt.Errorf("access log buffer full, dropping entry"))
		}
	}
}

// LogRequest is a convenience method to log HTTP request data
func (al *AccessLogger) LogRequest(method, url, cacheStatus string, status int, size int64, duration time.Duration, contentType string) {
	entry := AccessLogEntry{
		Timestamp:   time.Now(),
		CacheStatus: cacheStatus,
		Status:      status,
		Method:      method,
		Size:        size,
		Duration:    duration.Milliseconds(),
		URL:         url,
		ContentType: contentType,
	}
	al.Log(entry)
}

// Close gracefully shuts down the access logger
func (al *AccessLogger) Close() error {
	al.mu.Lock()
	if al.closed {
		al.mu.Unlock()
		return nil
	}
	al.closed = true
	al.mu.Unlock()
	
	close(al.done)
	al.wg.Wait()
	
	al.mu.Lock()
	defer al.mu.Unlock()
	
	if al.fileWriter != nil {
		return al.fileWriter.Close()
	}
	return nil
}

// worker processes log entries in a background goroutine
func (al *AccessLogger) worker() {
	defer al.wg.Done()
	
	for {
		select {
		case entry := <-al.entries:
			al.writeEntry(entry)
		case <-al.done:
			// Drain remaining entries
			for {
				select {
				case entry := <-al.entries:
					al.writeEntry(entry)
				default:
					return
				}
			}
		}
	}
}

// writeEntry writes a single log entry to configured outputs
func (al *AccessLogger) writeEntry(entry AccessLogEntry) {
	var output string
	var err error
	
	switch al.format {
	case FormatHuman:
		output = al.formatHuman(entry)
	case FormatJSON:
		output, err = al.formatJSON(entry)
		if err != nil && al.errorHandler != nil {
			al.errorHandler(fmt.Errorf("failed to format JSON: %w", err))
			return
		}
	default:
		if al.errorHandler != nil {
			al.errorHandler(fmt.Errorf("unknown format: %s", al.format))
		}
		return
	}
	
	// Write to stdout if enabled
	if al.stdoutEnabled {
		if _, err := fmt.Fprintln(os.Stdout, output); err != nil {
			al.mu.Lock()
			al.writeErrors++
			al.mu.Unlock()
			if al.errorHandler != nil {
				al.errorHandler(fmt.Errorf("failed to write to stdout: %w", err))
			}
		}
	}
	
	// Write to file if configured
	al.mu.RLock()
	fileWriter := al.fileWriter
	al.mu.RUnlock()
	
	if fileWriter != nil {
		if _, err := fmt.Fprintln(fileWriter, output); err != nil {
			al.mu.Lock()
			al.writeErrors++
			al.mu.Unlock()
			if al.errorHandler != nil {
				al.errorHandler(fmt.Errorf("failed to write to file: %w", err))
			}
		}
	}
}

// formatHuman formats the entry as space-separated fields
func (al *AccessLogger) formatHuman(entry AccessLogEntry) string {
	// Format: timestamp cache_status status method size duration_ms url content_type
	timestamp := entry.Timestamp.Format(time.RFC3339)
	cacheStatus := entry.CacheStatus
	if cacheStatus == "" {
		cacheStatus = `""`
	}
	contentType := entry.ContentType
	if contentType == "" {
		contentType = `""`
	}
	
	return fmt.Sprintf("%s %s %d %s %d %d %s %s",
		timestamp,
		cacheStatus,
		entry.Status,
		entry.Method,
		entry.Size,
		entry.Duration,
		entry.URL,
		contentType,
	)
}

// formatJSON formats the entry as JSON
func (al *AccessLogger) formatJSON(entry AccessLogEntry) (string, error) {
	jsonEntry := struct {
		Timestamp   string `json:"timestamp"`
		CacheStatus string `json:"cache_status"`
		Status      int    `json:"status"`
		Method      string `json:"method"`
		Size        int64  `json:"size"`
		DurationMs  int64  `json:"duration_ms"`
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	}{
		Timestamp:   entry.Timestamp.Format(time.RFC3339),
		CacheStatus: entry.CacheStatus,
		Status:      entry.Status,
		Method:      entry.Method,
		Size:        entry.Size,
		DurationMs:  entry.Duration,
		URL:         entry.URL,
		ContentType: entry.ContentType,
	}
	
	data, err := json.Marshal(jsonEntry)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// CountingResponseWriter wraps an http.ResponseWriter to count bytes written
type CountingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int64
}

// NewCountingResponseWriter creates a new CountingResponseWriter
func NewCountingResponseWriter(w http.ResponseWriter) *CountingResponseWriter {
	return &CountingResponseWriter{
		ResponseWriter: w,
		statusCode:     200, // Default status code
	}
}

// Write implements io.Writer and counts bytes
func (crw *CountingResponseWriter) Write(data []byte) (int, error) {
	n, err := crw.ResponseWriter.Write(data)
	crw.size += int64(n)
	return n, err
}

// WriteHeader captures the status code
func (crw *CountingResponseWriter) WriteHeader(statusCode int) {
	crw.statusCode = statusCode
	crw.ResponseWriter.WriteHeader(statusCode)
}

// StatusCode returns the HTTP status code
func (crw *CountingResponseWriter) StatusCode() int {
	return crw.statusCode
}

// Size returns the number of bytes written
func (crw *CountingResponseWriter) Size() int64 {
	return crw.size
}

// AccessLoggerMetrics contains metrics about the access logger's performance
type AccessLoggerMetrics struct {
	EntriesLogged  uint64 // Total entries successfully queued for logging
	EntriesDropped uint64 // Total entries dropped due to buffer overflow
	WriteErrors    uint64 // Total write errors (stdout/file)
}

// GetMetrics returns current metrics for the access logger
func (al *AccessLogger) GetMetrics() AccessLoggerMetrics {
	al.mu.RLock()
	defer al.mu.RUnlock()
	
	return AccessLoggerMetrics{
		EntriesLogged:  al.entriesLogged,
		EntriesDropped: al.entriesDropped,
		WriteErrors:    al.writeErrors,
	}
}

// ResetMetrics resets all metrics counters to zero
func (al *AccessLogger) ResetMetrics() {
	al.mu.Lock()
	defer al.mu.Unlock()
	
	al.entriesLogged = 0
	al.entriesDropped = 0
	al.writeErrors = 0
}

// DefaultErrorHandler provides a default error handler that logs to stderr
func DefaultErrorHandler(err error) {
	log.Printf("access log error: %v", err)
}