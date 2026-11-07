// Package factory is Droi's rewrite layer for Factory: the part of
// @magpie-community/opencode-factory-auth that Droi needs, written in Go
// inside the LLM proxy (design §5). A request to Factory must open its system
// prompt with droid's line: Factory's usage policy check refuses one that
// says it is something else, with 403 content_policy_rejected.
//
// Only the identity opening lives here so far; the rest of the layer (4B and
// 4C of the M4 plan) still comes. A request droid itself would send comes
// back byte for byte.
//
// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: droidBody 613-651, droidChat 581-611, anthropicBody's system
// half 1609-1674, geminiBody's identity half 663-698. See PORTED-FROM.md.
package factory

import (
	"encoding/json"
	"regexp"
	"strings"
)

// DroidLine is how droid opens a system prompt, and the first thing Factory's
// usage policy check looks for. It opens a chat completions system message,
// Responses' instructions, a Messages system block and a Gemini system
// instruction alike.
const DroidLine = "You are Droid, an AI software engineering agent built by Factory."

// geminiCall is the AI SDK's Google package request path, the model in it.
var geminiCall = regexp.MustCompile(`(?i)/llm/g/v1/models/([^/:]+):(?:stream)?generateContent$`)

// DroidBody answers body, a request to Factory at path, as droid opens it, and
// whether it changed: the first system message of a chat completions
// request, Responses' instructions, a Messages request's system, a Gemini
// request's system instruction. Anything else, a body that cannot be read,
// and one that already opens so are returned as they came.
func DroidBody(path string, body []byte) ([]byte, bool) {
	switch {
	case strings.Contains(path, "/llm/o/") && strings.HasSuffix(path, "/responses"):
		return responsesBody(body)
	case strings.Contains(path, "/llm/o/") && strings.HasSuffix(path, "/chat/completions"):
		return chatBody(body)
	case strings.Contains(path, "/llm/a/") && (strings.HasSuffix(path, "/messages") || strings.HasSuffix(path, "/messages/count_tokens")):
		return anthropicBody(body)
	case geminiCall.MatchString(path):
		return geminiBody(body)
	}
	return body, false
}

// chatBody opens a chat completions request's first system message.
func chatBody(body []byte) ([]byte, bool) {
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	messages, ok := jsonArray(m["messages"])
	if !ok {
		return body, false
	}
	opened, changed := droidChat(messages)
	if !changed {
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

// responsesBody opens a Responses request's instructions, which droid sends
// as a string; instructions of another shape are not something droid sends.
func responsesBody(body []byte) ([]byte, bool) {
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	instructions := ""
	if raw, has := m["instructions"]; has && !isJSONNull(raw) {
		text, ok := rawStringOK(raw)
		if !ok {
			return body, false
		}
		instructions = text
	}
	if strings.HasPrefix(instructions, DroidLine) {
		return body, false
	}
	opened, ok := jsonValue(lineWith(instructions))
	if !ok {
		return body, false
	}
	m["instructions"] = opened
	return marshalObject(m, body)
}

// anthropicBody opens a Messages request's system with droid's line, as a
// text block first. That half is all Droi needs of the reference's
// anthropicBody: the billing header it strips and the client context it
// adapts belong to Claude Code's requests, which Droi does not send.
func anthropicBody(body []byte) ([]byte, bool) {
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	if _, ok := jsonArray(m["messages"]); !ok {
		return body, false
	}
	var blocks []json.RawMessage
	changed := false
	if raw, has := m["system"]; has && !isJSONNull(raw) {
		if text, isString := rawStringOK(raw); isString {
			blocks = []json.RawMessage{textPart(text)}
			changed = !strings.HasPrefix(text, DroidLine)
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
		if text, isText := textOf(block); isText && strings.TrimSpace(text) == "" {
			changed = true
			continue
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
		changed = true
	case first > 0:
		moved := append([]json.RawMessage{kept[first]}, kept[:first]...)
		kept = append(moved, kept[first+1:]...)
		changed = true
	}
	// Only a block that is the line alone is a duplicate; one that carries
	// instructions after it stays.
	deduped := make([]json.RawMessage, 0, len(kept))
	for i, block := range kept {
		if text, isText := textOf(block); isText && text == DroidLine && i > 0 {
			changed = true
			continue
		}
		deduped = append(deduped, block)
	}
	if !changed {
		return body, false
	}
	system, ok := jsonValue(deduped)
	if !ok {
		return body, false
	}
	m["system"] = system
	return marshalObject(m, body)
}

// geminiBody opens a Gemini request's system instruction with droid's line,
// as one text part of the line and the instruction's own text parts; its
// role, which Factory's /generate route does not take, goes. Only this half
// of geminiBody is here: the model in the body and the /generate path come
// with the rest of the Gemini route.
func geminiBody(body []byte) ([]byte, bool) {
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	var parts []json.RawMessage
	raw, has := m["systemInstruction"]
	if has && !isJSONNull(raw) {
		if text, isString := rawStringOK(raw); isString {
			// A string instruction is one text part, as the reference makes it.
			parts = []json.RawMessage{geminiPart(text)}
		} else {
			si, isObject := jsonObject(raw)
			if !isObject {
				return body, false
			}
			if p, hasParts := si["parts"]; hasParts {
				if json.Unmarshal(p, &parts) != nil {
					return body, false
				}
			}
			if _, hasRole := si["role"]; !hasRole && opensWithDroidLine(parts) {
				return body, false
			}
		}
	}
	texts := make([]string, 0, len(parts))
	rest := make([]json.RawMessage, 0, len(parts))
	for _, part := range parts {
		if text, isText := pureText(part); isText {
			texts = append(texts, text)
			continue
		}
		rest = append(rest, part)
	}
	instruction := DroidLine
	if joined := strings.Join(texts, "\n"); strings.HasPrefix(joined, DroidLine) {
		instruction = joined
	} else if strings.TrimSpace(joined) != "" {
		instruction = DroidLine + "\n" + joined
	}
	system, ok := jsonValue(map[string]any{"parts": append([]json.RawMessage{geminiPart(instruction)}, rest...)})
	if !ok {
		return body, false
	}
	m["systemInstruction"] = system
	return marshalObject(m, body)
}

// opensWithDroidLine reports a system instruction whose first part opens with
// the line; as in the reference, such an instruction is left as it came.
func opensWithDroidLine(parts []json.RawMessage) bool {
	if len(parts) == 0 {
		return false
	}
	first, isText := pureText(parts[0])
	return isText && strings.HasPrefix(first, DroidLine)
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

// jsonValue marshals v into a value a body's field can hold.
func jsonValue(v any) (json.RawMessage, bool) {
	b, err := json.Marshal(v)
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
// sorted, which the model does not see: only a body whose system prompt needed
// opening takes this path.
func marshalObject(m map[string]json.RawMessage, body []byte) ([]byte, bool) {
	out, err := json.Marshal(m)
	if err != nil {
		return body, false
	}
	return out, true
}
