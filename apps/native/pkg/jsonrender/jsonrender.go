// Package jsonrender is the rich output in an assistant's reply: a
// `<json-render>` tag holding one line of JSON that describes a small tree of
// display components (a table, a status line, a bar chart). The Droid CLI
// draws it in the terminal; the Clients split the reply around it and draw
// the tree with their own components. A tag inside a code fence is text about
// the format, not output (a port of json-render.ts). Props are decoded
// generic JSON, so a map's keys come in no particular order.
package jsonrender

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type Element struct {
	Type     string
	Props    map[string]any
	Children []string
}

type Spec struct {
	Root     string
	Elements map[string]Element
}

type SegmentKind uint8

const (
	Markdown SegmentKind = iota
	Render
	// Pending is a tag still open: the reply is streaming, or it ended
	// without closing it.
	Pending
)

// Segment is a piece of a reply: Text for Markdown, Spec for Render, Raw for Pending.
type Segment struct {
	Kind SegmentKind
	Text string
	Spec *Spec
	Raw  string
}

const (
	openTagText  = "<json-render>"
	closeTagText = "</json-render>"
)

var fence = regexp.MustCompile("^\\s{0,3}(```|~~~)")

func HasRenderTag(text string) bool { return strings.Contains(text, openTagText) }

// SplitReply splits a reply around its render tags.
func SplitReply(text string) []Segment {
	if !HasRenderTag(text) {
		return []Segment{{Kind: Markdown, Text: text}}
	}
	var segments []Segment
	var markdown []string
	capturing, capturingSet := "", false
	fenceMarker := ""

	flush := func() {
		joined := strings.Join(markdown, "\n")
		if strings.TrimSpace(joined) != "" {
			segments = append(segments, Segment{Kind: Markdown, Text: joined})
		}
		markdown = nil
	}
	closeTag := func(raw string) {
		flush()
		if spec := ParseSpec(raw); spec != nil {
			segments = append(segments, Segment{Kind: Render, Spec: spec})
		} else {
			// What cannot be drawn is still shown, as the JSON it is.
			segments = append(segments, Segment{Kind: Markdown, Text: "```json\n" + strings.TrimSpace(raw) + "\n```"})
		}
	}

	for _, whole := range strings.Split(text, "\n") {
		line := whole
		if capturingSet {
			end := strings.Index(line, closeTagText)
			if end < 0 {
				capturing += "\n" + line
				continue
			}
			closeTag(capturing + "\n" + line[:end])
			capturing, capturingSet = "", false
			line = line[end+len(closeTagText):]
			if strings.TrimSpace(line) == "" {
				continue
			}
		}
		if m := fence.FindStringSubmatch(line); m != nil {
			if fenceMarker == "" {
				fenceMarker = m[1]
			} else if m[1] == fenceMarker {
				fenceMarker = ""
			}
			markdown = append(markdown, line)
			continue
		}
		if fenceMarker != "" {
			markdown = append(markdown, line)
			continue
		}
		for {
			start := openTag(line)
			if start < 0 {
				if strings.TrimSpace(line) != "" || line == whole {
					markdown = append(markdown, line)
				}
				break
			}
			if before := line[:start]; strings.TrimSpace(before) != "" {
				markdown = append(markdown, before)
			}
			after := line[start+len(openTagText):]
			end := strings.Index(after, closeTagText)
			if end < 0 {
				capturing, capturingSet = after, true
				break
			}
			closeTag(after[:end])
			line = after[end+len(closeTagText):]
			if strings.TrimSpace(line) == "" {
				break
			}
		}
	}
	if capturingSet {
		flush()
		segments = append(segments, Segment{Kind: Pending, Raw: capturing})
	}
	flush()
	return segments
}

// openTag is where the line's first tag opens; one inside an inline code span
// is text.
func openTag(line string) int {
	spans := codeSpans(line)
	for from := 0; from <= len(line); {
		i := strings.Index(line[from:], openTagText)
		if i < 0 {
			return -1
		}
		at := from + i
		inside := false
		for _, s := range spans {
			if at > s[0] && at < s[1] {
				inside = true
				break
			}
		}
		if !inside {
			return at
		}
		from = at + 1
	}
	return -1
}

var backticks = regexp.MustCompile("`+")

// codeSpans are the line's code spans: a backtick run up to the next run of
// the same length.
func codeSpans(line string) [][2]int {
	var spans [][2]int
	open, openLength := -1, 0
	for _, run := range backticks.FindAllStringIndex(line, -1) {
		length := run[1] - run[0]
		if open < 0 {
			open, openLength = run[0], length
		} else if length == openLength {
			spans = append(spans, [2]int{open, run[0]})
			open = -1
		}
	}
	return spans
}

// ParseSpec reads a tag's JSON; nil when it cannot be drawn.
func ParseSpec(raw string) *Spec {
	var parsed any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed) != nil {
		return nil
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil
	}
	root, rootOK := obj["root"].(string)
	elements, elementsOK := obj["elements"].(map[string]any)
	if !rootOK || !elementsOK {
		return nil
	}
	normalized := map[string]Element{}
	for id, v := range elements {
		value, ok := v.(map[string]any)
		if !ok {
			continue
		}
		typ, ok := value["type"].(string)
		if !ok {
			continue
		}
		props, ok := value["props"].(map[string]any)
		if !ok {
			props = map[string]any{}
		}
		children := []string{}
		if list, ok := value["children"].([]any); ok {
			for _, c := range list {
				if s, ok := c.(string); ok {
					children = append(children, s)
				}
			}
		}
		normalized[id] = Element{Type: typ, Props: props, Children: children}
	}
	if _, ok := normalized[root]; !ok {
		return nil
	}
	return &Spec{Root: root, Elements: normalized}
}

// ChildrenOf are the elements under id that exist, each drawn once: a spec
// that lists an element twice, or loops back to an ancestor, still draws a
// finite tree.
func ChildrenOf(spec *Spec, id string, seen map[string]bool) []string {
	element, ok := spec.Elements[id]
	if !ok {
		return nil
	}
	var out []string
	for _, child := range element.Children {
		if _, exists := spec.Elements[child]; exists && !seen[child] {
			out = append(out, child)
		}
	}
	return out
}

// The prop readers: the model writes the props, so each takes what it can and
// falls back instead of failing.

func formatNumber(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

func Str(props map[string]any, key string) string {
	switch v := props[key].(type) {
	case string:
		return v
	case float64:
		return formatNumber(v)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func Num(props map[string]any, key string) (float64, bool) {
	switch v := props[key].(type) {
	case float64:
		if !math.IsInf(v, 0) && !math.IsNaN(v) {
			return v, true
		}
	case string:
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) {
			return n, true
		}
	}
	return 0, false
}

func Records(props map[string]any, key string) []map[string]any {
	var out []map[string]any
	list, _ := props[key].([]any)
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func Strings(props map[string]any, key string) []string {
	var out []string
	list, _ := props[key].([]any)
	for _, v := range list {
		switch v := v.(type) {
		case string:
			out = append(out, v)
		case float64:
			out = append(out, formatNumber(v))
		}
	}
	return out
}

func Numbers(props map[string]any, key string) []float64 {
	var out []float64
	list, _ := props[key].([]any)
	for _, v := range list {
		if n, ok := v.(float64); ok && !math.IsInf(n, 0) && !math.IsNaN(n) {
			out = append(out, n)
		}
	}
	return out
}

// Cell is a cell's text: strings and numbers as they are, anything else as JSON.
func Cell(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return formatNumber(v)
	case bool:
		return strconv.FormatBool(v)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(value) != nil {
		return ""
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// Tone is the meaning behind a colour or status name, so each Client draws it
// with its own tokens. Colour carries status only; anything else reads as plain.
type Tone string

const (
	ToneDefault Tone = "default"
	ToneMuted   Tone = "muted"
	ToneSuccess Tone = "success"
	ToneWarning Tone = "warning"
	ToneError   Tone = "error"
	ToneInfo    Tone = "info"
)

var tones = map[string]Tone{
	"success": ToneSuccess, "ok": ToneSuccess, "done": ToneSuccess, "completed": ToneSuccess, "green": ToneSuccess,
	"warning": ToneWarning, "warn": ToneWarning, "pending": ToneWarning, "yellow": ToneWarning,
	"error": ToneError, "danger": ToneError, "failed": ToneError, "red": ToneError,
	"info": ToneInfo, "tip": ToneInfo, "note": ToneInfo, "blue": ToneInfo, "cyan": ToneInfo,
	"gray": ToneMuted, "grey": ToneMuted, "dim": ToneMuted, "muted": ToneMuted, "secondary": ToneMuted,
}

func ToneOf(name string) Tone {
	if t, ok := tones[strings.ToLower(strings.TrimSpace(name))]; ok {
		return t
	}
	return ToneDefault
}

// Fraction is a fraction for a bar, clamped to 0..1.
func Fraction(value, max float64) float64 {
	if !(max > 0) {
		return 0
	}
	return math.Min(1, math.Max(0, value/max))
}
