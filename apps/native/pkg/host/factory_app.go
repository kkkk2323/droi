package host

import (
	"os/exec"
	"runtime"
)

// factoryAppDaemon is how the Factory App starts its own Daemon, from the
// droid inside its bundle.
const factoryAppDaemon = "Factory.app/Contents/Resources/bin/droid daemon"

// FactoryAppRunning reports whether the Factory App's Daemon runs on this
// computer. It polls the same ~/.factory/automations as Droi's, and two
// Daemons do not coordinate, so a run can now and then start in both.
func FactoryAppRunning() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return exec.Command("pgrep", "-f", factoryAppDaemon).Run() == nil
}
