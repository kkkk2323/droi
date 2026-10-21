// Package assets embeds the files the native app ships inside its binary.
package assets

import _ "embed"

// The vendored variable fonts (apps/native/assets/fonts): JetBrains Mono,
// the code face, and the Geist pair the Font setting can still pick.
// Decompressed from WOFF2 to TrueType where they came that way: DirectWrite
// on Windows registers no WOFF2.
var (
	//go:embed fonts/JetBrainsMono-Variable.ttf
	JetBrainsMonoVariable []byte
	//go:embed fonts/Geist-Variable.ttf
	GeistVariable []byte
	//go:embed fonts/GeistMono-Variable.ttf
	GeistMonoVariable []byte
)
