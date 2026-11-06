package automations

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// rename retries a moment when Windows refuses to replace a file another
// process (a virus scanner, an indexer) holds open.
func rename(from, to string) error {
	err := os.Rename(from, to)
	for wait := 10 * time.Millisecond; err != nil && wait <= 160*time.Millisecond; wait *= 2 {
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		time.Sleep(wait)
		err = os.Rename(from, to)
	}
	return err
}
