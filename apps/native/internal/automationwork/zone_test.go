package automationwork

import (
	"strings"
	"testing"
)

func TestLocalZoneNamesTheZone(t *testing.T) {
	t.Setenv("TZ", "America/New_York")
	if got := LocalZone(); got != "America/New_York" {
		t.Fatalf("TZ: %q", got)
	}
	t.Setenv("TZ", "")
	if got := LocalZone(); got == "" || got == "Local" || strings.HasPrefix(got, "/") {
		t.Fatalf("the system's zone: %q", got)
	}
}
