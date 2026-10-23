package diag

import (
	"os"
	"syscall"
)

// On Windows, FileInfo.Sys carries no file index, so the identity comes from
// the open handle: volume serial number plus the 64-bit file index.
func fileID(f *os.File, _ os.FileInfo) (device, inode uint64, err error) {
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d); err != nil {
		return 0, 0, err
	}
	return uint64(d.VolumeSerialNumber), uint64(d.FileIndexHigh)<<32 | uint64(d.FileIndexLow), nil
}
