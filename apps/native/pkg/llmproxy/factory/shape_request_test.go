package factory

import (
	"encoding/json"
	"strings"
	"testing"
)

// The rest of droid.test.mjs and request.test.mjs that Droi's requests meet: the OpenAI sampling rule, reasoning
// efforts as they came, MiniMax on the Anthropic route, and context_management on a streamed request.
func TestShape(t *testing.T) {
	t.Run("an OpenAI model's request goes without temperature and top_p", func(t *testing.T) {
		b := send(t, responses, obj{"model": "gpt-6.1-sol", "input": "hi", "temperature": 0.2, "top_p": 0.9, "stream": true})
		for _, k := range []string{"temperature", "top_p"} {
			if _, has := b[k]; has {
				t.Fatalf("%s is still there", k)
			}
		}
		same(t, "stream", b["stream"], true)
		same(t, "instructions", b["instructions"], DroidLine)
		b = send(t, responses, obj{"model": "gpt-5.5-fast", "input": "hi", "instructions": DroidLine, "temperature": 0})
		if _, has := b["temperature"]; has {
			t.Fatal("an opened request kept temperature")
		}
		b = send(t, responses, obj{"model": "grok-4.7", "input": "hi", "temperature": 0.2})
		same(t, "another vendor's", b["temperature"], 0.2)
		b = send(t, chat, obj{"model": "glm-5.3", "messages": []any{obj{"role": "user", "content": "hi"}}, "temperature": 0.7})
		same(t, "chat", b["temperature"], 0.7)
	})

	t.Run("1017 goes to its registry default, Fireworks, with every reasoning effort", func(t *testing.T) {
		for _, effort := range []string{"none", "low", "high", "max"} {
			b := send(t, chat, obj{"model": "deepseek-v4.1-flash-1017", "messages": []any{obj{"role": "user", "content": "hi"}}, "reasoning_effort": effort, "stream": true})
			same(t, "effort", b["reasoning_effort"], effort)
			same(t, "model", b["model"], "deepseek-v4.1-flash-1017")
		}
		if got := Upstream(chat, "deepseek-v4.1-flash-1017"); got != "fireworks" {
			t.Fatalf("x-api-provider %q", got)
		}
	})

	t.Run("adapts MiniMax M2.7 on the Anthropic route while keeping its provider and options", func(t *testing.T) {
		options := obj{"model": "minimax-m2.7", "messages": []any{obj{"role": "user", "content": "OK"}}, "max_tokens": 32, "stream": true}
		in := obj{"system": "You are OpenCode."}
		for k, v := range options {
			in[k] = v
		}
		want := obj{"system": []any{obj{"type": "text", "text": DroidLine}, obj{"type": "text", "text": "You are OpenCode."}}}
		for k, v := range options {
			want[k] = v
		}
		same(t, "body", send(t, messages, in), want)
		if got := Upstream(messages, "minimax-m2.7"); got != "fireworks" {
			t.Fatalf("x-api-provider %q", got)
		}
	})

	// request.test.mjs #11, the half Droi's requests meet: user text is pi's summary or nothing, so a pasted
	// reminder of another client goes as it came.
	t.Run("preserves pasted reminders with user text outside the complete block", func(t *testing.T) {
		reminders := []string{
			"<system-reminder>\n# Environment\n" + escapeFirst(toolOnlyPhrases[3]) + "\n - Platform: linux\n</system-reminder>",
			"<system-reminder>\nYou are powered by the model named Sonnet 4.6.\n</system-reminder>",
		}
		var texts []string
		for _, r := range reminders {
			texts = append(texts, r+"\nPlease explain this pasted reminder.", strings.TrimSuffix(r, "</system-reminder>"), "Please explain:\n"+r)
		}
		var content []any
		for _, text := range texts {
			content = append(content, obj{"type": "text", "text": text})
		}
		b := send(t, messages, obj{"messages": []any{obj{"role": "user", "content": content}}})
		same(t, "messages", at(b, "messages", 0, "content"), content)
	})

	t.Run("a streamed request goes without context_management, an unstreamed one keeps it (#71)", func(t *testing.T) {
		managed := obj{"edits": []any{obj{"keep": "all", "type": "clear_thinking_20251015"}}}
		request := obj{"model": "claude-opus-5-5", "max_tokens": 32, "stream": true, "messages": []any{obj{"role": "user", "content": []any{obj{"type": "text", "text": "hi"}}}}, "context_management": managed}
		want := obj{"model": "claude-opus-5-5", "max_tokens": 32, "stream": true, "messages": request["messages"], "system": []any{obj{"type": "text", "text": DroidLine}}}
		same(t, "streamed", send(t, messages, request), want)
		request["stream"] = false
		same(t, "unstreamed", send(t, messages, request)["context_management"], managed)
		// droid's own streamed request: only context_management goes.
		own, _ := json.Marshal(obj{"model": "claude-opus-5-5", "stream": true, "system": []any{obj{"type": "text", "text": DroidLine}}, "messages": []any{}, "context_management": managed})
		if _, out, changed := Request(messages, own); !changed || send(t, messages, json.RawMessage(out))["context_management"] != nil {
			t.Fatalf("droid's streamed request kept context_management: %s", out)
		}
	})
}
