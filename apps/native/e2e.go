//go:build e2e

package main

// The window test (window_test.go) drives this build through MyGo's
// automation socket, which MYGO_AUTOMATION names; release builds leave the
// plugin out.
import _ "github.com/egoist/mygo/plugins/automation"
