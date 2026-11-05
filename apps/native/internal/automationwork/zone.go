package automationwork

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// LocalZone names this computer's time zone for Droid: its IANA name where
// the system says it (TZ, or /etc/localtime's link), else its offset from
// UTC now.
func LocalZone() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil && tz != "Local" {
			return tz
		}
	}
	if runtime.GOOS != "windows" {
		if link, err := os.Readlink("/etc/localtime"); err == nil {
			if _, name, ok := strings.Cut(link, "zoneinfo/"); ok && name != "" {
				return name
			}
		}
	}
	_, offset := time.Now().Zone()
	sign := '+'
	if offset < 0 {
		sign, offset = '-', -offset
	}
	return fmt.Sprintf("UTC%c%02d:%02d", sign, offset/3600, offset%3600/60)
}
