// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: PI_OPENING, PI_COMPAT and clientOpening 478-482, PI_SUMMARIES and piSummary 494-502, promptOpenings
// 554-579.

package factory

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// piOpening is pi's opening sentence at the start of a line. Factory refuses it whole (yetone/magpie#952): either
// half passes, so it becomes its first half, piCompat, and the rest of the prompt stays.
var piOpening = regexp.MustCompile(`(^|\n)` + regexp.QuoteMeta(anywherePhrases[0]))

const piCompat = "You are an expert coding assistant."

// clientOpening is a system or developer prompt with pi's opening sentence, the first one at the start of a line,
// cut to its first half.
func clientOpening(text string) string {
	loc := piOpening.FindStringSubmatchIndex(text)
	if loc == nil {
		return text
	}
	return text[:loc[3]] + piCompat + text[loc[1]:]
}

// piSummaries are the fixed openings pi gives a compaction summary and a branch summary (pi-coding-agent
// dist/core/messages.js, COMPACTION_SUMMARY_PREFIX and BRANCH_SUMMARY_PREFIX), and what each becomes. Factory
// refuses the compaction one whole (yetone/magpie#1316): after a compaction every turn was 403.
var piSummaries = [][2]string{
	{anywherePhrases[1] + "\n\n<summary>\n", "Earlier conversation context is summarized below:\n\n<summary>\n"},
	{"The following is a summary of a branch that this conversation came back from:\n\n<summary>\n", "Summary of an earlier conversation branch:\n\n<summary>\n"},
}

// piSummary is a user text that is pi's whole summary, its opening first and </summary> last, opened in other
// words; the summary itself stays. Any other text comes back as it came.
func piSummary(text string) string {
	if !strings.HasSuffix(text, "</summary>") {
		return text
	}
	for _, s := range piSummaries {
		if strings.HasPrefix(text, s[0]) {
			return s[1] + text[len(s[0]):]
		}
	}
	return text
}

// promptOpenings adapts msgs, chat completions' messages or Responses' input, in place: pi's opening in system and
// developer messages, pi's summary in user messages, and refused phrases quoted in tool output (chat's tool
// messages, Responses' function_call_output). A string content or the text of its parts changes, nothing else:
// false when nothing did.
func promptOpenings(msgs []json.RawMessage) bool {
	changed := false
	for i, raw := range msgs {
		if !mayAdapt(raw) {
			continue
		}
		m, ok := objectOf(raw)
		if !ok {
			continue
		}
		field := "content"
		var adapt func(string) string
		switch role := rawString(m.get("role")); {
		case role == "system" || role == "developer":
			adapt = clientOpening
		case role == "user":
			adapt = piSummary
		case role == "tool":
			adapt = quotedToolText
		case rawString(m.get("type")) == "function_call_output":
			adapt, field = quotedToolText, "output"
		default:
			continue
		}
		out, did := adaptContent(m.get(field), adapt, false)
		if !did {
			continue
		}
		m.set(field, out)
		msgs[i], changed = m.raw(), true
	}
	return changed
}

// mayAdapt reports a message promptOpenings could change, so a long conversation's other messages are not decoded
// at all: pi's opening or compaction sentence, a pi summary (it ends with </summary>, which an escaped encoding
// spells \u003c/summary\u003e, so the check is its middle), or tool output (chat's tool messages, Responses'
// function_call_output). A message with a \u escape is decoded too: the escape could spell any of them.
func mayAdapt(raw json.RawMessage) bool {
	return bytes.Contains(raw, []byte(`\u`)) ||
		bytes.Contains(raw, []byte(anywherePhrases[0])) ||
		bytes.Contains(raw, []byte(anywherePhrases[1])) ||
		bytes.Contains(raw, []byte("/summary")) ||
		bytes.Contains(raw, []byte("\"tool\"")) ||
		bytes.Contains(raw, []byte("function_call_output"))
}

// adaptContent is a message's content with adapt applied to it: to a string, or to the text of each part that has
// one (only text parts', when textType). Nothing else in it changes.
func adaptContent(raw json.RawMessage, adapt func(string) string, textType bool) (json.RawMessage, bool) {
	if text, isString := rawStringOK(raw); isString {
		if out := adapt(text); out != text {
			return jsonStr(out), true
		}
		return raw, false
	}
	parts, isArray := arrayOf(raw)
	if !isArray {
		return raw, false
	}
	changed := false
	for i, part := range parts {
		p, isObject := objectOf(part)
		if !isObject || (textType && rawString(p.get("type")) != "text") {
			continue
		}
		text, isText := rawStringOK(p.get("text"))
		if !isText {
			continue
		}
		if out := adapt(text); out != text {
			p.set("text", jsonStr(out))
			parts[i], changed = p.raw(), true
		}
	}
	if !changed {
		return raw, false
	}
	return jsonList(parts), true
}
