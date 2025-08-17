package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const pidFileName = "gocache.pid"

var pidFilePath string // Unexported, for testing override

// SetPIDFilePath sets the path to the PID file for testing.
func SetPIDFilePath(path string) {
	pidFilePath = path
}

// getPIDFilePath returns the path to the PID file.
func getPIDFilePath() (string, error) {
	if pidFilePath != "" {
		return pidFilePath, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	gocacheDir := filepath.Join(configDir, "gocache")
	if err := os.MkdirAll(gocacheDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(gocacheDir, pidFileName), nil
}

// Write writes the current process ID to the PID file.
func Write() error {
	pidPath, err := getPIDFilePath()
	if err != nil {
		return fmt.Errorf("could not get pidfile path: %w", err)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		// Reading the existing PID could be useful here.
		// For now, just error out.
		return fmt.Errorf("pidfile already exists: %s", pidPath)
	}

	pid := os.Getpid()
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
}

// Read reads the process ID from the PID file.
func Read() (int, error) {
	pidPath, err := getPIDFilePath()
	if err != nil {
		return 0, err
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(string(data))
}

// Remove deletes the PID file.
func Remove() error {
	pidPath, err := getPIDFilePath()
	if err != nil {
		return err
	}
	return os.Remove(pidPath)
}