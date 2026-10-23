//go:build !windows

package diag

import (
	"os"
	"syscall"
)

func fileID(_ *os.File, info os.FileInfo) (device, inode uint64, err error) {
	stat := info.Sys().(*syscall.Stat_t)
	return uint64(stat.Dev), stat.Ino, nil
}
