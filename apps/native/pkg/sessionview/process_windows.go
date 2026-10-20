package sessionview

import (
	"errors"

	"golang.org/x/sys/windows"
)

// processAlive is whether process pid still runs on this computer.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}
