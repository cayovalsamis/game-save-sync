// Package instance keeps a second copy of the daemon from starting.
package instance

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

var ErrRunning = errors.New("another instance is already running")

// Lock holds a per-user named mutex for the life of the process.
func Lock(string) error {
	name, err := windows.UTF16PtrFromString(`Local\game-save-sync`)
	if err != nil {
		return err
	}
	_, err = windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return ErrRunning
	}
	return err
}

// SyncLock serialises syncs between the daemon and `savesync sync`. It
// waits up to wait for the lock and returns a function that releases it.
func SyncLock(dataDir string, wait time.Duration) (func(), error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "sync.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	deadline := time.Now().Add(wait)
	for {
		ol := new(windows.Overlapped)
		err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
		if err == nil {
			return func() { windows.UnlockFileEx(h, 0, 1, 0, ol); f.Close() }, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, errors.New("timed out waiting for another sync to finish")
		}
		time.Sleep(500 * time.Millisecond)
	}
}
