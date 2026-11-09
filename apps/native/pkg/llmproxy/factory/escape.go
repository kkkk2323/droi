// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: QUOTED_TOOL_PREFIX 1447, quotedToolText 1459-1466, escapePhrases 1474-1485, escapeDeep 1490-1514,
// sanitizeBody 1518-1529.

package factory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// quotedToolPrefix opens tool output that quotes a refused phrase, which then follows as a JSON string: the model
// can still read the output whole, and Factory finds no phrase in it.
const quotedToolPrefix = "Tool output encoded as a JSON string. Decode the JSON string to recover the exact original text before using it:\n"

// quotedToolText is tool output as Factory takes it: as it came, unless it quotes a refused phrase; then as a JSON
// string after quotedToolPrefix, with the first character of each phrase escaped once more. Nothing is lost:
// decoding the string gives the output back. Quoting it twice changes nothing.
func quotedToolText(text string) string {
	if !containsAny(text, refusedPhrases) {
		return text
	}
	encoded := jsString(text)
	for _, phrase := range escapeInTool {
		encoded = strings.ReplaceAll(encoded, phrase, escapeFirst(phrase))
	}
	return quotedToolPrefix + encoded
}

// escapePhrases is text with the first character of each of phrases in it written as a \u escape, so a
// conversation that quotes one can still be sent, compacted or replayed.
func escapePhrases(text string, phrases []string) string {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			text = strings.ReplaceAll(text, phrase, escapeFirst(phrase))
		}
	}
	return text
}

// sanitizeBody is the last pass before a request leaves: every string value in body gets escapePhrases. A body
// that needs nothing, or is no JSON object or array, comes back as the very bytes it came as.
func sanitizeBody(body []byte, phrases []string) ([]byte, bool) {
	if !mayQuote(body, phrases) || !json.Valid(body) {
		return body, false
	}
	return escapeBody(body, phrases)
}

// escapeBody is sanitizeBody of body, which is valid JSON.
func escapeBody(body []byte, phrases []string) ([]byte, bool) {
	i := skipSpace(body, 0)
	if !mayQuote(body, phrases) || i >= len(body) || (body[i] != '{' && body[i] != '[') {
		return body, false
	}
	out, changed := escapeDeep(body[i:], phrases)
	if !changed {
		return body, false
	}
	return out, true
}

// escapeDeep is raw, a valid JSON value with no space before it, with escapePhrases on every string in it; object
// keys stay, and so does every value with nothing to escape, byte for byte.
func escapeDeep(raw json.RawMessage, phrases []string) (json.RawMessage, bool) {
	if !mayQuote(raw, phrases) {
		return raw, false
	}
	switch raw[0] {
	case '"':
		text := stringOf(raw)
		if out := escapePhrases(text, phrases); out != text {
			return jsonStr(out), true
		}
	case '{':
		o, _ := objectOf(raw)
		changed := false
		for _, k := range o.keys {
			if out, did := escapeDeep(o.get(k), phrases); did {
				o.set(k, out)
				changed = true
			}
		}
		if changed {
			return o.raw(), true
		}
	case '[':
		items, _ := arrayOf(raw)
		changed := false
		for i, item := range items {
			if out, did := escapeDeep(item, phrases); did {
				items[i], changed = out, true
			}
		}
		if changed {
			return jsonList(items), true
		}
	}
	return raw, false
}

// mayQuote reports a body that could hold one of phrases: one of them past its first character, or a \u escape,
// which could spell one. Most bodies hold neither and are not decoded at all.
func mayQuote(body []byte, phrases []string) bool {
	if bytes.Contains(body, []byte(`\u`)) {
		return true
	}
	for _, phrase := range phrases {
		_, size := utf8.DecodeRuneInString(phrase)
		if bytes.Contains(body, []byte(phrase[size:])) {
			return true
		}
	}
	return false
}

func containsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// escapeFirst is phrase with its first character written as a \u escape, as JavaScript's charCodeAt reads it.
func escapeFirst(phrase string) string {
	r, size := utf8.DecodeRuneInString(phrase)
	return fmt.Sprintf(`\u%04x`, r) + phrase[size:]
}

// jsString is text as JavaScript's JSON.stringify writes it.
func jsString(text string) string {
	return string(jsonStr(text))
}

// marshalJSON is v as JSON without Go's HTML escapes, which upstream's JSON.stringify does not write.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
