// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: droidBody's /responses branch 630-644.

package factory

import "strings"

// responsesBody is a Responses request as droid sends it: its instructions, a string, open with droid's line, pi's
// openings and refused tool output in its input are adapted (promptOpenings), and an OpenAI model's request goes
// without temperature and top_p, which OpenAI's reasoning models answer with a 400 and droid never sends.
// Instructions of another shape are not something droid sends: such a body goes as it came.
func responsesBody(body []byte) ([]byte, bool) {
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	instructions, has := "", false
	if raw, present := m["instructions"]; present && !isJSONNull(raw) {
		text, ok := rawStringOK(raw)
		if !ok {
			return body, false
		}
		instructions, has = text, true
	}
	changed := false
	if model, ok := ModelByID(rawString(m["model"])); ok && model.Upstream == "openai" {
		for _, sampling := range []string{"temperature", "top_p"} {
			if _, present := m[sampling]; present {
				delete(m, sampling)
				changed = true
			}
		}
	}
	if input, isArray := jsonArray(m["input"]); isArray && promptOpenings(input) {
		if m["input"], ok = jsonValue(input); !ok {
			return body, false
		}
		changed = true
	}
	opened := clientOpening(instructions)
	switch {
	case !strings.HasPrefix(opened, DroidLine):
		opened = lineWith(opened)
	case opened == instructions && has && !changed:
		return body, false
	}
	m["instructions"] = mustJSON(opened)
	return marshalObject(m, body)
}
