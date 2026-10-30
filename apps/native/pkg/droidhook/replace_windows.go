package droidhook

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// replace renames from over to. Windows refuses to replace a file another
// process has open, as droid has the key file for each request, for a few
// milliseconds; Go's own robustio retries the same way.
func replace(from, to string) error {
	deadline := time.Now().Add(2 * time.Second)
	for wait := time.Millisecond; ; wait = min(2*wait, 50*time.Millisecond) {
		err := os.Rename(from, to)
		if err == nil || !transient(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(wait)
	}
}

func transient(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
