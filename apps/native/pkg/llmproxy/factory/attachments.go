// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: KEEP_ATTACHMENTS 717, LEFT_OUT 718, TOO_LARGE_HINT 722, dialectOf 726, DIALECTS 734,
// leaveOutAttachments 744, tooLarge 782.

package factory

import (
	"encoding/json"
	"fmt"
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
	is   func(*object) bool
	text func(*object) json.RawMessage
}

func leftOutText(kind string) json.RawMessage {
	return newObject("type", jsonStr(kind), "text", jsonStr(LeftOut)).raw()
}

var attachmentDialects = map[string]attachmentDialect{
	"anthropic": {"messages",
		func(p *object) bool { t := rawString(p.get("type")); return t == "image" || t == "document" },
		func(p *object) json.RawMessage {
			out := newObject("type", jsonStr("text"), "text", jsonStr(LeftOut))
			if c := p.get("cache_control"); truthy(c) {
				out.set("cache_control", c)
			}
			return out.raw()
		}},
	"responses": {"input",
		func(p *object) bool { t := rawString(p.get("type")); return t == "input_image" || t == "input_file" },
		func(*object) json.RawMessage { return leftOutText("input_text") }},
	"chat": {"messages",
		func(p *object) bool { t := rawString(p.get("type")); return t == "image_url" || t == "file" },
		func(*object) json.RawMessage { return leftOutText("text") }},
	"gemini": {"contents",
		func(p *object) bool {
			for _, k := range []string{"inlineData", "inline_data", "fileData", "file_data"} {
				if truthy(p.get(k)) {
					return true
				}
			}
			return false
		},
		func(*object) json.RawMessage { return newObject("text", jsonStr(LeftOut)).raw() }},
}

// truthy is whether a JSON value is truthy as JavaScript reads it: present, and not null, false, 0 or "".
func truthy(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	switch s := strings.TrimSpace(string(raw)); s {
	case "null", "false", `""`:
		return false
	default:
		var n float64
		if json.Unmarshal(raw, &n) == nil {
			return n != 0
		}
		return true
	}
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
	if !json.Valid(body) {
		return nil, 0
	}
	m, ok := objectOf(body)
	if !ok {
		return nil, 0
	}
	list := m.get(d.list)
	if _, ok := arrayOf(list); !ok {
		return nil, 0
	}
	w := attachmentWalk{d: d}
	w.walk(list, "")
	if w.found <= keepAttachments {
		return nil, 0
	}
	w.cut, w.seen = w.found-keepAttachments, 0
	out, _ := w.walk(list, "")
	m.set(d.list, out)
	return canonical(m.raw()), w.cut
}

// attachmentWalk finds a body's attachments in the order they were sent, an object's keys in JavaScript's order as
// upstream's Object.entries walks them, then, with cut set, leaves the first cut of them out.
type attachmentWalk struct {
	d                attachmentDialect
	found, cut, seen int
}

// walk is raw with the attachments it holds left out, and whether any was; key is the field raw is under, or under
// one of, so a Gemini function response's parts are told apart.
func (w *attachmentWalk) walk(raw json.RawMessage, key string) (json.RawMessage, bool) {
	if items, ok := arrayOf(raw); ok {
		out := make([]json.RawMessage, 0, len(items))
		changed := false
		for _, p := range items {
			if obj, ok := objectOf(p); ok && w.d.is(obj) {
				w.seen++
				if w.cut == 0 {
					w.found++
				} else if w.seen <= w.cut {
					changed = true
					if key == "functionResponse" {
						continue
					}
					p = w.d.text(obj)
				}
				out = append(out, p)
				continue
			}
			next, did := w.walk(p, key)
			changed = changed || did
			out = append(out, next)
		}
		if !changed {
			return raw, false
		}
		return jsonList(out), true
	}
	o, ok := objectOf(raw)
	if !ok {
		return raw, false
	}
	changed := false
	for _, k := range jsOrder(o.keys) {
		under := key
		if k == "functionResponse" {
			under = k
		}
		if next, did := w.walk(o.get(k), under); did {
			o.set(k, next)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	return o.raw(), true
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
