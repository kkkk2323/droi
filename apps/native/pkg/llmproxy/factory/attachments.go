// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: KEEP_ATTACHMENTS 717, LEFT_OUT 718, TOO_LARGE_HINT 722, dialectOf 726, DIALECTS 734,
// leaveOutAttachments 744, tooLarge 782.

package factory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Factory answers a request body over its size limit with 413 "Request Entity Too Large": a limit on bytes, not on
// the model's context, which the images a conversation gathers reach long before its tokens do (yetone/magpie#2082:
// nine screenshots, about 4.5 MB of data URLs). droid 0.239.0 leaves older images and files out of such a request;
// the proxy does the same once, on a 413.

// keepAttachments is how many of the newest images and files a request asked again keeps.
const keepAttachments = 1

// LeftOut stands in place of an image or file left out, so each tool result stays beside its call.
const LeftOut = "[An earlier image or file was left out of this request: it was over Factory's request size limit.]"

// TooLargeHint is what to do about a 413.
const TooLargeHint = "This is a request-body byte limit, not the model's context: compact the conversation, or start a new one with a short summary; fewer images or smaller tool results also help."

type attachmentDialect struct {
	list string
	is   func(map[string]any) bool
	text func(map[string]any) map[string]any
}

func partType(p map[string]any) string {
	s, _ := p["type"].(string)
	return s
}

var attachmentDialects = map[string]attachmentDialect{
	"anthropic": {"messages",
		func(p map[string]any) bool { return partType(p) == "image" || partType(p) == "document" },
		func(p map[string]any) map[string]any {
			out := map[string]any{"type": "text", "text": LeftOut}
			if c, ok := p["cache_control"]; ok && c != nil && c != false {
				out["cache_control"] = c
			}
			return out
		}},
	"responses": {"input",
		func(p map[string]any) bool { return partType(p) == "input_image" || partType(p) == "input_file" },
		func(map[string]any) map[string]any { return map[string]any{"type": "input_text", "text": LeftOut} }},
	"chat": {"messages",
		func(p map[string]any) bool { return partType(p) == "image_url" || partType(p) == "file" },
		func(map[string]any) map[string]any { return map[string]any{"type": "text", "text": LeftOut} }},
	"gemini": {"contents",
		func(p map[string]any) bool {
			for _, k := range []string{"inlineData", "inline_data", "fileData", "file_data"} {
				if v, ok := p[k]; ok && v != nil {
					return true
				}
			}
			return false
		},
		func(map[string]any) map[string]any { return map[string]any{"text": LeftOut} }},
}

// attachmentDialectOf is which API's body a request to path carries, as attachments are found in it, or "" for one
// that has none to leave out.
func attachmentDialectOf(path string) string {
	switch {
	case strings.Contains(path, "/llm/a/") && strings.HasSuffix(path, "/messages"):
		return "anthropic"
	case strings.HasSuffix(path, "/responses"):
		return "responses"
	case strings.HasSuffix(path, "/chat/completions"):
		return "chat"
	case geminiCall.MatchString(path) || strings.HasSuffix(path, "/llm/g/v1/generate"):
		return "gemini"
	}
	return ""
}

// LeaveOutAttachments is body, a request to path, with every image and file but the newest left out, and how many
// were: 0 when there are none to leave out or the body cannot be read. Each becomes a LeftOut text in its place; in
// a Gemini function response's parts, which take inline data alone, it goes.
func LeaveOutAttachments(path string, body []byte) ([]byte, int) {
	d, ok := attachmentDialects[attachmentDialectOf(path)]
	if !ok {
		return nil, 0
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if dec.Decode(&m) != nil || m == nil {
		return nil, 0
	}
	list, ok := m[d.list].([]any)
	if !ok {
		return nil, 0
	}
	w := attachmentWalk{d: d}
	w.walk(list, false)
	if w.found <= keepAttachments {
		return nil, 0
	}
	w.cut, w.seen = w.found-keepAttachments, 0
	m[d.list] = w.walk(list, false)
	out, err := marshalJSON(m)
	if err != nil {
		return nil, 0
	}
	return out, w.cut
}

// attachmentWalk finds a body's attachments in the order they were sent, then, with cut set, leaves the first cut
// of them out. An object's keys are walked in sorted order: in every dialect only one key of an object holds parts.
type attachmentWalk struct {
	d                attachmentDialect
	found, cut, seen int
}

func (w *attachmentWalk) walk(v any, inFunctionResponse bool) any {
	switch x := v.(type) {
	case []any:
		out := x[:0:0]
		for _, p := range x {
			if obj, ok := p.(map[string]any); ok && w.d.is(obj) {
				w.seen++
				if w.cut == 0 {
					w.found++
				} else if w.seen <= w.cut {
					if inFunctionResponse {
						continue
					}
					p = w.d.text(obj)
				}
				out = append(out, p)
				continue
			}
			out = append(out, w.walk(p, inFunctionResponse))
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			x[k] = w.walk(x[k], inFunctionResponse || k == "functionResponse")
		}
		return x
	}
	return v
}

// TooLarge is what a 413 of Factory's says to the person: Factory's own words, that the limit is on bytes, how many
// older images or files were left out before it was asked again, and TooLargeHint.
func TooLarge(body []byte, left int) string {
	tried := ""
	if left > 0 {
		s := "s"
		if left == 1 {
			s = ""
		}
		tried = fmt.Sprintf(" It was still too large with %d older image%s or file%s left out.", left, s, s)
	}
	return "Factory refused the request as too large (" + VendorError(body, "413") + "): it limits the size of a request's body in bytes." + tried + " " + TooLargeHint
}
