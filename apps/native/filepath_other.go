//go:build !darwin

package main

// filePath is nil: file references are a macOS thing.
var filePath func(string) string
