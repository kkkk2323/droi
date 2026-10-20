package mermaid

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// lineHeight is the height of a line of label text, in units of its size.
const lineHeight = 1.5

// advance is how far a one-cell glyph moves the pen, in units of the size.
const advance = 0.6

func runeCells(r rune) int {
	switch {
	case r >= 0x300 && r <= 0x36f, r >= 0x200b && r <= 0x200f, r >= 0xfe00 && r <= 0xfe0f, r == 0xfeff,
		unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r > 0xffff:
		return 2
	}
	return 1
}

func cells(s string) int {
	n := 0
	for _, r := range s {
		n += runeCells(r)
	}
	return n
}

func textWidth(s string, size float64) float64 { return float64(cells(s)) * advance * size }

// label is text broken into the lines it is drawn in.
type label struct {
	lines    []string
	w, h     float64
	size, lh float64
}

var (
	reBreak = regexp.MustCompile(`(?i)<br\s*/?>|\\n|\n`)
	reTag   = regexp.MustCompile(`</?[a-zA-Z][\w-]*(?:\s[^<>]*)?/?>`)
	reIcon  = regexp.MustCompile(`fa[bklrs]?:fa-[\w-]+`)
)

// makeLabel lays out raw label text: entities decoded, `<br>` and newlines
// breaking lines, other tags dropped, and lines longer than maxW (when it is
// positive) wrapped at spaces and between CJK characters.
func makeLabel(raw string, size, maxW float64, markdown bool) label {
	l := label{size: size, lh: size * lineHeight}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return l
	}
	for _, part := range reBreak.Split(raw, -1) {
		part = reTag.ReplaceAllString(part, "")
		part = reIcon.ReplaceAllString(part, "")
		if markdown {
			part = stripMarkdown(part)
		}
		part = strings.Join(strings.Fields(decodeEntities(part)), " ")
		if maxW > 0 && textWidth(part, size) > maxW {
			l.lines = append(l.lines, wrap(part, size, maxW)...)
		} else {
			l.lines = append(l.lines, part)
		}
	}
	for len(l.lines) > 0 && l.lines[len(l.lines)-1] == "" {
		l.lines = l.lines[:len(l.lines)-1]
	}
	if len(l.lines) == 0 {
		return l
	}
	for _, line := range l.lines {
		l.w = max(l.w, textWidth(line, size))
	}
	l.h = float64(len(l.lines)) * l.lh
	return l
}

// stripMarkdown drops the emphasis markers of a Markdown string, keeping its text.
func stripMarkdown(s string) string {
	for _, m := range []string{"**", "__", "~~", "`"} {
		s = strings.ReplaceAll(s, m, "")
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '*' || c == '_' {
			prevSpace := i == 0 || s[i-1] == ' ' || s[i-1] == '('
			nextSpace := i+1 >= len(s) || strings.IndexByte(" ).,!?:;", s[i+1]) >= 0
			if prevSpace != nextSpace {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isCJK(r rune) bool {
	return r >= 0x2e80 && r <= 0x9fff || r >= 0xf900 && r <= 0xfaff || r >= 0xff00 && r <= 0xff60 || r >= 0xac00 && r <= 0xd7a3
}

// wrap breaks one line greedily into lines no wider than maxW, except a word
// that is wider on its own.
func wrap(s string, size, maxW float64) []string {
	var pieces []string
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		j := i + n
		switch {
		case r == ' ':
		case isCJK(r):
			// Closing punctuation stays with the character before it.
			for j < len(s) {
				r2, n2 := utf8.DecodeRuneInString(s[j:])
				if !strings.ContainsRune("、。，．：；！？）」』】》,.:;!?)", r2) {
					break
				}
				j += n2
			}
		default:
			for j < len(s) {
				r2, n2 := utf8.DecodeRuneInString(s[j:])
				if r2 == ' ' || isCJK(r2) {
					break
				}
				j += n2
			}
		}
		pieces = append(pieces, s[i:j])
		i = j
	}
	var lines []string
	cur := ""
	for _, p := range pieces {
		if cur != "" && p != " " && textWidth(strings.TrimRight(cur+p, " "), size) > maxW {
			lines = append(lines, strings.TrimRight(cur, " "))
			cur = ""
		}
		if cur == "" && p == " " {
			continue
		}
		cur += p
	}
	if cur = strings.TrimRight(cur, " "); cur != "" || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

var entities = map[string]string{
	"quot": `"`, "amp": "&", "lt": "<", "gt": ">", "nbsp": " ", "apos": "'", "infin": "∞",
	"hellip": "…", "mdash": "—", "ndash": "–", "rarr": "→", "larr": "←", "harr": "↔",
	"uarr": "↑", "darr": "↓", "times": "×", "copy": "©", "reg": "®", "deg": "°",
	"hearts": "♥", "check": "✓", "semi": ";", "num": "#", "colon": ":", "bull": "•",
}

// decodeEntities decodes Mermaid's `#name;` and `#123;` codes and the
// HTML `&name;` and `&#123;` ones.
func decodeEntities(s string) string {
	if !strings.ContainsAny(s, "#&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '#' && c != '&' {
			b.WriteByte(c)
			continue
		}
		end := strings.IndexByte(s[i+1:], ';')
		if end <= 0 || end > 10 {
			b.WriteByte(c)
			continue
		}
		name := s[i+1 : i+1+end]
		if c == '&' && strings.HasPrefix(name, "#") {
			name = name[1:]
		}
		if v, ok := entities[name]; ok {
			b.WriteString(v)
			i += end + 1
			continue
		}
		digits, base := name, 10
		if c == '&' && strings.HasPrefix(name, "x") {
			digits, base = name[1:], 16
		}
		if n, err := strconv.ParseUint(digits, base, 32); err == nil && n >= 32 && n <= unicode.MaxRune {
			b.WriteRune(rune(n))
			i += end + 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
