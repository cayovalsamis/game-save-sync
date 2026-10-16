// Package execx runs helper programs without flashing a console window on
// Windows, where the daemon itself has no console.
package execx

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Output runs name with args and returns stdout. On failure the error
// includes stderr so logs say why.
func Output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	hide(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return stdout.Bytes(), &Error{Cmd: name + " " + strings.Join(args, " "), Err: err, Msg: msg}
	}
	return stdout.Bytes(), nil
}

type Error struct {
	Cmd string
	Err error
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v: %s", e.Cmd, e.Err, e.Msg) }
func (e *Error) Unwrap() error { return e.Err }

// ExitCode returns the process exit code, or -1 if it did not run.
func (e *Error) ExitCode() int {
	if exit, ok := e.Err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return -1
}
