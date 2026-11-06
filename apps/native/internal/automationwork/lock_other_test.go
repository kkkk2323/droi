//go:build !windows

package automationwork

import (
	"errors"
	"testing"
)

func TestOneSchedulerAtATimeHoldsAFolder(t *testing.T) {
	dir := t.TempDir()
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("a second lock: %v", err)
	}
	release()
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("after the release: %v", err)
	}
	again()
}
