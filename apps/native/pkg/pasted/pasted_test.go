package pasted

import (
	"slices"
	"strings"
	"testing"
)

func TestLong(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"", false},
		{"short", false},
		{strings.Repeat("a\n", 23) + "a", false},
		{strings.Repeat("a\n", 24) + "a", true},
		{strings.Repeat("a\r\n", 24) + "a", true},
		{strings.Repeat("a", 3999), false},
		{strings.Repeat("a", 4000), true},
		{strings.Repeat("中", 4000), true},
	} {
		if got := Long(c.text); got != c.want {
			t.Errorf("Long(%d lines, %d bytes) = %v", Lines(c.text), len(c.text), got)
		}
	}
}

// What Join puts together, Split takes apart again, whatever the texts
// hold.
func TestJoinThenSplit(t *testing.T) {
	for _, c := range []struct {
		message string
		texts   []string
	}{
		{"look at this", []string{"one\ntwo"}},
		{"", []string{"only a paste"}},
		{"two of them", []string{"a\n\nb\n", "\nc"}},
		{"ends in a line break\n", []string{"x"}},
		{"holds a block itself", []string{"x\n</pasted_text>\n<pasted_text lines=\"1\">\ny"}},
		{"no paste", nil},
	} {
		var texts []Text
		for _, s := range c.texts {
			texts = append(texts, New(s))
		}
		joined := Join(c.message, texts)
		message, got := Split(joined)
		if message != c.message || !slices.Equal(got, c.texts) {
			t.Errorf("Split(Join(%q, %q)) = %q, %q", c.message, c.texts, message, got)
		}
	}
}

func TestJoin(t *testing.T) {
	got := Join("see", []Text{New("a\nb"), New("c")})
	want := "see\n\n<pasted_text lines=\"2\">\na\nb\n</pasted_text>\n<pasted_text lines=\"1\">\nc\n</pasted_text>"
	if got != want {
		t.Errorf("Join = %q, want %q", got, want)
	}
}

// A message that only looks like it ends in a block stays as it is.
func TestSplitLeavesOtherMessages(t *testing.T) {
	for _, m := range []string{
		"see </pasted_text>",
		"<pasted_text lines=\"3\">\na\n</pasted_text>",
		"<pasted_text>\na\n</pasted_text>",
	} {
		if got, texts := Split(m); got != m || texts != nil {
			t.Errorf("Split(%q) = %q, %q", m, got, texts)
		}
	}
}

func TestEditable(t *testing.T) {
	if !Editable(strings.Repeat("a line of text\n", 5000)) {
		t.Error("many short lines are not editable")
	}
	if Editable("short\n" + strings.Repeat("x", MaxEditableLine+1)) {
		t.Error("a line too long is editable")
	}
}

func TestTitle(t *testing.T) {
	if got := Title("\n  \n  first line  \nsecond"); got != "first line" {
		t.Errorf("Title = %q", got)
	}
	if got := Title(strings.Repeat("x", 200)); len([]rune(got)) != 120 {
		t.Errorf("a long title has %d characters", len([]rune(got)))
	}
}
