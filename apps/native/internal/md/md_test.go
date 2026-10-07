package md

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const sample = "# Title\n\nSome **bold** and `code` and [a link](https://x.dev).\n\n- one\n- two\n  - nested\n\n1. first\n2. second\n\n```go\nfunc main() {\n\n\tprintln(1)\n}\n```\n\n> quoted\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n---\n\nLast paragraph.\n"

func TestParse(t *testing.T) {
	b := Parse(sample)
	kinds := []Kind{}
	for _, x := range b {
		kinds = append(kinds, x.Kind)
	}
	want := []Kind{Heading, Paragraph, Paragraph, Paragraph, Paragraph, Paragraph, Paragraph, CodeBlock, Paragraph, Table, Rule, Paragraph}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds %v, want %v", kinds, want)
	}
	if b[1].Inlines[1] != (Inline{Text: "bold", Bold: true}) {
		t.Errorf("bold run %+v", b[1].Inlines)
	}
	if b[4].Indent != 2 || b[4].Marker != "◦" {
		t.Errorf("nested item %+v", b[4])
	}
	if b[6].Marker != "2." {
		t.Errorf("ordered marker %q", b[6].Marker)
	}
	if b[7].Lang != "go" || !strings.Contains(b[7].Code, "\n\n\tprintln") {
		t.Errorf("code %+v", b[7])
	}
	if b[8].Quote != 1 {
		t.Errorf("quote %+v", b[8])
	}
	if len(b[9].Header) != 2 || len(b[9].Rows) != 1 {
		t.Errorf("table %+v", b[9])
	}
}

func TestRemend(t *testing.T) {
	cases := map[string]string{
		"Hello **wor":          "Hello **wor**",
		"Use `fmt.Pri":         "Use `fmt.Pri`",
		"a ~~gone":             "a ~~gone~~",
		"```go\nfunc":          "```go\nfunc\n```",
		"```go\nx\n```\n\nok":  "```go\nx\n```\n\nok",
		"**done** and `x`":     "**done** and `x`",
		"para **one**\n\n**tw": "para **one**\n\n**tw**",
	}
	for in, want := range cases {
		if got := Remend(in); got != want {
			t.Errorf("Remend(%q) = %q, want %q", in, got, want)
		}
	}
}

// Streaming a document in any chunks ends where parsing it at once does,
// and every step parses.
func TestStreamMatchesParse(t *testing.T) {
	want := Parse(sample)
	for _, size := range []int{1, 3, 7, 50} {
		s := NewStream("")
		for i := 0; i < len(sample); {
			j := min(i+size, len(sample))
			for j < len(sample) && !utf8.RuneStart(sample[j]) {
				j++
			}
			s.Append(sample[i:j])
			i = j
		}
		if got := s.Blocks(); !reflect.DeepEqual(got, want) {
			t.Errorf("chunks of %d: blocks differ before Finish\n got %+v\nwant %+v", size, got, want)
		}
		s.Finish()
		if got := s.Blocks(); !reflect.DeepEqual(got, want) {
			t.Errorf("chunks of %d: blocks differ after Finish", size)
		}
	}
}

// A code block shows as code while it streams, before its fence closes.
func TestStreamOpenFence(t *testing.T) {
	s := NewStream("Intro\n\n```ts\nconst a = 1\n\nconst b")
	b := s.Blocks()
	if len(b) != 2 || b[1].Kind != CodeBlock || b[1].Code != "const a = 1\n\nconst b" {
		t.Fatalf("blocks %+v", b)
	}
}

func TestBoundaryKeepsFencesWhole(t *testing.T) {
	src := "a\n\n```\nx\n\ny\n```\n\nb\n"
	if got := boundary(src, 0); src[got:] != "b\n" {
		t.Errorf("boundary at %q", src[got:])
	}
}

func BenchmarkStreamAppend(b *testing.B) {
	doc := strings.Repeat(sample, 40) // ~16 KB
	b.ReportAllocs()
	for b.Loop() {
		s := NewStream("")
		for i := 0; i < len(doc); i += 8 {
			s.Append(doc[i:min(i+8, len(doc))])
		}
	}
}

func BenchmarkParseWhole(b *testing.B) {
	doc := strings.Repeat(sample, 40)
	for b.Loop() {
		Parse(doc)
	}
}
