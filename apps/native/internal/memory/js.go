package memory

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// JavaScript's whitespace, as its \s and String.prototype.trim know it. RE2's
// \s is ASCII only and unicode.IsSpace differs at U+0085 and U+FEFF, and a
// Chinese prompt often carries U+3000.
const jsSpaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

var jsClasses = strings.NewReplacer(
	`[\s`, "["+jsSpaceClass,
	`\s`, "["+jsSpaceClass+"]",
	`\S`, "[^"+jsSpaceClass+"]",
)

// jsRegexp compiles a pattern written with JavaScript's \s and \S.
func jsRegexp(pattern string) *regexp.Regexp {
	return regexp.MustCompile(jsClasses.Replace(pattern))
}

// jsLength is a string's length in UTF-16 code units, as JavaScript counts it.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// isoNow is Date.prototype.toISOString of now.
func isoNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// today is the UTC day, as the TS store records it.
func today() string { return time.Now().UTC().Format("2006-01-02") }

// jsonText is JSON.stringify, which leaves <, > and & alone.
func jsonText(v any) (string, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
