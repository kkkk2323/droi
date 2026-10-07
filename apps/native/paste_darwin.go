package main

import "github.com/ebitengine/purego/objc"

// nativePaste is what Paste of MyGo's Edit menu does: paste: to the first
// responder, which hands it to the focused text area. Main thread only.
func nativePaste() {
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	app.Send(objc.RegisterName("sendAction:to:from:"), objc.RegisterName("paste:"), objc.ID(0), objc.ID(0))
}
