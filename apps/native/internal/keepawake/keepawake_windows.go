package keepawake

import (
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// setState is SetThreadExecutionState; a test replaces it.
var setState = func(flags uint32) {
	_, _, _ = setThreadExecutionState.Call(uintptr(flags))
}

// start asks Windows not to sleep when idle. The request belongs to the
// thread that makes it, so one goroutine locked to its thread makes it and
// clears it.
func start() (stop func()) {
	done, cleared := make(chan struct{}), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		setState(esContinuous | esSystemRequired)
		<-done
		setState(esContinuous)
		close(cleared)
	}()
	return func() {
		close(done)
		<-cleared
	}
}
