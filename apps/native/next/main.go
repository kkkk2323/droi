// Command next is Droi Next: this branch's Droi, installed beside a released Droi for everyday use while 2.0 is
// made. Its name, its folders (~/.droi-next, ~/Library/Application Support/Droi Next), its Gateway port and its
// bundle identifier are its own, so it never touches the released app's, and it has no update feed.
// scripts/install-next.sh writes its mygo.json from apps/native's, builds it and installs it.
package main

import (
	"os"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/droiapp"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
)

// devVersion is the version of `go run`; a build takes the mygo.json install-next.sh writes.
const devVersion = "0.0.0"

func main() {
	// Droi Next is Droi on its Kernel, which a released Droi runs only when asked to (host.KernelEnv).
	if os.Getenv(host.KernelEnv) == "" {
		_ = os.Setenv(host.KernelEnv, "1")
	}
	droiapp.Run(droiapp.Options{
		DevVersion: devVersion,
		Brand:      brand.Brand{Name: "Droi Next", ID: "droi-next", Port: 41418},
	})
}
