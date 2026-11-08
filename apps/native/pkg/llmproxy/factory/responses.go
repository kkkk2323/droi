// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: droidBody's /responses branch 630-644.

package factory

import "strings"

// responsesBody is a Responses request as droid sends it: its instructions, a string, open with droid's line, pi's
// openings and refused tool output in its input are adapted (promptOpenings), and an OpenAI model's request goes
// without temperature and top_p, which OpenAI's reasoning models answer with a 400 and droid never sends.
// Instructions of another shape are not something droid sends: such a body goes as it came.
func responsesBody(body []byte) ([]byte, bool) {
	m, ok := objectOf(body)
	if !ok {
		return body, false
	}
	instructions, has := "", false
	if raw := m.get("instructions"); raw != nil && !isJSONNull(raw) {
		text, ok := rawStringOK(raw)
		if !ok {
			return body, false
		}
		instructions, has = text, true
	}
	changed := false
	if model, ok := ModelByID(rawString(m.get("model"))); ok && model.Upstream == "openai" {
		for _, sampling := range []string{"temperature", "top_p"} {
			if m.has(sampling) {
				m.del(sampling)
				changed = true
			}
		}
	}
	if input, isArray := arrayOf(m.get("input")); isArray && promptOpenings(input) {
		m.set("input", jsonList(input))
		changed = true
	}
	opened := clientOpening(instructions)
	switch {
	case !strings.HasPrefix(opened, DroidLine):
		opened = lineWith(opened)
	case opened == instructions && has && !changed:
		return body, false
	}
	m.set("instructions", jsonStr(opened))
	return m.raw(), true
}
