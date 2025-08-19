package config

import (
	"testing"
)

// TestLoggingDefaults verifies that application logging is disabled by default
func TestLoggingDefaults(t *testing.T) {
	cfg := NewDefaultConfig()
	
	// Test that application logging is disabled by default
	if cfg.Logging.Level != "" {
		t.Errorf("expected Level to be empty (disabled), got %q", cfg.Logging.Level)
	}
	
	if cfg.Logging.AppLevel != "" {
		t.Errorf("expected AppLevel to be empty (disabled), got %q", cfg.Logging.AppLevel)
	}
	
	// Test that GetEffectiveAppLevel returns empty string (disabled)
	effectiveLevel := cfg.Logging.GetEffectiveAppLevel()
	if effectiveLevel != "" {
		t.Errorf("expected effective app level to be empty (disabled), got %q", effectiveLevel)
	}
	
	// Test that access logging has proper defaults
	// Note: AccessToStdout depends on process detection, so we won't test its exact value
	if cfg.Logging.AccessLogfile != "" {
		t.Errorf("expected AccessLogfile to be empty by default, got %q", cfg.Logging.AccessLogfile)
	}
	
	if cfg.Logging.AccessFormat != "human" {
		t.Errorf("expected AccessFormat to be 'human', got %q", cfg.Logging.AccessFormat)
	}
	
	t.Log("Application logging is correctly disabled by default")
}

// TestLoggingBackwardCompatibility tests that existing configurations still work
func TestLoggingBackwardCompatibility(t *testing.T) {
	// Test that if someone sets the legacy Level field, it's respected
	cfg := NewDefaultConfig()
	cfg.Logging.Level = "debug"
	
	effectiveLevel := cfg.Logging.GetEffectiveAppLevel()
	if effectiveLevel != "debug" {
		t.Errorf("expected effective app level to be 'debug' from legacy field, got %q", effectiveLevel)
	}
	
	// Test that new AppLevel field takes precedence over legacy Level
	cfg.Logging.AppLevel = "warn"
	
	effectiveLevel = cfg.Logging.GetEffectiveAppLevel()
	if effectiveLevel != "warn" {
		t.Errorf("expected AppLevel to take precedence, got %q", effectiveLevel)
	}
	
	t.Log("Backward compatibility works correctly")
}