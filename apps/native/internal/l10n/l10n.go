// Package l10n is the native app's words in the user's language. The key
// is the English text, so a string without a translation reads as written;
// the table is zh.go, which TestCatalog checks against every L("…") in the
// app's sources.
//
// The language can change while the app runs: L reads the table in force,
// and the views, built for every frame, read it again. So call L as a view
// is built, and never keep its words in a package-level variable.
package l10n

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Language is a language the app speaks.
type Language string

const (
	English Language = "en"
	Chinese Language = "zh-Hans"
)

type state struct {
	chosen Language // "" follows the system
	system string   // the system's locale, such as "zh-CN"
}

var current atomic.Pointer[state]

func init() { current.Store(&state{system: "en-US"}) }

// Set applies the language picked in Settings ("en", "zh-Hans", or "" to
// follow the system) and the system's locale, kept when ""; it reports
// whether the words changed.
func Set(pick, system string) bool {
	before := Code()
	next := &state{system: current.Load().system}
	if system != "" {
		next.system = system
	}
	switch Language(pick) {
	case English, Chinese:
		next.chosen = Language(pick)
	}
	current.Store(next)
	return before != Code()
}

// Code is the language in force: the one picked, else the system's.
func Code() Language {
	s := current.Load()
	if s.chosen != "" {
		return s.chosen
	}
	if strings.HasPrefix(strings.ToLower(s.system), "zh") {
		return Chinese
	}
	return English
}

// L is a text in the language in force: L("New session"), or with values,
// as fmt.Sprintf fills them: L("Droi %s is available", version).
func L(key string, args ...any) string {
	text := T(key)
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// N marks a key kept in a table, such as an option's label, for the
// catalog; T translates it where the view is built.
func N(key string) string { return key }

// T is the key's text in the language in force, for a key marked with N.
func T(key string) string {
	if Code() == Chinese {
		if t, ok := zh[key]; ok {
			return t
		}
	}
	return key
}
