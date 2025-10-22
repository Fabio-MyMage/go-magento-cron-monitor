package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// PIDFile manages a PID file for preventing multiple instances
type PIDFile struct {
	path string
}

// New creates a new PID file at the specified path
func New(path string) *PIDFile {
	return &PIDFile{path: path}
}

// Create creates the PID file with fallback logic
func (p *PIDFile) Create() error {
	// Check if another instance is already running
	if err := p.checkExisting(); err != nil {
		return err
	}

	// Try to write PID file
	if err := p.write(); err != nil {
		// If write fails, try fallback location
		if p.path != GetDefaultPath("") {
			fallbackPath := filepath.Join("/tmp", filepath.Base(p.path))
			p.path = fallbackPath
			if err := p.write(); err != nil {
				return fmt.Errorf("failed to write PID file: %w", err)
			}
		} else {
			return fmt.Errorf("failed to write PID file: %w", err)
		}
	}

	return nil
}

// checkExisting verifies if another instance is already running
func (p *PIDFile) checkExisting() error {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No PID file exists, we're good
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	// Parse PID
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Invalid PID file, remove it
		os.Remove(p.path)
		return nil
	}

	// Check if process is still running
	if isProcessRunning(pid) {
		return fmt.Errorf("another instance is already running (PID: %d)", pid)
	}

	// Process not running, remove stale PID file
	os.Remove(p.path)
	return nil
}

// write writes the current process PID to the file
func (p *PIDFile) write() error {
	pid := os.Getpid()
	content := fmt.Sprintf("%d\n", pid)

	// Ensure directory exists
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	err := os.WriteFile(p.path, []byte(content), 0644)
	if err != nil {
		return err
	}

	return nil
}

// Remove removes the PID file
func (p *PIDFile) Remove() error {
	return os.Remove(p.path)
}

// GetDefaultPath determines the best PID file location
// Priority: 1) config directory, 2) /var/run, 3) /tmp
func GetDefaultPath(configPath string) string {
	pidFileName := "go-magento-cron-monitor.pid"

	// 1. Try config directory if absolute config path provided
	if configPath != "" && filepath.IsAbs(configPath) {
		configDir := filepath.Dir(configPath)
		pidPath := filepath.Join(configDir, pidFileName)
		if isWritable(configDir) {
			return pidPath
		}
	}

	// 2. Try /var/run
	runDir := "/var/run"
	if isWritable(runDir) {
		return filepath.Join(runDir, pidFileName)
	}

	// 3. Fallback to /tmp
	return filepath.Join("/tmp", pidFileName)
}

// isWritable tests if a directory is writable
func isWritable(path string) bool {
	// Try to create a temporary file
	testFile := filepath.Join(path, ".write_test")
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(testFile)
	return true
}

// isProcessRunning checks if a process with given PID exists
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
