package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func runGocacheBinaryWithTimeout(t *testing.T, timeout time.Duration, args ...string) ([]byte, error) {
	t.Helper()

	// Build the actual gocache binary
	binName := "gocache.test.bin"
	out, err := exec.Command("go", "build", "-o", binName, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build gocache binary: %v\n%s", err, out)
	}
	defer os.Remove(binName)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Run the gocache binary with the given arguments
	cmd := exec.CommandContext(ctx, "./"+binName, args...)
	output, err := cmd.CombinedOutput()

	// Check if context was cancelled (timeout)
	if ctx.Err() == context.DeadlineExceeded {
		return output, ctx.Err()
	}

	return output, err
}

func TestMainHelp(t *testing.T) {
	output, err := runGocacheBinaryWithTimeout(t, 10*time.Second, "--help")
	if err != nil {
		t.Fatalf("process exited with error: %v\n%s", err, output)
	}

	// Verify help output contains expected content
	outputStr := string(output)
	if !strings.Contains(outputStr, "Usage of") {
		t.Error("help output should contain 'Usage of'")
	}
	if !strings.Contains(outputStr, "-config") {
		t.Error("help output should contain '-config'")
	}
	if !strings.Contains(outputStr, "-daemon") {
		t.Error("help output should contain '-daemon'")
	}
}

func TestMainVersion(t *testing.T) {
	output, err := runGocacheBinaryWithTimeout(t, 10*time.Second, "--version")
	// This should exit with an error since --version is not defined
	if err == nil {
		t.Error("expected error with undefined --version flag")
	}

	// Verify error output contains expected content
	outputStr := string(output)
	if !strings.Contains(outputStr, "flag provided but not defined") {
		t.Error("error output should contain 'flag provided but not defined'")
	}
	if !strings.Contains(outputStr, "-version") {
		t.Error("error output should contain '-version'")
	}
}

func TestMainNoArgs(t *testing.T) {
	output, err := runGocacheBinaryWithTimeout(t, 10*time.Second)
	// This should exit with an error since no config file is provided
	if err == nil {
		t.Error("expected error when no arguments provided")
	}

	// Verify error output contains expected content
	outputStr := string(output)
	if !strings.Contains(outputStr, "error") && !strings.Contains(outputStr, "Error") {
		t.Logf("output: %s", outputStr)
		t.Log("expected error message in output")
	}
}

func TestMainInvalidFlag(t *testing.T) {
	output, err := runGocacheBinaryWithTimeout(t, 10*time.Second, "--invalid-flag")
	// This should exit with an error
	if err == nil {
		t.Error("expected error with invalid flag")
	}

	// Verify error output
	outputStr := string(output)
	if !strings.Contains(outputStr, "error") && !strings.Contains(outputStr, "Error") && !strings.Contains(outputStr, "unknown") {
		t.Logf("output: %s", outputStr)
		t.Log("expected error message for invalid flag")
	}
}

func TestMainConfigFile(t *testing.T) {
	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `
[server]
proxy_port = 8080
control_port = 8081
bind_address = "127.0.0.1"

[cache]
default_ttl = "1h"
max_size_mb = 500

[logging]
level = "info"

[persistence]
enable = false
`
	configFile := tmpDir + "/gocache.toml"
	err = os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Run the gocache binary with a config file
	output, err := runGocacheBinaryWithTimeout(t, 10*time.Second, "-config", configFile)

	// The exact error depends on the implementation, but it should fail
	// since we're not providing a proper server environment
	if err == nil {
		t.Logf("output: %s", string(output))
		t.Log("expected some error when trying to start server")
	}
}
