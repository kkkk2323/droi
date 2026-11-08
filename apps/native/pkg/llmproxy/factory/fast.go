// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: FAST_BETA 125, FAST_OF 126, fastBody 135-158, send's beta 2077-2082.

package factory

import (
	"bytes"
	"net/http"
	"strings"
)

// FastModeBeta is the anthropic-beta droid sends with an Anthropic Fast Mode model (its anthropicFastMode).
const FastModeBeta = "fast-mode-2026-02-01"

// fastBody sends a Fast Mode model as droid does: an Anthropic one at speed "fast", an OpenAI one at the priority
// service tier. A request that asks its base model for fast (speed "fast" on Messages, service_tier "priority" on
// Responses) goes to the base model's Fast Mode model, as droid's fast toggle switches to it: asked of the base
// model, Factory answers at standard speed and says nothing (magpie-community/plugins#119).
func fastBody(path string, body []byte) ([]byte, bool) {
	anthropic := strings.Contains(path, "/llm/a/") && strings.HasSuffix(path, "/messages")
	responses := strings.Contains(path, "/llm/o/") && strings.HasSuffix(path, "/responses")
	if !anthropic && !responses {
		return body, false
	}
	// A Fast Mode id, speed "fast" and the priority tier all hold one of these, unless a \u escape spells them.
	if !bytes.Contains(body, []byte("fast")) && !bytes.Contains(body, []byte("priority")) && !bytes.Contains(body, []byte(`\u`)) {
		return body, false
	}
	m, ok := jsonObject(body)
	if !ok {
		return body, false
	}
	model, ok := rawStringOK(m["model"])
	if !ok {
		return body, false
	}
	wire, field, value := WireResponses, "service_tier", "priority"
	if anthropic {
		wire, field, value = WireAnthropic, "speed", "fast"
	}
	asked := rawString(m[field]) == value
	target := model
	if _, has := ModelByID(model + "-fast"); asked && has {
		target = model + "-fast"
	}
	if fast, ok := ModelByID(target); !ok || !strings.HasSuffix(target, "-fast") || fast.Wire != wire {
		return body, false
	}
	if target == model && asked {
		return body, false
	}
	m["model"] = mustJSON(target)
	m[field] = mustJSON(value)
	return marshalObject(m, body)
}

// FastBeta adds FastModeBeta to h's anthropic-beta for a request on path whose body names model, when droid would
// send it: an Anthropic Fast Mode model on Messages. A beta already there is not added twice.
func FastBeta(h http.Header, path, model string) {
	if !strings.Contains(path, "/llm/a/") || !strings.HasSuffix(path, "/messages") || !strings.HasSuffix(model, "-fast") {
		return
	}
	if m, ok := ModelByID(model); !ok || m.Wire != WireAnthropic {
		return
	}
	var betas []string
	for _, v := range h.Values("Anthropic-Beta") {
		for _, b := range strings.Split(v, ",") {
			if b = strings.TrimSpace(b); b != "" {
				if b == FastModeBeta {
					return
				}
				betas = append(betas, b)
			}
		}
	}
	h.Set("Anthropic-Beta", strings.Join(append(betas, FastModeBeta), ","))
}
