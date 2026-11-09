// Command local is Droi built from this checkout and installed as Droi itself, in place of a released Droi: the
// same name, bundle identifier, folders and Gateway port, so it keeps that Droi's settings, Memory, Automations,
// Key Pool and paired phones. It runs on the Kernel and has no update feed: a new build is installed by hand.
// `scripts/install-next.sh droi` writes its mygo.json from apps/native's, backs up the released Droi, builds it and
// installs it.
package main

import (
	"os"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/droiapp"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
)

// devVersion is the version of `go run`; a build takes the mygo.json install-next.sh writes.
const devVersion = "0.0.0"

func main() {
	if os.Getenv(host.KernelEnv) == "" {
		_ = os.Setenv(host.KernelEnv, "1")
	}
	droiapp.Run(droiapp.Options{DevVersion: devVersion})
}
