//go:build !windows

package host

import (
	"errors"
	"os"
	"testing"
)

// pipeEndClosed reports whether f has been closed. Unix answers a closed
// descriptor with os.ErrClosed.
func pipeEndClosed(t *testing.T, f *os.File) bool {
	t.Helper()
	_, err := f.Stat()
	return errors.Is(err, os.ErrClosed)
}
