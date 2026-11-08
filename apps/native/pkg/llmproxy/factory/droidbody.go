// Package factory is Droi's rewrite layer for Factory: the part of
// @magpie-community/opencode-factory-auth that Droi needs, written in Go
// inside the LLM proxy (design §5). Factory's usage policy check refuses a
// request that does not look like droid's, with 403 content_policy_rejected:
// one whose system prompt does not open with droid's line, and one that holds
// another client's fixed phrases, pi's compaction opening among them.
//
// Request is the way in. A request droid itself would send comes back byte
// for byte.
//
// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: droidBody 613-651, droidChat 581-611, anthropicBody 1609-1747
// (pi's half), geminiBody 663-703, GEMINI_CALL 661 and send's Gemini route
// 2058-2066. See PORTED-FROM.md.
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
	changed := false
	if loc := geminiCall.FindStringSubmatchIndex(path); loc != nil && strings.Contains(path, "/llm/g/") {
		model, err := url.PathUnescape(path[loc[2]:loc[3]])
		if err != nil {
			model = path[loc[2]:loc[3]]
		}
		path = path[:loc[0]] + "/generate"
		body, _ = geminiBody(model, body)
		changed = true
	} else {
		var sped bool
		body, sped = fastBody(path, body)
		body, changed = dialectBody(path, body)
		changed = changed || sped
	}
	// Last: a phrase escaped earlier must not be escaped again.
	body, escaped := sanitizeBody(body, anywherePhrases)
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
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	messages, ok := jsonArray(m["messages"])
	if !ok {
		return body, false
	}
	adapted := promptOpenings(messages)
	opened, changed := droidChat(messages)
	if !changed && !adapted {
		return body, false
	}
	if m["messages"], ok = jsonValue(opened); !ok {
		return body, false
	}
	return marshalObject(m, body)
}

// droidChat opens msgs' first system message with droid's line, or puts one
// before them with the line alone: false when it opens so already.
func droidChat(msgs []json.RawMessage) ([]json.RawMessage, bool) {
	if len(msgs) > 0 {
		first, ok := jsonObject(msgs[0])
		if ok && rawString(first["role"]) == "system" {
			switch content, state := openContent(first["content"]); state {
			case contentUnchanged:
				return msgs, false
			case contentChanged:
				first["content"] = content
				if out, ok := jsonValue(first); ok {
					msgs[0] = out
					return msgs, true
				}
				return msgs, false
			}
		}
	}
	identity, _ := jsonValue(map[string]string{"role": "system", "content": DroidLine})
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
		return mustJSON(lineWith(text)), contentChanged
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return raw, contentUnsupported
	}
	if len(parts) > 0 {
		if first, isText := textOf(parts[0]); isText && strings.HasPrefix(first, DroidLine) {
			return raw, contentUnchanged
		}
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		block, isObject := jsonObject(part)
		text, isText := rawStringOK(block["text"])
		if !isObject || rawString(block["type"]) != "text" || !isText {
			return mustJSON(append([]json.RawMessage{textPart(DroidLine)}, parts...)), contentChanged
		}
		texts = append(texts, text)
	}
	joined := DroidLine
	if len(texts) > 0 {
		joined += "\n" + strings.Join(texts, "\n")
	}
	return mustJSON(joined), contentChanged
}

// anthropicBody is a Messages request as droid sends it: its system as text
// blocks, droid's line first and once, pi's opening adapted in them; pi's
// opening in system messages, pi's summary in user text, and refused phrases
// quoted in tool results adapted; and, streamed, no context_management, which
// Factory's streaming route refuses. The billing header and the client context
// the reference also adapts belong to Claude Code's requests, which Droi does
// not send.
func anthropicBody(body []byte) ([]byte, bool) {
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	messages, ok := jsonArray(m["messages"])
	if !ok {
		return body, false
	}
	var blocks []json.RawMessage
	systemChanged := false
	if raw, has := m["system"]; has && !isJSONNull(raw) {
		if text, isString := rawStringOK(raw); isString {
			blocks = []json.RawMessage{textPart(text)}
			systemChanged = !strings.HasPrefix(text, DroidLine)
		} else if json.Unmarshal(raw, &blocks) != nil {
			return body, false
		}
	}
	// A block of another shape is not something droid sends: leave the body
	// as it came rather than guess at it.
	for _, block := range blocks {
		if b, isObject := jsonObject(block); isObject && rawString(b["type"]) == "text" {
			if _, isText := rawStringOK(b["text"]); !isText {
				return body, false
			}
		}
	}
	kept := make([]json.RawMessage, 0, len(blocks)+1)
	for _, block := range blocks {
		if text, isText := textOf(block); isText {
			if strings.TrimSpace(text) == "" {
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
	_, managed := m["context_management"]
	unmanaged := managed && strings.TrimSpace(string(m["stream"])) == "true"
	if !systemChanged && !messagesChanged && !unmanaged {
		return body, false
	}
	if systemChanged {
		if m["system"], ok = jsonValue(deduped); !ok {
			return body, false
		}
	}
	if messagesChanged {
		if m["messages"], ok = jsonValue(messages); !ok {
			return body, false
		}
	}
	if unmanaged {
		delete(m, "context_management")
	}
	return marshalObject(m, body)
}

// anthropicMessages adapts a Messages request's messages in place: pi's
// opening in a system message's text, pi's summary in a user's text, and
// tool results that quote a refused phrase. False when nothing changed.
func anthropicMessages(messages []json.RawMessage) bool {
	changed := false
	for i, raw := range messages {
		msg, ok := jsonObject(raw)
		if !ok {
			continue
		}
		var did bool
		switch rawString(msg["role"]) {
		case "system":
			msg["content"], did = adaptContent(msg["content"], clientOpening, true)
		case "user":
			msg["content"], did = userContent(msg["content"])
		}
		if did {
			messages[i], changed = mustJSON(msg), true
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
	blocks, isArray := jsonArray(raw)
	if !isArray {
		return raw, false
	}
	changed := false
	for i, block := range blocks {
		b, isObject := jsonObject(block)
		if !isObject {
			continue
		}
		var did bool
		switch rawString(b["type"]) {
		case "tool_result":
			b["content"], did = adaptContent(b["content"], quotedToolText, true)
		case "text":
			if text, isText := rawStringOK(b["text"]); isText {
				if out := piSummary(text); out != text {
					b["text"], did = mustJSON(out), true
				}
			}
		}
		if did {
			blocks[i], changed = mustJSON(b), true
		}
	}
	if !changed {
		return raw, false
	}
	return mustJSON(blocks), true
}

// withText is a text block with text in place of its own; its other fields stay.
func withText(block json.RawMessage, text string) json.RawMessage {
	b, ok := jsonObject(block)
	if !ok {
		return block
	}
	b["text"] = mustJSON(text)
	return mustJSON(b)
}

// geminiBody is a Gemini generateContent request for model as Factory's
// /api/llm/g/v1/generate takes it: the model in the body, no stream field,
// the system instruction without a role and opening with droid's line, as one
// text part of the line and the instruction's text parts joined with "\n",
// then its other parts. Gemini's route has no tool output adaptation of its
// own, so every refused phrase in it is escaped here. A body that cannot be
// read goes as it came.
func geminiBody(model string, body []byte) ([]byte, bool) {
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	m["model"] = mustJSON(model)
	delete(m, "stream")
	si := map[string]json.RawMessage{}
	if raw, has := m["systemInstruction"]; has {
		if text, isString := rawStringOK(raw); isString {
			si["parts"] = mustJSON([]json.RawMessage{geminiPart(text)})
		} else if obj, isObject := jsonObject(raw); isObject {
			si = obj
		}
	}
	delete(si, "role")
	parts, isArray := jsonArray(si["parts"])
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
	si["parts"] = mustJSON(parts)
	m["systemInstruction"] = mustJSON(si)
	out, ok := marshalObject(m, body)
	if !ok {
		return body, false
	}
	out, _ = sanitizeBody(out, refusedPhrases)
	return out, true
}

// opensWithDroidLine reports parts whose first part has a text that opens
// with the line; as in the reference, the instruction's parts then stay.
func opensWithDroidLine(parts []json.RawMessage) bool {
	if len(parts) == 0 {
		return false
	}
	first, isObject := jsonObject(parts[0])
	if !isObject {
		return false
	}
	text, isText := rawStringOK(first["text"])
	return isText && strings.HasPrefix(text, DroidLine)
}

// lineWith is text as droid opens a system prompt: the line, then the text,
// or the line alone when there is nothing else.
func lineWith(text string) string {
	if strings.TrimSpace(text) == "" {
		return DroidLine
	}
	return DroidLine + "\n" + text
}

// textPart is a Messages text block.
func textPart(text string) json.RawMessage {
	return mustJSON(map[string]string{"type": "text", "text": text})
}

// geminiPart is a Gemini text part, which carries no type field.
func geminiPart(text string) json.RawMessage {
	return mustJSON(map[string]string{"text": text})
}

// textOf is a Messages block's text, when the block is a text block.
func textOf(block json.RawMessage) (string, bool) {
	b, isObject := jsonObject(block)
	if !isObject || rawString(b["type"]) != "text" {
		return "", false
	}
	return rawStringOK(b["text"])
}

// pureText is a block that is one text and nothing else, as droid sends a
// Gemini system instruction's parts.
func pureText(part json.RawMessage) (string, bool) {
	b, isObject := jsonObject(part)
	if !isObject || len(b) != 1 {
		return "", false
	}
	return rawStringOK(b["text"])
}

// jsonObject decodes raw as a JSON object: false for anything else.
func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

// jsonArray decodes raw as a JSON array of values: false for anything else,
// including a JSON null and a missing field.
func jsonArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	var a []json.RawMessage
	if json.Unmarshal(raw, &a) != nil || a == nil {
		return nil, false
	}
	return a, true
}

// rawStringOK is raw as a JSON string: false for anything else, including a
// JSON null.
func rawStringOK(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
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

// jsonValue marshals v into a value a body's field can hold. It is marshalJSON: no HTML escapes, as upstream's
// JSON.stringify writes none, so adapted text keeps its < and &.
func jsonValue(v any) (json.RawMessage, bool) {
	b, err := marshalJSON(v)
	if err != nil {
		return nil, false
	}
	return b, true
}

// mustJSON marshals a value the package builds itself, which cannot fail.
func mustJSON(v any) json.RawMessage {
	b, _ := jsonValue(v)
	return b
}

// marshalObject answers body with m in place of it. An object's keys come out
// sorted, which the model does not see: only a body that needed a change
// takes this path.
func marshalObject(m map[string]json.RawMessage, body []byte) ([]byte, bool) {
	out, err := marshalJSON(m)
	if err != nil {
		return body, false
	}
	return out, true
}
