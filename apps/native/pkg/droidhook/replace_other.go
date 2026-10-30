//go:build !windows

package droidhook

import "os"

func replace(from, to string) error { return os.Rename(from, to) }
