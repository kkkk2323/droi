// Command native is Droi as a native app (package droiapp).
package main

import "github.com/kkkk2323/droi/apps/native/pkg/droiapp"

// devVersion is the version of `go run`; a build takes mygo.json's.
const devVersion = "1.87.1"

func main() {
	droiapp.Run(droiapp.Options{DevVersion: devVersion})
}
