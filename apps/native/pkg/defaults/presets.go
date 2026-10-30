package defaults

// Presets are what an app built on Droi sets up on a new install (ADR
// 0029): Session Defaults for the Daemon to keep, and the tool mode new
// Sessions on this device start with. They are applied once per Version,
// so a change the user makes afterwards stays, and a later Version is
// applied again.
type Presets struct {
	// Version counts the app's presets: raise it when they change.
	Version int
	Session Patch
	// ToolMode, "" for none, becomes this device's default (DefaultToolMode).
	ToolMode ToolMode
}
