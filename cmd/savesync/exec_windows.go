package main

import "errors"

// On Windows games are detected by process instead; see `savesync run`.
func execGame([]string) error {
	return errors.New("wrap is only for Steam launch options on the Steam Deck")
}
