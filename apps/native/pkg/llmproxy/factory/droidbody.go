// Package factory is Droi's rewrite layer for Factory: the part of
// @magpie-community/opencode-factory-auth that Droi needs, written in Go
// inside the LLM proxy (design §5). Factory's usage policy check refuses a
// request that does not look like droid's, with 403 content_policy_rejected:
// one whose system prompt does not open with droid's line, and one that holds
// another client's fixed phrases, pi's compaction opening among them.
//
// Request is the way in. A request droid itself would send comes back byte
// for byte; one that changes goes out as upstream's JSON.stringify writes it.
//
// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: droidBody 613-651, droidChat 581-611, anthropicBody 1609-1747,
// geminiBody 663-703, GEMINI_CALL 661 and send's Gemini route 2058-2066.
// See PORTED-FROM.md.
package factory

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// DroidLine is how droid opens a system prompt, and the first thing Factory's
// usage policy check looks for. It opens a chat completions system message,
// Responses' instructions, a Messages system block and a Gemini system
// instruction alike.
const DroidLine = "You are Droid, an AI software engineering agent built by Factory."

// geminiCall is the end of the path Google's clients send a Gemini request
// to, the model in it.
var geminiCall = regexp.MustCompile(`(?i)/models/([^/:]+):(?:stream)?generateContent$`)

// Request is a model request to Factory at path as droid sends it: the path
// it goes to, its body, and whether either changed. A Gemini request in
// Google's shape goes to Factory's own route, /api/llm/g/v1/generate, the
// model in its body; when the path changes, the request's query goes too.
// A Fast Mode request names its Fast Mode model and asks for its speed
// (fastBody; FastBeta is its header). Then the body takes droid's shape for
// its route (chat completions, Responses, Messages, Gemini), and last, every
// refused phrase still in it is escaped. A body that cannot be read goes as
// it came.
func Request(path string, body []byte) (string, []byte, bool) {
	// A body that is not JSON goes as it came; the rest of the package reads one that is.
	valid := json.Valid(body)
	changed := false
	if loc := geminiCall.FindStringSubmatchIndex(path); loc != nil && strings.Contains(path, "/llm/g/") {
		model, err := url.PathUnescape(path[loc[2]:loc[3]])
		if err != nil {
			model = path[loc[2]:loc[3]]
		}
		path = path[:loc[0]] + "/generate"
		if valid {
			body, _ = geminiBody(model, body)
		}
		changed = true
	} else if valid {
		var sped bool
		body, sped = fastBody(path, body)
		body, changed = dialectBody(path, body)
		changed = changed || sped
	}
	if !valid {
		return path, body, changed
	}
	// Last: a phrase escaped earlier must not be escaped again.
	body, escaped := escapeBody(body, anywherePhrases)
	if changed || escaped {
		body = canonical(body)
	}
	return path, body, changed || escaped
}

// dialectBody answers body, a request to Factory at path, in droid's shape
// for its route, and whether it changed: chat completions, Responses,
// Messages and its token count. Any other route, a body that cannot be read,
// and one droid would send as it is are returned as they came.
func dialectBody(path string, body []byte) ([]byte, bool) {
	switch {
	case strings.Contains(path, "/llm/o/") && strings.HasSuffix(path, "/responses"):
		return responsesBody(body)
	case strings.Contains(path, "/llm/o/") && strings.HasSuffix(path, "/chat/completions"):
		return chatBody(body)
	case strings.Contains(path, "/llm/a/") && (strings.HasSuffix(path, "/messages") || strings.HasSuffix(path, "/messages/count_tokens")):
		return anthropicBody(body)
	}
	return body, false
}

// chatBody is a chat completions request with its messages adapted
// (promptOpenings) and its first system message opening with droid's line.
func chatBody(body []byte) ([]byte, bool) {
	m, ok := objectOf(body)
	if !ok {
		return body, false
	}
	messages, ok := arrayOf(m.get("messages"))
	if !ok {
		return body, false
	}
	adapted := promptOpenings(messages)
	opened, changed := droidChat(messages)
	if !changed && !adapted {
		return body, false
	}
	m.set("messages", jsonList(opened))
	return m.raw(), true
}

// droidChat opens msgs' first system message with droid's line, or puts one
// before them with the line alone: false when it opens so already.
func droidChat(msgs []json.RawMessage) ([]json.RawMessage, bool) {
	if len(msgs) > 0 {
		first, ok := objectOf(msgs[0])
		if ok && rawString(first.get("role")) == "system" {
			switch content, state := openContent(first.get("content")); state {
			case contentUnchanged:
				return msgs, false
			case contentChanged:
				first.set("content", content)
				msgs[0] = first.raw()
				return msgs, true
			}
		}
	}
	identity := newObject("role", jsonStr("system"), "content", jsonStr(DroidLine)).raw()
	return append([]json.RawMessage{identity}, msgs...), true
}

// contentState is what openContent found: a content it cannot open (a system
// message of another shape, which gets the line before it with the rest of
// the list), one that opens so already, or one that changed.
type contentState int

const (
	contentUnsupported contentState = iota
	contentUnchanged
	contentChanged
)

// openContent answers content, a chat completions system message's content,
// with droid's line first. droid sends one string, its blocks joined with
// "\n", so text-only parts become one string; a part that is not text keeps
// its array and gets the line as a text block before it.
func openContent(raw json.RawMessage) (json.RawMessage, contentState) {
	if text, ok := rawStringOK(raw); ok {
		if strings.HasPrefix(text, DroidLine) {
			return raw, contentUnchanged
		}
		return jsonStr(lineWith(text)), contentChanged
	}
	parts, isArray := arrayOf(raw)
	if !isArray {
		return raw, contentUnsupported
	}
	if len(parts) > 0 {
		if first, isText := textOf(parts[0]); isText && strings.HasPrefix(first, DroidLine) {
			return raw, contentUnchanged
		}
	}
	texts := make([]string, 0, len(parts)+1)
	texts = append(texts, DroidLine)
	for _, part := range parts {
		text, isText := textOf(part)
		if !isText {
			return jsonList(append([]json.RawMessage{textPart(DroidLine)}, parts...)), contentChanged
		}
		texts = append(texts, text)
	}
	return jsonStr(strings.Join(texts, "\n")), contentChanged
}

// anthropicBody is a Messages request as droid sends it: its system as text
// blocks, droid's line first and once, pi's opening adapted in them; pi's
// opening in system messages, pi's summary in user text, and refused phrases
// quoted in tool results adapted; and, streamed, no context_management, which
// Factory's streaming route refuses.
func anthropicBody(body []byte) ([]byte, bool) {
	m, ok := objectOf(body)
	if !ok {
		return body, false
	}
	messages, ok := arrayOf(m.get("messages"))
	if !ok {
		return body, false
	}
	var blocks []json.RawMessage
	systemChanged := false
	if raw := m.get("system"); raw != nil && !isJSONNull(raw) {
		if text, isString := rawStringOK(raw); isString {
			blocks = []json.RawMessage{textPart(text)}
			systemChanged = !strings.HasPrefix(text, DroidLine)
		} else if blocks, ok = arrayOf(raw); !ok {
			return body, false
		}
	}
	// A block of another shape is not something droid sends: leave the body
	// as it came rather than guess at it.
	for _, block := range blocks {
		if b, isObject := objectOf(block); isObject && rawString(b.get("type")) == "text" {
			if _, isText := rawStringOK(b.get("text")); !isText {
				return body, false
			}
		}
	}
	kept := make([]json.RawMessage, 0, len(blocks)+1)
	for _, block := range blocks {
		if text, isText := textOf(block); isText {
			if jsTrim(text) == "" {
				systemChanged = true
				continue
			}
			if adapted := clientOpening(text); adapted != text {
				block = withText(block, adapted)
				systemChanged = true
			}
		}
		kept = append(kept, block)
	}
	first := -1
	for i, block := range kept {
		if text, isText := textOf(block); isText && strings.HasPrefix(text, DroidLine) {
			first = i
			break
		}
	}
	switch {
	case first < 0:
		kept = append([]json.RawMessage{textPart(DroidLine)}, kept...)
		systemChanged = true
	case first > 0:
		moved := append([]json.RawMessage{kept[first]}, kept[:first]...)
		kept = append(moved, kept[first+1:]...)
		systemChanged = true
	}
	// Only a block that is the line alone is a duplicate; one that carries
	// instructions after it stays.
	deduped := make([]json.RawMessage, 0, len(kept))
	for i, block := range kept {
		if text, isText := textOf(block); isText && text == DroidLine && i > 0 {
			systemChanged = true
			continue
		}
		deduped = append(deduped, block)
	}
	messagesChanged := anthropicMessages(messages)
	// Factory's streaming route refuses context_management ("Extra inputs are
	// not permitted") while its other route takes it: a streamed request goes
	// without it, and the client's own compaction still runs.
	unmanaged := m.has("context_management") && strings.TrimSpace(string(m.get("stream"))) == "true"
	if !systemChanged && !messagesChanged && !unmanaged {
		return body, false
	}
	if systemChanged {
		m.set("system", jsonList(deduped))
	}
	if messagesChanged {
		m.set("messages", jsonList(messages))
	}
	if unmanaged {
		m.del("context_management")
	}
	return m.raw(), true
}

// anthropicMessages adapts a Messages request's messages in place: pi's
// opening in a system message's text, pi's summary in a user's text, and
// tool results that quote a refused phrase. False when nothing changed.
func anthropicMessages(messages []json.RawMessage) bool {
	changed := false
	for i, raw := range messages {
		msg, ok := objectOf(raw)
		if !ok {
			continue
		}
		var content json.RawMessage
		var did bool
		switch rawString(msg.get("role")) {
		case "system":
			content, did = adaptContent(msg.get("content"), clientOpening, true)
		case "user":
			content, did = userContent(msg.get("content"))
		}
		if did {
			msg.set("content", content)
			messages[i], changed = msg.raw(), true
		}
	}
	return changed
}

// userContent is a Messages user turn's content adapted: a string, or its
// text blocks, as pi's summary; its tool results' text quoted when it holds a
// refused phrase. Other blocks and fields stay.
func userContent(raw json.RawMessage) (json.RawMessage, bool) {
	if _, isString := rawStringOK(raw); isString {
		return adaptContent(raw, piSummary, false)
	}
	blocks, isArray := arrayOf(raw)
	if !isArray {
		return raw, false
	}
	changed := false
	for i, block := range blocks {
		b, isObject := objectOf(block)
		if !isObject {
			continue
		}
		switch rawString(b.get("type")) {
		case "tool_result":
			if content, did := adaptContent(b.get("content"), quotedToolText, true); did {
				b.set("content", content)
				blocks[i], changed = b.raw(), true
			}
		case "text":
			if text, isText := rawStringOK(b.get("text")); isText {
				if out := piSummary(text); out != text {
					b.set("text", jsonStr(out))
					blocks[i], changed = b.raw(), true
				}
			}
		}
	}
	if !changed {
		return raw, false
	}
	return jsonList(blocks), true
}

// withText is a text block with text in place of its own; its other fields stay.
func withText(block json.RawMessage, text string) json.RawMessage {
	b, ok := objectOf(block)
	if !ok {
		return block
	}
	b.set("text", jsonStr(text))
	return b.raw()
}

// geminiBody is a Gemini generateContent request for model as Factory's
// /api/llm/g/v1/generate takes it: the model in the body, no stream field,
// the system instruction without a role and opening with droid's line, as one
// text part of the line and the instruction's text parts joined with "\n",
// then its other parts. Gemini's route has no tool output adaptation of its
// own, so every refused phrase in it is escaped here. A body that cannot be
// read goes as it came.
func geminiBody(model string, body []byte) ([]byte, bool) {
	m, ok := objectOf(body)
	if !ok {
		return body, false
	}
	m.set("model", jsonStr(model))
	m.del("stream")
	si := newObject()
	if raw := m.get("systemInstruction"); raw != nil {
		if text, isString := rawStringOK(raw); isString {
			si.set("parts", jsonList([]json.RawMessage{geminiPart(text)}))
		} else if obj, isObject := objectOf(raw); isObject {
			si = obj
		}
	}
	si.del("role")
	parts, isArray := arrayOf(si.get("parts"))
	if !isArray {
		parts = []json.RawMessage{}
	}
	if !opensWithDroidLine(parts) {
		texts := make([]string, 0, len(parts))
		rest := make([]json.RawMessage, 0, len(parts))
		for _, part := range parts {
			if text, isText := pureText(part); isText {
				texts = append(texts, text)
				continue
			}
			rest = append(rest, part)
		}
		parts = append([]json.RawMessage{geminiPart(lineWith(strings.Join(texts, "\n")))}, rest...)
	}
	si.set("parts", jsonList(parts))
	m.set("systemInstruction", si.raw())
	out, _ := escapeBody(m.raw(), refusedPhrases)
	return out, true
}

// opensWithDroidLine reports parts whose first part has a text that opens
// with the line; as in the reference, the instruction's parts then stay.
func opensWithDroidLine(parts []json.RawMessage) bool {
	if len(parts) == 0 {
		return false
	}
	first, isObject := objectOf(parts[0])
	if !isObject {
		return false
	}
	text, isText := rawStringOK(first.get("text"))
	return isText && strings.HasPrefix(text, DroidLine)
}

// lineWith is text as droid opens a system prompt: the line, then the text,
// or the line alone when there is nothing else.
func lineWith(text string) string {
	if jsTrim(text) == "" {
		return DroidLine
	}
	return DroidLine + "\n" + text
}

// jsTrim is s without the white space and line terminators JavaScript's trim removes at its ends.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
			return true
		}
		return r >= 0x2000 && r <= 0x200a
	})
}

// textPart is a Messages text block.
func textPart(text string) json.RawMessage {
	return newObject("type", jsonStr("text"), "text", jsonStr(text)).raw()
}

// geminiPart is a Gemini text part, which carries no type field.
func geminiPart(text string) json.RawMessage {
	return newObject("text", jsonStr(text)).raw()
}

// textOf is a Messages block's text, when the block is a text block.
func textOf(block json.RawMessage) (string, bool) {
	b, isObject := objectOf(block)
	if !isObject || rawString(b.get("type")) != "text" {
		return "", false
	}
	return rawStringOK(b.get("text"))
}

// pureText is a block that is one text and nothing else, as droid sends a
// Gemini system instruction's parts.
func pureText(part json.RawMessage) (string, bool) {
	b, isObject := objectOf(part)
	if !isObject || b.len() != 1 {
		return "", false
	}
	return rawStringOK(b.get("text"))
}

// rawStringOK is raw as a JSON string: false for anything else, including a
// JSON null.
func rawStringOK(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil || isJSONNull(raw) {
		return "", false
	}
	return s, true
}

// rawString is raw as a JSON string, or "" for anything else: for fields
// whose type only decides which branch a request takes.
func rawString(raw json.RawMessage) string {
	s, _ := rawStringOK(raw)
	return s
}

// isJSONNull reports the JSON null.
func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
