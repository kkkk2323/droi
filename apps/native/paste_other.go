//go:build !darwin

package main

// nativePaste does nothing: off macOS the focused text area takes Ctrl+V itself.
func nativePaste() {}
