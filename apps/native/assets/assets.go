// Package assets embeds the files the native app ships inside its binary.
package assets

import _ "embed"

// The web Client's vendored variable fonts (apps/desktop/src/renderer/src/
// assets/fonts), decompressed from WOFF2 to TrueType: DirectWrite on
// Windows registers no WOFF2.
var (
	//go:embed fonts/Geist-Variable.ttf
	GeistVariable []byte
	//go:embed fonts/GeistMono-Variable.ttf
	GeistMonoVariable []byte
)
