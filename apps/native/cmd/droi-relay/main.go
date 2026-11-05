// Command droi-relay is Droi's Relay (ADR 0034): a server that Droi on a
// computer connects out to, so that a paired phone away from the computer's
// network reaches it. Run `droi-relay help`.
package main

import (
	"os"

	"github.com/kkkk2323/droi/apps/native/internal/relay"
)

// version is set by the release build: -ldflags "-X main.version=1.91.0".
var version = "dev"

func main() {
	os.Exit(relay.Main(os.Args[1:], version))
}
