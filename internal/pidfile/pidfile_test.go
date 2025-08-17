package pidfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPIDFile(t *testing.T) {
	t.Run("Write and Read", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-pid")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Override the pidfile path for this test
		originalPIDFilePath := pidFilePath
		pidFilePath = filepath.Join(tmpDir, "gocache.pid")
		defer func() { pidFilePath = originalPIDFilePath }()

		err = Write()
		if err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		readPID, err := Read()
		if err != nil {
			t.Fatalf("failed to read PID file: %v", err)
		}
		if readPID != os.Getpid() {
			t.Errorf("got PID %d, want %d", readPID, os.Getpid())
		}
	})

	t.Run("Write already exists", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-pid")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Override the pidfile path for this test
		originalPIDFilePath := pidFilePath
		pidFilePath = filepath.Join(tmpDir, "gocache.pid")
		defer func() { pidFilePath = originalPIDFilePath }()

		err = Write()
		if err != nil {
			t.Fatalf("failed to write first PID file: %v", err)
		}

		// Try to write again - should fail
		err = Write()
		if err == nil {
			t.Error("expected error when writing to existing PID file")
		}
	})

	t.Run("Remove", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-pid")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Override the pidfile path for this test
		originalPIDFilePath := pidFilePath
		pidFilePath = filepath.Join(tmpDir, "gocache.pid")
		defer func() { pidFilePath = originalPIDFilePath }()

		err = Write()
		if err != nil {
			t.Fatalf("failed to write PID file: %v", err)
		}

		err = Remove()
		if err != nil {
			t.Fatalf("failed to remove PID file: %v", err)
		}

		// Verify file is removed
		_, err = Read()
		if err == nil {
			t.Error("expected error reading removed PID file")
		}
	})

	t.Run("Read non-existent", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "gocache-test-pid")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Override the pidfile path for this test
		originalPIDFilePath := pidFilePath
		pidFilePath = filepath.Join(tmpDir, "gocache.pid")
		defer func() { pidFilePath = originalPIDFilePath }()

		_, err = Read()
		if err == nil {
			t.Error("expected error reading non-existent PID file")
		}
	})
}

func TestSetPIDFilePath(t *testing.T) {
	// Test setting a custom PID file path
	customPath := "/tmp/custom-pid-file.pid"
	SetPIDFilePath(customPath)

	// Verify the path was set by checking if getPIDFilePath returns it
	path, err := getPIDFilePath()
	if err != nil {
		t.Fatalf("getPIDFilePath failed: %v", err)
	}
	if path != customPath {
		t.Errorf("expected path %s, got %s", customPath, path)
	}
}

func TestGetPIDFilePath(t *testing.T) {
	// Reset pidFilePath to test default behavior
	originalPIDFilePath := pidFilePath
	pidFilePath = ""
	defer func() { pidFilePath = originalPIDFilePath }()

	// Test that getPIDFilePath returns a valid path
	path, err := getPIDFilePath()
	if err != nil {
		t.Fatalf("getPIDFilePath failed: %v", err)
	}
	if path == "" {
		t.Error("getPIDFilePath returned empty path")
	}

	// Test that the path is absolute
	if !filepath.IsAbs(path) {
		t.Errorf("getPIDFilePath returned relative path: %s", path)
	}

	// Test that the directory exists or can be created
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// Try to create the directory
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			t.Errorf("failed to create directory %s: %v", dir, err)
		}
		defer os.RemoveAll(dir)
	}
}

func TestWriteWithInvalidDirectory(t *testing.T) {
	// Test with non-existent directory
	nonExistentDir := "/non/existent/directory"
	SetPIDFilePath(filepath.Join(nonExistentDir, "gocache.pid"))

	err := Write()
	if err == nil {
		t.Error("expected error writing to non-existent directory")
	}
}

func TestReadWithInvalidFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-pid")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override the pidfile path for this test
	originalPIDFilePath := pidFilePath
	pidFilePath = filepath.Join(tmpDir, "gocache.pid")
	defer func() { pidFilePath = originalPIDFilePath }()

	// Create a PID file with invalid content
	err = os.WriteFile(pidFilePath, []byte("invalid-pid"), 0644)
	if err != nil {
		t.Fatalf("failed to write invalid PID file: %v", err)
	}

	_, err = Read()
	if err == nil {
		t.Error("expected error reading invalid PID file")
	}
}

func TestReadWithEmptyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-pid")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override the pidfile path for this test
	originalPIDFilePath := pidFilePath
	pidFilePath = filepath.Join(tmpDir, "gocache.pid")
	defer func() { pidFilePath = originalPIDFilePath }()

	// Create an empty PID file
	err = os.WriteFile(pidFilePath, []byte(""), 0644)
	if err != nil {
		t.Fatalf("failed to write empty PID file: %v", err)
	}

	_, err = Read()
	if err == nil {
		t.Error("expected error reading empty PID file")
	}
}

func TestRemoveNonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gocache-test-pid")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override the pidfile path for this test
	originalPIDFilePath := pidFilePath
	pidFilePath = filepath.Join(tmpDir, "gocache.pid")
	defer func() { pidFilePath = originalPIDFilePath }()

	// Try to remove a non-existent PID file
	err = Remove()
	if err != nil {
		t.Logf("Remove non-existent PID file returned error: %v", err)
	}
}
