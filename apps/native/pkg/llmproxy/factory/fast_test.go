package factory

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fast.test.mjs (magpie-community/plugins#119): droid 0.240.0's Fast Mode entries and request code, kept byte for
// byte in testdata/droid-0.240.0-fast.txt.

var offeredFast = []string{"claude-opus-5-5-fast", "claude-opus-5-fast", "claude-opus-4-8-fast", "gpt-5.5-fast", "gpt-5.4-fast", "gpt-5.3-codex-fast"}

func fastFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/droid-0.240.0-fast.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fastEntry(t *testing.T, droid, id string) string {
	t.Helper()
	for _, line := range strings.Split(droid, "\n") {
		if strings.HasPrefix(line, `"`+id+`":{id:"`+id+`",`) {
			return line
		}
	}
	t.Fatalf("%s is not in the fixture", id)
	return ""
}

func TestFast(t *testing.T) {
	droid := fastFixture(t)

	t.Run("droid's Fast Mode models are offered at their base model's limits and efforts", func(t *testing.T) {
		for _, id := range offeredFast {
			text := fastEntry(t, droid, id)
			base := regexp.MustCompile(`baseVariant:"([^"]+)"`).FindStringSubmatch(text)[1]
			m, ok := ModelByID(id)
			b, okBase := ModelByID(base)
			if !ok || !okBase {
				t.Fatalf("%s or its base %s is missing", id, base)
			}
			if name := regexp.MustCompile(`,name:"([^"]+)"`).FindStringSubmatch(text)[1]; m.Name != name {
				t.Errorf("%s: name %q, want %q", id, m.Name, name)
			}
			if m.Context != b.Context || m.Output != b.Output || m.Upstream != b.Upstream || m.Wire != b.Wire {
				t.Errorf("%s: not at %s's limits and provider", id, base)
			}
			var efforts []string
			if err := json.Unmarshal([]byte(regexp.MustCompile(`reasoningEffort:\{supported:(\[[^\]]*\])`).FindStringSubmatch(text)[1]), &efforts); err != nil {
				t.Fatal(err)
			}
			efforts = slices.DeleteFunc(efforts, func(e string) bool { return e == "off" })
			if !slices.Equal(m.Efforts, efforts) {
				t.Errorf("%s: efforts %v, want %v", id, m.Efforts, efforts)
			}
		}
		for _, id := range []string{"claude-opus-4-6-fast", "claude-opus-4-7-fast", "gpt-5.4-mini-fast", "gpt-5.6-sol-fast", "glm-5.2-fast"} {
			if _, ok := ModelByID(id); ok {
				t.Errorf("%s is offered", id)
			}
		}
		if !strings.Contains(fastEntry(t, droid, "gpt-5.6-sol-fast"), "unreleased:!0") || !strings.Contains(fastEntry(t, droid, "glm-5.2-fast"), "featureFlag:") {
			t.Fatal("the reasons two are left out are gone from the fixture")
		}
	})

	t.Run("an Opus Fast Mode model goes out as droid sends it: its own id, speed fast and the fast-mode beta", func(t *testing.T) {
		if !strings.Contains(fastEntry(t, droid, "claude-opus-5-5-fast"), "anthropicFastMode:!0") ||
			!strings.Contains(droid, `if(t?.anthropicFastMode){if(_.speed="fast",!f.betaFlags.includes("fast-mode-2026-02-01"))`) {
			t.Fatal("droid's fast request code is not in the fixture")
		}
		sent := send(t, messages, obj{"model": "claude-opus-5-5-fast", "max_tokens": 8, "messages": []any{obj{"role": "user", "content": "hi"}}})
		same(t, "model", sent["model"], "claude-opus-5-5-fast")
		same(t, "speed", sent["speed"], "fast")
		h := http.Header{"Anthropic-Beta": {"client-beta"}}
		Headers(h, messages, "claude-opus-5-5-fast", "s")
		same(t, "beta", h.Get("Anthropic-Beta"), "client-beta,fast-mode-2026-02-01")
		same(t, "provider", h.Get("X-Api-Provider"), "anthropic")

		// the issue's second case: Opus 5.5 asked for fast reaches Opus 5.5 Fast
		sent = send(t, messages, obj{"model": "claude-opus-5-5", "speed": "fast", "max_tokens": 8, "messages": []any{obj{"role": "user", "content": "hi"}}})
		same(t, "model", sent["model"], "claude-opus-5-5-fast")
		same(t, "speed", sent["speed"], "fast")
		h = http.Header{"Anthropic-Beta": {"fast-mode-2026-02-01"}}
		Headers(h, messages, "claude-opus-5-5-fast", "s")
		same(t, "beta once", h.Get("Anthropic-Beta"), "fast-mode-2026-02-01")

		// not asked for fast: Opus 5.5 as it was
		sent = send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 8, "messages": []any{obj{"role": "user", "content": "hi"}}})
		same(t, "model", sent["model"], "claude-opus-5-5")
		if _, has := sent["speed"]; has {
			t.Fatal("speed was added")
		}
		h = http.Header{}
		Headers(h, messages, "claude-opus-5-5", "s")
		if h.Values("Anthropic-Beta") != nil {
			t.Fatalf("beta on a standard request: %v", h.Values("Anthropic-Beta"))
		}
	})

	t.Run("a GPT Fast Mode model goes out as droid sends it: its own id at the priority tier", func(t *testing.T) {
		if !strings.Contains(fastEntry(t, droid, "gpt-5.5-fast"), "apiRequest:Ee,") ||
			!strings.Contains(droid, `Ee=C({parallelToolCalls:!0,extendedCache:!0,safetyId:!0,verbosity:"low",serviceTier:"priority"})`) {
			t.Fatal("droid's GPT fast request config is not in the fixture")
		}
		sent := send(t, responses, obj{"model": "gpt-5.5-fast", "input": "hi"})
		same(t, "model", sent["model"], "gpt-5.5-fast")
		same(t, "tier", sent["service_tier"], "priority")
		h := http.Header{}
		Headers(h, responses, "gpt-5.5-fast", "s")
		same(t, "provider", h.Get("X-Api-Provider"), "openai")
		if h.Values("Anthropic-Beta") != nil {
			t.Fatal("an OpenAI request got the Anthropic beta")
		}
		// GPT-5.5 at the priority tier is its Fast Mode model
		sent = send(t, responses, obj{"model": "gpt-5.5", "service_tier": "priority", "input": "hi"})
		same(t, "model", sent["model"], "gpt-5.5-fast")
		// a model with no Fast Mode model is left as it was asked
		sent = send(t, responses, obj{"model": "gpt-6-sol", "service_tier": "priority", "input": "hi"})
		same(t, "model", sent["model"], "gpt-6-sol")
		same(t, "tier", sent["service_tier"], "priority")
	})

	// Upstream's fourth case is the picker's order: the Fast Mode models after every other model.
	t.Run("the Fast Mode models come after every other model", func(t *testing.T) {
		var ids []string
		for _, m := range Models() {
			ids = append(ids, m.ID)
		}
		first := slices.IndexFunc(ids, func(id string) bool { return strings.HasSuffix(id, "-fast") })
		if first < 0 || !reflect.DeepEqual(ids[first:], offeredFast) {
			t.Fatalf("fast models: %v", ids[max(first, 0):])
		}
	})

	t.Run("a Fast Mode request already in droid's shape comes back byte for byte", func(t *testing.T) {
		for path, body := range map[string]string{
			messages:  `{"max_tokens":8,"messages":[],"model":"claude-opus-5-5-fast","speed":"fast","system":[{"type":"text","text":"` + DroidLine + `"}]}`,
			responses: `{"input":"hi","instructions":"` + DroidLine + `","model":"gpt-5.5-fast","service_tier":"priority"}`,
		} {
			if _, out, changed := Request(path, []byte(body)); changed || string(out) != body {
				t.Errorf("%s changed: %s", path, out)
			}
		}
	})

	t.Run("count_tokens, chat completions, a prompt that says fast and another wire's model are not sped", func(t *testing.T) {
		sent := send(t, count, obj{"model": "claude-opus-5-5", "speed": "fast", "messages": []any{}})
		same(t, "count model", sent["model"], "claude-opus-5-5")
		h := http.Header{}
		Headers(h, count, "claude-opus-5-5-fast", "s")
		if h.Values("Anthropic-Beta") != nil {
			t.Fatal("count_tokens got the beta")
		}
		sent = send(t, chat, obj{"model": "gpt-5.5", "service_tier": "priority", "messages": []any{obj{"role": "user", "content": "hi"}}})
		same(t, "chat model", sent["model"], "gpt-5.5")
		// a prompt that says "fast" or "priority" does not ask for it
		said := []any{obj{"role": "user", "content": "make it fast, top priority"}}
		same(t, "said on Messages", send(t, messages, obj{"model": "claude-opus-5-5", "messages": said})["model"], "claude-opus-5-5")
		same(t, "said on Responses", send(t, responses, obj{"model": "gpt-5.5", "input": said})["model"], "gpt-5.5")
		// a value written with \u escapes says what it decodes to
		for _, raw := range []string{
			`{"model":"claude-opus-5-5","speed":"fa\u0073t","messages":[]}`,
			`{"model":"claude-opus-5-5-fa\u0073t","messages":[]}`,
		} {
			_, out, _ := Request(messages, []byte(raw))
			var got obj
			_ = json.Unmarshal(out, &got)
			if got["model"] != "claude-opus-5-5-fast" || got["speed"] != "fast" {
				t.Errorf("%s went as %s", raw, out)
			}
		}
		_, out, _ := Request(responses, []byte(`{"model":"gpt-5.5","service_tier":"priori\u0074y","input":"hi"}`))
		if !strings.Contains(string(out), `"model":"gpt-5.5-fast"`) {
			t.Errorf("an escaped tier went as %s", out)
		}
		// a Fast Mode model of the other wire stays as it was asked
		sent = send(t, messages, obj{"model": "gpt-5.5", "speed": "fast", "messages": []any{}})
		same(t, "another wire's model", sent["model"], "gpt-5.5")
	})
}
