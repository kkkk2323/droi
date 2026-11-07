//go:build windows

package host

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// pipeEndClosed reports whether f has been closed. Windows answers a closed
// handle with ERROR_INVALID_HANDLE, which is os.ErrClosed's meaning there but
// not its value.
func pipeEndClosed(t *testing.T, f *os.File) bool {
	t.Helper()
	_, err := f.Stat()
	return errors.Is(err, os.ErrClosed) || errors.Is(err, windows.ERROR_INVALID_HANDLE)
}
