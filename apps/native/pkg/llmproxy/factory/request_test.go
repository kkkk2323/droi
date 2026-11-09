package factory

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The rest of upstream's golden cases that Droi's requests meet: request.test.mjs, droid.test.mjs, effort.test.mjs
// and models.test.mjs, one subtest per upstream test, its name kept. Each holds the half of the upstream test that a
// request of the Kernel's or droid's can reach.
func TestUpstreamRequests(t *testing.T) {
	sentAs := func(t *testing.T, path, body string) string {
		t.Helper()
		_, out, _ := Request(path, []byte(body))
		return string(out)
	}
	asCame := func(t *testing.T, path, body string) {
		t.Helper()
		if got := sentAs(t, path, body); got != body {
			t.Fatalf("%s changed:\n got %s\nwant %s", path, got, body)
		}
	}
	line := jsString(DroidLine)

	t.Run("keeps native Droid requests byte-for-byte and adds its preamble to generic callers", func(t *testing.T) {
		asCame(t, messages, `{ "model": "claude-sonnet-4-6", "system": [{"type":"text","text":`+line+`}], "messages": [{"role":"user","content":"OK"}] }`)
		b := send(t, messages, obj{"model": "claude-sonnet-4-6", "system": "Keep these exact instructions.", "messages": []any{obj{"role": "user", "content": "OK"}}})
		same(t, "system", b["system"], []any{obj{"type": "text", "text": DroidLine}, obj{"type": "text", "text": "Keep these exact instructions."}})
	})

	t.Run("does not change OpenAI requests, malformed JSON or invalid system schemas", func(t *testing.T) {
		asCame(t, responses, `{"model":"gpt-6-sol","instructions":`+line+`,"input":"OK"}`)
		for _, body := range []string{"not-json", `{"model":"claude-sonnet-4-6","system":{},"messages":[]}`, `{"system":[{"type":"text","text":123}],"messages":[]}`} {
			asCame(t, messages, body)
		}
	})

	t.Run("drops an empty or whitespace-only string system", func(t *testing.T) {
		for _, system := range []any{"", " \n\t", []any{obj{"type": "text", "text": ""}}} {
			b := send(t, messages, obj{"system": system, "messages": []any{obj{"role": "user", "content": "OK"}}})
			same(t, "system", b["system"], []any{obj{"type": "text", "text": DroidLine}})
		}
	})

	t.Run("preserves a native Droid string system without duplicating the preamble", func(t *testing.T) {
		for _, system := range []string{DroidLine, DroidLine + "\nKeep the user's instructions."} {
			asCame(t, messages, `{"system":`+jsString(system)+`,"messages":[{"role":"user","content":"OK"}]}`)
		}
	})

	t.Run("adapts token-counting requests consistently with Messages and leaves other paths alone", func(t *testing.T) {
		body := `{"system":"Count these instructions.","messages":[{"role":"user","content":"OK"}]}`
		if a, b := sentAs(t, messages, body), sentAs(t, count, body); a != b || a == body {
			t.Fatalf("Messages and its count differ:\n%s\n%s", a, b)
		}
		asCame(t, "/api/llm/a/v1/models", body)
	})

	t.Run("retains large integer tokens byte-for-byte in an unmodified native Droid request", func(t *testing.T) {
		asCame(t, messages, `{"system":[{"type":"text","text":`+line+`}],"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"Read","input":{"id":12345678901234567890}}]}]}`)
	})

	t.Run("another agent's request to /api/llm/o opens with droid's line", func(t *testing.T) {
		b := send(t, chat, json.RawMessage(`{"model":"glm-5.3-flash","messages":[{"role":"system","content":"You are Codex, a coding agent."},{"role":"user","content":"hi <b> & co"}],"stream":true,"reasoning_effort":"high"}`))
		same(t, "messages", b["messages"], []any{obj{"role": "system", "content": DroidLine + "\nYou are Codex, a coding agent."}, obj{"role": "user", "content": "hi <b> & co"}})
		same(t, "the rest", []any{b["model"], b["reasoning_effort"], b["stream"]}, []any{"glm-5.3-flash", "high", true})
		b = send(t, chat, json.RawMessage(`{"model":"kimi-k3","messages":[{"role":"system","content":[{"type":"text","text":"You are Claude Code."},{"type":"text","text":"Be brief."}]},{"role":"user","content":"hi"}]}`))
		same(t, "parts joined", at(b, "messages", 0), obj{"role": "system", "content": DroidLine + "\nYou are Claude Code.\nBe brief."})
		b = send(t, chat, json.RawMessage(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`))
		same(t, "no system prompt", b["messages"], []any{obj{"role": "system", "content": DroidLine}, obj{"role": "user", "content": "hi"}})
		b = send(t, responses, json.RawMessage(`{"model":"gpt-5.5","instructions":"You are Codex, a coding agent.","input":[{"role":"user","content":"hi"}],"stream":true}`))
		same(t, "instructions", b["instructions"], DroidLine+"\nYou are Codex, a coding agent.")
		same(t, "Responses without instructions", send(t, responses, json.RawMessage(`{"model":"grok-4.7","input":"hi"}`))["instructions"], DroidLine)
	})

	t.Run("droid's own requests go on byte for byte on each API", func(t *testing.T) {
		asCame(t, chat, `{"model":"glm-5.3-flash","messages":[{"role":"system","content":"`+DroidLine+`\nYou work in the user's terminal."},{"role":"user","content":"hi"}],"stream":true,"n":1.0}`)
		asCame(t, responses, `{"model":"gpt-5.5","input":[],"store":false,"instructions":"`+DroidLine+`\nYou work in the user's terminal.","stream":true}`)
		asCame(t, messages, `{"model":"claude-opus-5-5","system":[{"type":"text","text":"`+DroidLine+`"}],"messages":[{"role":"user","content":"hi"}]}`)
		asCame(t, messages, `{"model":"minimax-m2.7","system":[{"type":"text","text":"`+DroidLine+`"}],"messages":[{"role":"user","content":"hi"}]}`)
	})

	t.Run("bytes are read as the string, and what isn't JSON goes as it is", func(t *testing.T) {
		same(t, "instructions", send(t, responses, json.RawMessage(`{"model":"gpt-5.5","input":"hi"}`))["instructions"], DroidLine)
		asCame(t, responses, "not json")
		asCame(t, responses, `{"instructions":[1]}`)
		asCame(t, "/api/llm/o/v1/models", `{"a":1}`)
	})

	t.Run("none is sent on to Factory as it was asked", func(t *testing.T) {
		b := send(t, chat, json.RawMessage(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`))
		same(t, "reasoning_effort", b["reasoning_effort"], "none")
	})
}

// models.test.mjs and droid.test.mjs's model cases: the table holds what droid's registry says of a model, as the
// excerpts of droid's binary upstream keeps (testdata) say it.
func TestUpstreamModels(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	entry := func(t *testing.T, registry, id string) string {
		t.Helper()
		for _, l := range strings.Split(registry, "\n") {
			if strings.HasPrefix(l, `"`+id+`":{id:"`+id+`",`) {
				return l
			}
		}
		t.Fatalf("%s is not in the registry", id)
		return ""
	}
	match := func(t *testing.T, re, text string) []string {
		t.Helper()
		m := regexp.MustCompile(re).FindStringSubmatch(text)
		if m == nil {
			t.Fatalf("%s does not match %.200s", re, text)
		}
		return m
	}
	number := func(s string) int {
		f, _ := strconv.ParseFloat(s, 64)
		return int(f)
	}
	efforts := func(t *testing.T, text string) []string {
		t.Helper()
		var supported []string
		_ = json.Unmarshal([]byte(match(t, `reasoningEffort:\{supported:(\[[^\]]*\])`, text)[1]), &supported)
		return supported
	}
	model := func(t *testing.T, id string) Model {
		t.Helper()
		m, ok := ModelByID(id)
		if !ok {
			t.Fatalf("%s is not offered", id)
		}
		return m
	}
	// The table's efforts are droid's, its "off" written "none".
	asDroids := func(e []string) []string {
		out := slices.Clone(e)
		for i, x := range out {
			if x == "none" {
				out[i] = "off"
			}
		}
		return out
	}
	droid240, droid241, droid251 := read("droid-0.240.0-models.txt"), read("droid-0.241.1-gemini.txt"), read("droid-0.251.0-deepseek-1017.txt")

	t.Run("Haiku 5.5 and Mistral Large 4 are offered as droid 0.240.0 has them", func(t *testing.T) {
		for _, id := range []string{"claude-haiku-5-5", "mistral-large-4"} {
			text, m := entry(t, droid240, id), model(t, id)
			same(t, id+" name", m.Name, match(t, `,name:"([^"]+)"`, text)[1])
			var context, output int
			if l := regexp.MustCompile(`contextLimits:R\(\{maxInputTokens:(\d+),maxOutputTokens:(\d+)\}\)`).FindStringSubmatch(text); l != nil {
				context, output = number(l[1]), number(l[2])
			} else {
				name := match(t, `contextLimits:(\w+),`, text)[1]
				def := match(t, `(?m)^`+name+`=te\(\{[^}]*maxInputTokens:(\d+),maxOutputTokens:(\d+)\}\)`, droid240)
				context, output = number(def[1]), number(def[2])
			}
			same(t, id+" limits", []int{m.Context, m.Output}, []int{context, output})
			same(t, id+" efforts", slices.DeleteFunc(asDroids(m.Efforts), func(e string) bool { return e == "off" }), slices.DeleteFunc(efforts(t, text), func(e string) bool { return e == "off" }))
		}
		if !strings.Contains(entry(t, droid240, "mistral-large-4"), `apiModelProvider:"generic-chat-completion-api"`) || model(t, "mistral-large-4").Wire != WireChat {
			t.Fatal("Mistral Large 4 is on droid's generic chat completions")
		}
		if !strings.Contains(entry(t, droid240, "mistral-large-4"), "images:!0") || !model(t, "mistral-large-4").Images {
			t.Fatal("Mistral Large 4 takes images")
		}
		if model(t, "claude-haiku-5-5").Wire != WireAnthropic {
			t.Fatal("Haiku 5.5 is on Anthropic's Messages")
		}
	})

	t.Run("droid 0.241.1's Gemini models are offered on Factory's Gemini route, as droid has them", func(t *testing.T) {
		var picker []string
		_ = json.Unmarshal([]byte(match(t, `(?m)^var y=(\[[^\]]*\]);`, droid241)[1]), &picker)
		gemini := slices.DeleteFunc(picker, func(id string) bool { return !strings.HasPrefix(id, "gemini-") })
		same(t, "droid's Gemini models", gemini, []string{"gemini-4-argon", "gemini-3.1-pro-preview", "gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash", "gemini-3-flash-preview"})
		var offered []string
		for _, m := range Models() {
			if m.Wire == WireGemini {
				offered = append(offered, m.ID)
			}
		}
		same(t, "the table's", offered, gemini)
		if !strings.Contains(droid241, "function I(o,t){return O({maxInputTokens:o-t,maxOutputTokens:t})}") {
			t.Fatal("I(total, output) no longer states the total context")
		}
		for _, id := range gemini {
			text, m := entry(t, droid241, id), model(t, id)
			if !strings.Contains(text, `provider:"google"`) || m.Upstream != "google" {
				t.Fatalf("%s is Google's", id)
			}
			same(t, id+" name", m.Name, match(t, `,name:"([^"]+)"`, text)[1])
			l := match(t, `contextLimits:[Ik]\((\d+e\d+|\d+),(\d+)\)`, text)
			same(t, id+" limits", []int{m.Context, m.Output}, []int{number(l[1]), number(l[2])})
			same(t, id+" efforts", m.Efforts, efforts(t, text))
			// droid takes images unless an entry says images:!1
			if strings.Contains(text, "images:!1") || !m.Images {
				t.Fatalf("%s takes images", id)
			}
		}
	})

	t.Run("DeepSeek V4.1 Flash 1017 reaches both model hooks as droid 0.251.0 has it", func(t *testing.T) {
		id := "deepseek-v4.1-flash-1017"
		text, m := entry(t, droid251, id), model(t, id)
		for _, want := range []string{`apiModelProvider:"generic-chat-completion-api"`, `apiProviders:["fireworks","baseten","databricks"]`, "images:!0"} {
			if !strings.Contains(text, want) {
				t.Fatalf("the entry no longer says %s", want)
			}
		}
		if !strings.Contains(droid251, "function c(o,t){return R({maxInputTokens:o-t,maxOutputTokens:t})}") {
			t.Fatal("c(total, output) no longer states the total context")
		}
		l := match(t, `contextLimits:c\((\d+),(\d+)\)`, text)
		same(t, "name", m.Name, match(t, `,name:"([^"]+)"`, text)[1])
		same(t, "limits", []int{m.Context, m.Output}, []int{number(l[1]), number(l[2])})
		same(t, "efforts", asDroids(m.Efforts), efforts(t, text))
		rate, _ := strconv.ParseFloat(match(t, `cost:\{tokenMultiplier:([\d.]+)`, text)[1], 64)
		same(t, "rate", m.Rate, rate)
		if m.Wire != WireChat || m.Upstream != "fireworks" || !m.Images {
			t.Fatalf("%+v", m)
		}
	})

	t.Run("GLM-5.3-Flash takes images, as droid 0.231.0's registry says", func(t *testing.T) {
		if !model(t, "glm-5.3-flash").Images {
			t.Fatal("GLM-5.3-Flash takes images")
		}
	})
}
