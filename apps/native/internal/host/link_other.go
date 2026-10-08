//go:build !windows

package host

import "os"

func link(target, at string, _ bool) error { return os.Symlink(target, at) }
