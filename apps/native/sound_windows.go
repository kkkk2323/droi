package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var playSoundW = windows.NewLazySystemDLL("winmm.dll").NewProc("PlaySoundW")

const (
	sndNoDefault = 0x0002
	sndFilename  = 0x00020000
)

// playFile plays a .wav file to its end.
func playFile(path string) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	_, _, _ = playSoundW.Call(uintptr(unsafe.Pointer(p)), 0, sndFilename|sndNoDefault)
}
