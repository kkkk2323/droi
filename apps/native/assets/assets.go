// Package assets embeds the files the native app ships inside its binary.
package assets

import _ "embed"

// The web Client's vendored variable fonts (apps/desktop/src/renderer/src/
// assets/fonts), copied as they are.
var (
	//go:embed fonts/Geist-Variable.woff2
	GeistVariable []byte
	//go:embed fonts/GeistMono-Variable.woff2
	GeistMonoVariable []byte
)
