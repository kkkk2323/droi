//go:build !darwin

package main

// nativePaste does nothing: the app ships for macOS only.
func nativePaste() {}
