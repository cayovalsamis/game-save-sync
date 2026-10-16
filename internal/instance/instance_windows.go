// Package instance keeps a second copy of the daemon from starting.
package instance

import (
	"errors"

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
