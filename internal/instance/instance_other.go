//go:build !windows

// Package instance keeps a second copy of the daemon from starting.
package instance

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
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

// SyncLock serialises syncs between the daemon and launch wrappers. It
// waits up to wait for the lock and returns a function that releases it.
func SyncLock(dataDir string, wait time.Duration) (func(), error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "sync.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, errors.New("timed out waiting for another sync to finish")
		}
		time.Sleep(500 * time.Millisecond)
	}
}
