//go:build !windows

// Package instance keeps a second copy of the daemon from starting.
package instance

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var ErrRunning = errors.New("another instance is already running")

// Lock holds an exclusive lock on dataDir/daemon.lock for the life of the process.
func Lock(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return ErrRunning
	}
	return nil
}
