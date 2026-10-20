// Package pasted is long text pasted in a composer, which waits as a card
// beside the images instead of in the text area. The text area lays a
// paragraph out again on each edit, in time that grows with the square of
// its length (100 KB on one line take a second per key), so such a paste
// would hang the window.
//
// A message carries its pasted texts after its own text, each in a block
// whose first line counts the lines it holds:
//
//	<pasted_text lines="2">
//	first line
//	second line
//	</pasted_text>
//
// The count tells where a block ends even when the text holds the closing
// tag itself, and Split reads the blocks back out for the transcript.
package pasted

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// A paste of MinLines lines or MinChars characters becomes a card.
const (
	MinLines = 25
	MinChars = 4000
)

const closing = "</pasted_text>"

// Text is a pasted text waiting in a composer.
type Text struct {
	ID   string
	Text string
}

// New is a Text of s, its line ends made "\n".
func New(s string) Text { return Text{ID: uuid.NewString(), Text: Normalize(s)} }

// Normalize makes the line ends of s "\n"; it keeps the spaces around it,
// which can matter in what was pasted.
func Normalize(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// Long tells whether s is long enough to paste as a card.
func Long(s string) bool {
	s = Normalize(s)
	return Lines(s) >= MinLines || utf8.RuneCountInString(s) >= MinChars
}

// Lines is how many lines s has: 0 for "", and a line after a last "\n".
func Lines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// MaxEditableLine is the longest line a pasted text can have to go back
// into the text area: 10,000 characters on one line take it about 10 ms a
// key, 26,000 already 60 ms.
const MaxEditableLine = 10_000

// Editable tells whether s can go into the text area without slowing it.
func Editable(s string) bool {
	for line := range strings.SplitSeq(s, "\n") {
		if len(line) > MaxEditableLine && utf8.RuneCountInString(line) > MaxEditableLine {
			return false
		}
	}
	return true
}

// Title is the first line of s with text, cut to a card's width.
func Title(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if utf8.RuneCountInString(line) > 120 {
				line = string([]rune(line)[:117]) + "..."
			}
			return line
		}
	}
	return ""
}

// Join is message with texts after it, as the Daemon gets them.
func Join(message string, texts []Text) string {
	var b strings.Builder
	b.WriteString(message)
	wrote := false
	for _, t := range texts {
		if t.Text == "" {
			continue
		}
		switch {
		case wrote:
			b.WriteString("\n")
		case message != "":
			// A blank line sets the blocks apart from the message.
			b.WriteString("\n\n")
		}
		b.WriteString(`<pasted_text lines="` + strconv.Itoa(Lines(t.Text)) + `">` + "\n")
		b.WriteString(t.Text)
		b.WriteString("\n" + closing)
		wrote = true
	}
	return b.String()
}

// Split is Join undone: the message and the texts pasted after it.
func Split(message string) (string, []string) {
	if !strings.HasSuffix(message, closing) {
		return message, nil
	}
	lines := strings.Split(message, "\n")
	for i := range lines {
		texts, ok := blocks(lines[i:])
		if !ok {
			continue
		}
		// The lines before hold the message and the blank line after it.
		return strings.TrimSuffix(strings.Join(lines[:i], "\n"), "\n"), texts
	}
	return message, nil
}

// blocks reads lines as nothing but blocks, up to their end.
func blocks(lines []string) ([]string, bool) {
	var texts []string
	for len(lines) > 0 {
		n, ok := header(lines[0])
		if !ok || n < 1 || len(lines) < n+2 || lines[n+1] != closing {
			return nil, false
		}
		texts = append(texts, strings.Join(lines[1:n+1], "\n"))
		lines = lines[n+2:]
	}
	return texts, texts != nil
}

func header(line string) (int, bool) {
	rest, ok := strings.CutPrefix(line, `<pasted_text lines="`)
	if !ok {
		return 0, false
	}
	digits, ok := strings.CutSuffix(rest, `">`)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil
}
