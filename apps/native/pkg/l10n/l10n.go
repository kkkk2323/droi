// Package l10n is the native app's words in the user's language. The key
// is the English text, so a string without a translation reads as written;
// the table is zh.go, which TestCatalog checks against every L("…") in the
// app's sources. Another app built on these packages adds its own words
// with Add.
//
// The language can change while the app runs: L reads the table in force,
// and the views, built for every frame, read it again. So call L as a view
// is built, and never keep its words in a package-level variable.
//
// Droi's words name the app "Droi". An app built on these packages under
// another name sets it with SetAppName, and the word Droi in every text
// reads as that name.
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

// droi is the app's name as Droi's words write it.
const droi = "Droi"

var (
	current atomic.Pointer[state]
	added   atomic.Pointer[map[string]string]
	appName atomic.Pointer[string]
)

func init() {
	current.Store(&state{system: "en-US"})
	added.Store(&map[string]string{})
	name := droi
	appName.Store(&name)
}

// SetAppName names the app in every text: the word Droi, but not droid or
// droi-memory, reads as name. "" is Droi.
func SetAppName(name string) {
	if name == "" {
		name = droi
	}
	appName.Store(&name)
}

// AppName is the name SetAppName gave, Droi by default.
func AppName() string { return *appName.Load() }

// renamed is text with each word Droi replaced by name.
func renamed(text, name string) string {
	if name == droi || !strings.Contains(text, droi) {
		return text
	}
	var b strings.Builder
	for {
		i := strings.Index(text, droi)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		end := i + len(droi)
		if wordByte(text, i-1) || wordByte(text, end) {
			b.WriteString(text[:end])
		} else {
			b.WriteString(text[:i])
			b.WriteString(name)
		}
		text = text[end:]
	}
}

// wordByte says whether s[i] is a letter, a digit or an underscore; outside
// s it is not.
func wordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// Add adds Chinese words for keys of another app's own. A key this
// package already translates keeps its translation.
func Add(chinese map[string]string) {
	for {
		old := added.Load()
		next := make(map[string]string, len(*old)+len(chinese))
		for k, v := range *old {
			next[k] = v
		}
		for k, v := range chinese {
			next[k] = v
		}
		if added.CompareAndSwap(old, &next) {
			return
		}
	}
}

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
	return renamed(translated(key), AppName())
}

func translated(key string) string {
	if Code() == Chinese {
		if t, ok := zh[key]; ok {
			return t
		}
		if t, ok := (*added.Load())[key]; ok {
			return t
		}
	}
	return key
}
