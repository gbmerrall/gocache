package logging

import (
	"os"
	"syscall"
	"unsafe"
)

// ProcessMode represents whether the process is running in foreground or daemon mode
type ProcessMode int

const (
	ProcessModeForeground ProcessMode = iota
	ProcessModeDaemon
)

// DetectProcessMode determines if the process is running in foreground or daemon mode
func DetectProcessMode() ProcessMode {
	// Check multiple indicators to determine if we're in foreground mode:
	
	// 1. Check if stdin is a terminal
	if !isTerminal(syscall.Stdin) {
		return ProcessModeDaemon
	}
	
	// 2. Check if stdout is a terminal
	if !isTerminal(syscall.Stdout) {
		return ProcessModeDaemon
	}
	
	// 3. Check if stderr is a terminal
	if !isTerminal(syscall.Stderr) {
		return ProcessModeDaemon
	}
	
	// 4. Check if we have a controlling terminal
	if !hasControllingTerminal() {
		return ProcessModeDaemon
	}
	
	// 5. Check if process group ID equals process ID (session leader check)
	pid := os.Getpid()
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid != pid {
		// Not a session leader, likely a daemon
		return ProcessModeDaemon
	}
	
	return ProcessModeForeground
}

// isTerminal checks if the given file descriptor is a terminal
func isTerminal(fd int) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGETA, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}

// hasControllingTerminal checks if the process has a controlling terminal
func hasControllingTerminal() bool {
	// Try to open /dev/tty which represents the controlling terminal
	file, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	file.Close()
	return true
}

// IsForegroundMode is a convenience function that returns true if running in foreground
func IsForegroundMode() bool {
	return DetectProcessMode() == ProcessModeForeground
}

// String returns a string representation of the process mode
func (pm ProcessMode) String() string {
	switch pm {
	case ProcessModeForeground:
		return "foreground"
	case ProcessModeDaemon:
		return "daemon"
	default:
		return "unknown"
	}
}