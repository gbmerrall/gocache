package logging

import (
	"os"
	"testing"
)

func TestDetectProcessMode(t *testing.T) {
	// Test that the function doesn't panic and returns a valid mode
	mode := DetectProcessMode()
	if mode != ProcessModeForeground && mode != ProcessModeDaemon {
		t.Errorf("DetectProcessMode() returned invalid mode: %v", mode)
	}
}

func TestIsForegroundMode(t *testing.T) {
	// Test convenience function
	isForeground := IsForegroundMode()
	expectedMode := DetectProcessMode()
	
	if isForeground && expectedMode != ProcessModeForeground {
		t.Error("IsForegroundMode() returned true but DetectProcessMode() returned daemon")
	}
	if !isForeground && expectedMode != ProcessModeDaemon {
		t.Error("IsForegroundMode() returned false but DetectProcessMode() returned foreground")
	}
}

func TestProcessModeString(t *testing.T) {
	tests := []struct {
		mode     ProcessMode
		expected string
	}{
		{ProcessModeForeground, "foreground"},
		{ProcessModeDaemon, "daemon"},
		{ProcessMode(999), "unknown"},
	}

	for _, test := range tests {
		if got := test.mode.String(); got != test.expected {
			t.Errorf("ProcessMode(%d).String() = %q, want %q", test.mode, got, test.expected)
		}
	}
}

func TestProcessDetectionConsistency(t *testing.T) {
	// Multiple calls should return the same result
	mode1 := DetectProcessMode()
	mode2 := DetectProcessMode()
	
	if mode1 != mode2 {
		t.Error("DetectProcessMode() returned different results on consecutive calls")
	}
}

// TestProcessDetectionIndicators tests that our detection aligns with common expectations
func TestProcessDetectionIndicators(t *testing.T) {
	// When running tests, we expect to be in foreground mode typically
	// This is a soft assertion since test environments can vary
	mode := DetectProcessMode()
	
	// Check if we can access /dev/tty
	_, err := os.Open("/dev/tty")
	hasTty := err == nil
	
	if hasTty && mode == ProcessModeDaemon {
		t.Logf("Warning: have controlling terminal but detected daemon mode - this may be test environment specific")
	}
	
	t.Logf("Process mode detected: %s, has TTY: %v", mode.String(), hasTty)
}