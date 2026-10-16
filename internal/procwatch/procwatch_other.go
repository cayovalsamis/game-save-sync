//go:build !windows

package procwatch

import "errors"

// Running is only implemented on Windows; the Steam Deck uses a launch wrapper instead.
func Running(string) (map[string]bool, error) {
	return nil, errors.New("process watching is only supported on Windows")
}
