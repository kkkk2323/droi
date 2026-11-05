//go:build !windows

package automations

import "os"

func rename(from, to string) error { return os.Rename(from, to) }
