package factory

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// pi's golden cases: pi.test.mjs, one subtest per upstream test, its name kept. pi's requests as the reporters of
// yetone/magpie#952 and #1316 captured them; the refused sentences come from phrases.go, so this file holds none.

var (
	piOpeningSentence = anywherePhrases[0]
	piCompacted       = anywherePhrases[1]
	escapedOpening    = escapeFirst(piOpeningSentence)
	escapedCompacted  = escapeFirst(piCompacted)
	piRest            = " You help users by reading files, executing commands, editing code, and writing new files.\n\nAvailable tools:\n- read: Read file contents"
	piPrompt          = piOpeningSentence + piRest
	piAdapted         = "You are an expert coding assistant." + piRest
	piSummaryText     = "## Goal\nFix the login redirect.\n\n## Progress\n### Done\n- [x] Read src/auth.ts\n\n## Next Steps\n1. Patch the callback\n\n<read-files>\nsrc/auth.ts\n</read-files>"
	piCompaction      = piCompacted + "\n\n<summary>\n" + piSummaryText + "\n</summary>"
	adaptedCompaction = "Earlier conversation context is summarized below:\n\n<summary>\n" + piSummaryText + "\n</summary>"
	piBranch          = "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n" + piSummaryText + "</summary>"
	adaptedBranch     = "Summary of an earlier conversation branch:\n\n<summary>\n" + piSummaryText + "</summary>"
	piPartial         = "You are an expert coding assistant operating inside pi."
	piSnapshot        = "{\n  \"piVersion\": \"1.1.0\",\n  \"systemPrompt\": \"" + piPrompt + "\"\n}"
	piRecord          = `{"role":"user","content":"` + piCompaction + `"}`
	piTools           = func() []any {
		var tools []any
		for _, name := range []string{"read", "bash", "edit", "write"} {
			tools = append(tools, obj{"type": "function", "function": obj{"name": name, "description": name, "parameters": obj{"type": "object", "properties": obj{}}}})
		}
		return tools
	}()
)

type obj = map[string]any

// send is the body Factory gets for body sent to path, decoded.
func send(t *testing.T, path string, body any) obj {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	_, out, _ := Request(path, raw)
	var got obj
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, out)
	}
	return got
}

// same fails unless got and want are the same JSON.
func same(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(norm(t, got), norm(t, want)) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Fatalf("%s:\n got %s\nwant %s", what, g, w)
	}
}

func norm(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func at(v any, path ...any) any {
	for _, step := range path {
		switch k := step.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k < 0 || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func str(v any, path ...any) string {
	s, _ := at(v, path...).(string)
	return s
}

// quoted is a tool output quotedToolText encoded: the text it decodes to.
func quoted(t *testing.T, out string) string {
	t.Helper()
	if !strings.HasPrefix(out, quotedToolPrefix) {
		t.Fatalf("not quoted: %q", out)
	}
	var text string
	if err := json.Unmarshal([]byte(out[len(quotedToolPrefix):]), &text); err != nil {
		t.Fatalf("the quoted output is not a JSON string: %v", err)
	}
	return text
}

func TestPi(t *testing.T) {
	t.Run("pi's opening sentence is cut to its first half on chat completions, the rest of its prompt kept", func(t *testing.T) {
		b := send(t, chat, obj{"model": "deepseek-v4.1-flash", "messages": []any{obj{"role": "developer", "content": piPrompt}, obj{"role": "user", "content": "hi"}}, "tools": piTools, "stream": true})
		same(t, "messages", b["messages"], []any{obj{"role": "system", "content": DroidLine}, obj{"role": "developer", "content": piAdapted}, obj{"role": "user", "content": "hi"}})
		same(t, "tools", b["tools"], piTools)
		c := send(t, chat, obj{"model": "kimi-k3", "messages": []any{obj{"role": "system", "content": piPrompt}, obj{"role": "user", "content": "hi"}}})
		same(t, "a system message", at(c, "messages", 0), obj{"role": "system", "content": DroidLine + "\n" + piAdapted})
		c = send(t, chat, obj{"model": "kimi-k3", "messages": []any{obj{"role": "developer", "content": []any{obj{"type": "text", "text": piPrompt}}}, obj{"role": "user", "content": "hi"}}})
		same(t, "text parts", at(c, "messages", 1), obj{"role": "developer", "content": []any{obj{"type": "text", "text": piAdapted}}})
	})

	t.Run("pi's opening sentence is adapted in Messages' system block, its cache_control kept", func(t *testing.T) {
		b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "system": []any{obj{"type": "text", "text": piPrompt, "cache_control": obj{"type": "ephemeral"}}}, "messages": []any{obj{"role": "user", "content": []any{obj{"type": "text", "text": "hi"}}}}})
		same(t, "system", b["system"], []any{obj{"type": "text", "text": DroidLine}, obj{"type": "text", "text": piAdapted, "cache_control": obj{"type": "ephemeral"}}})
		same(t, "messages", b["messages"], []any{obj{"role": "user", "content": []any{obj{"type": "text", "text": "hi"}}}})
	})

	t.Run("pi's opening sentence is adapted in Responses' instructions and input", func(t *testing.T) {
		b := send(t, responses, obj{"model": "gpt-6.1", "instructions": piPrompt, "input": []any{obj{"role": "user", "content": "hi"}}})
		same(t, "instructions", b["instructions"], DroidLine+"\n"+piAdapted)
		b = send(t, responses, obj{"model": "gpt-6.1", "input": []any{obj{"role": "developer", "content": piPrompt}, obj{"role": "user", "content": "hi"}}})
		same(t, "no instructions", b["instructions"], DroidLine)
		same(t, "input", at(b, "input", 0), obj{"role": "developer", "content": piAdapted})
		b = send(t, responses, obj{"model": "gpt-6.1", "instructions": DroidLine + "\n" + piPrompt, "input": []any{obj{"role": "user", "content": "hi"}}})
		same(t, "droid's own instructions", b["instructions"], DroidLine+"\n"+piAdapted)
	})

	t.Run("pi's opening quoted in user, assistant or system text is escaped, a fragment kept", func(t *testing.T) {
		quote := "Why does pi say: " + piOpeningSentence
		b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "system": []any{obj{"type": "text", "text": "Quote: " + piOpeningSentence}}, "messages": []any{
			obj{"role": "user", "content": []any{obj{"type": "text", "text": quote}}},
			obj{"role": "assistant", "content": []any{obj{"type": "text", "text": quote}}},
			obj{"role": "user", "content": []any{obj{"type": "text", "text": piPartial}}},
		}})
		same(t, "system", str(b, "system", 1, "text"), "Quote: "+escapedOpening)
		same(t, "user", str(b, "messages", 0, "content", 0, "text"), "Why does pi say: "+escapedOpening)
		same(t, "assistant", str(b, "messages", 1, "content", 0, "text"), "Why does pi say: "+escapedOpening)
		same(t, "fragment", str(b, "messages", 2, "content", 0, "text"), piPartial)
	})

	t.Run("a compaction-shaped user text is escaped, so a poisoned conversation can still be summarized", func(t *testing.T) {
		serialized := "Summarize this conversation:\n<conversation>\n[Tool result]: " + piOpeningSentence + "\n[Assistant]: done\n</conversation>"
		want := strings.Replace(serialized, piOpeningSentence, escapedOpening, 1)
		b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "messages": []any{obj{"role": "user", "content": []any{obj{"type": "text", "text": serialized}}}}})
		same(t, "Messages", str(b, "messages", 0, "content", 0, "text"), want)
		c := send(t, chat, obj{"model": "deepseek-v4.1-flash", "messages": []any{obj{"role": "user", "content": serialized}}})
		same(t, "chat", str(c, "messages", 1, "content"), want)
	})

	t.Run("a tool_use argument that quotes pi's opening is escaped too", func(t *testing.T) {
		b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "messages": []any{
			obj{"role": "user", "content": "hi"},
			obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "t1", "name": "bash", "input": obj{"command": "echo " + piOpeningSentence}}}},
			obj{"role": "user", "content": []any{obj{"type": "tool_result", "tool_use_id": "t1", "content": "done"}}},
		}})
		same(t, "command", str(b, "messages", 1, "content", 0, "input", "command"), "echo "+escapedOpening)
		if raw, _ := json.Marshal(b); strings.Contains(string(raw), piOpeningSentence) {
			t.Fatal("the sentence is still in the body")
		}
	})

	t.Run("pi's compaction summary opens without its fixed sentence on Messages, the summary kept", func(t *testing.T) {
		after := []any{obj{"role": "assistant", "content": []any{obj{"type": "text", "text": "Patched."}}}, obj{"role": "user", "content": []any{obj{"type": "text", "text": "go on"}}}}
		b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "system": []any{obj{"type": "text", "text": piPrompt, "cache_control": obj{"type": "ephemeral"}}}, "messages": append([]any{obj{"role": "user", "content": []any{obj{"type": "text", "text": piCompaction}}}}, after...)})
		same(t, "messages", b["messages"], append([]any{obj{"role": "user", "content": []any{obj{"type": "text", "text": adaptedCompaction}}}}, after...))
		if raw, _ := json.Marshal(b); strings.Contains(string(raw), piCompacted) {
			t.Fatal("the compaction sentence is still in the body")
		}
		c := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "messages": []any{obj{"role": "user", "content": piCompaction}, obj{"role": "user", "content": []any{obj{"type": "text", "text": piBranch}}}}})
		same(t, "a string content and a branch", c["messages"], []any{obj{"role": "user", "content": adaptedCompaction}, obj{"role": "user", "content": []any{obj{"type": "text", "text": adaptedBranch}}}})
	})

	t.Run("pi's compaction summary opens without its fixed sentence on chat completions and Responses", func(t *testing.T) {
		b := send(t, chat, obj{"model": "deepseek-v4.1-flash", "messages": []any{obj{"role": "developer", "content": piPrompt}, obj{"role": "user", "content": []any{obj{"type": "text", "text": piCompaction}}}, obj{"role": "user", "content": "go on"}}})
		same(t, "chat", b["messages"].([]any)[1:], []any{obj{"role": "developer", "content": piAdapted}, obj{"role": "user", "content": []any{obj{"type": "text", "text": adaptedCompaction}}}, obj{"role": "user", "content": "go on"}})
		b = send(t, chat, obj{"model": "kimi-k3", "messages": []any{obj{"role": "user", "content": piBranch}}})
		same(t, "a branch", at(b, "messages", 1), obj{"role": "user", "content": adaptedBranch})
		b = send(t, responses, obj{"model": "gpt-6.1", "instructions": piPrompt, "input": []any{obj{"type": "message", "role": "user", "content": []any{obj{"type": "input_text", "text": piCompaction}}}, obj{"role": "user", "content": "go on"}}})
		same(t, "Responses", b["input"], []any{obj{"type": "message", "role": "user", "content": []any{obj{"type": "input_text", "text": adaptedCompaction}}}, obj{"role": "user", "content": "go on"}})
	})

	t.Run("the compaction sentence is escaped wherever it appears, a fragment kept", func(t *testing.T) {
		for _, text := range []string{
			"Why does pi write: " + piCompacted,
			piCompacted + " What does that mean?",
			piCompacted + "\n\n<summary>\n" + piSummaryText,
		} {
			escaped := strings.Replace(text, piCompacted, escapedCompacted, 1)
			b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "messages": []any{obj{"role": "user", "content": []any{obj{"type": "text", "text": text}}}}})
			same(t, "Messages", str(b, "messages", 0, "content", 0, "text"), escaped)
			c := send(t, chat, obj{"model": "kimi-k3", "messages": []any{obj{"role": "user", "content": text}}})
			same(t, "chat", str(c, "messages", 1, "content"), escaped)
		}
		b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "messages": []any{obj{"role": "user", "content": "hi"}, obj{"role": "assistant", "content": []any{obj{"type": "text", "text": piCompaction}}}, obj{"role": "user", "content": "ok"}}})
		same(t, "an assistant", str(b, "messages", 1, "content", 0, "text"), strings.Replace(piCompaction, piCompacted, escapedCompacted, 1))
		fragment := "The conversation history before this point"
		c := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "messages": []any{obj{"role": "user", "content": fragment}}})
		same(t, "a fragment", str(c, "messages", 0, "content"), fragment)
	})

	t.Run("pi's opening and compaction sentence in tool output are quoted, the record kept", func(t *testing.T) {
		outputs := []any{
			obj{"type": "tool_result", "tool_use_id": "bash_1", "content": piSnapshot, "is_error": false, "cache_control": obj{"type": "ephemeral"}},
			obj{"type": "tool_result", "tool_use_id": "read_1", "content": []any{obj{"type": "text", "text": piRecord, "cache_control": obj{"type": "ephemeral"}}, obj{"type": "image", "source": obj{"type": "base64", "media_type": "image/png", "data": "image-data"}}}, "is_error": true},
		}
		b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "system": []any{obj{"type": "text", "text": piPrompt}}, "messages": []any{obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "bash_1", "name": "Bash", "input": obj{"command": "node scripts/model-snapshot.mjs"}}}}, obj{"role": "user", "content": outputs}}})
		first := str(b, "messages", 1, "content", 0, "content")
		same(t, "the first output", quoted(t, first), piSnapshot)
		if strings.Contains(first, piOpeningSentence) {
			t.Fatal("the opening is still in the first output")
		}
		withFirst := obj{}
		for k, v := range outputs[0].(obj) {
			withFirst[k] = v
		}
		withFirst["content"] = first
		same(t, "the first result's other fields", at(b, "messages", 1, "content", 0), withFirst)
		second := str(b, "messages", 1, "content", 1, "content", 0, "text")
		same(t, "the second output", quoted(t, second), piRecord)
		if strings.Contains(second, piCompacted) {
			t.Fatal("the compaction sentence is still in the second output")
		}
		same(t, "the second result's image", at(b, "messages", 1, "content", 1, "content", 1), at(outputs[1], "content", 1))
		same(t, "the second result's flag", at(b, "messages", 1, "content", 1, "is_error"), true)
	})

	t.Run("pi's opening in chat and Responses tool output is quoted too", func(t *testing.T) {
		b := send(t, chat, obj{"model": "deepseek-v4.1-flash", "messages": []any{obj{"role": "assistant", "content": "called bash", "tool_calls": []any{obj{"id": "c1", "type": "function", "function": obj{"name": "bash", "arguments": "{}"}}}}, obj{"role": "tool", "tool_call_id": "c1", "content": piSnapshot}}})
		out := str(b, "messages", 2, "content")
		same(t, "chat", quoted(t, out), piSnapshot)
		b = send(t, responses, obj{"model": "gpt-6.1", "instructions": piPrompt, "input": []any{obj{"type": "function_call", "call_id": "c1", "name": "bash", "arguments": "{}"}, obj{"type": "function_call_output", "call_id": "c1", "output": piSnapshot}}})
		out = str(b, "input", 1, "output")
		same(t, "Responses", quoted(t, out), piSnapshot)
		if strings.Contains(out, piOpeningSentence) {
			t.Fatal("the opening is still in the output")
		}
	})

	t.Run("pi's opening is escaped in user text, a fragment in tool output stays", func(t *testing.T) {
		for _, text := range []string{"Why does pi say: " + piOpeningSentence, escapedOpening, piPartial} {
			escaped := strings.Replace(text, piOpeningSentence, escapedOpening, 1)
			b := send(t, messages, obj{"model": "claude-opus-5-5", "max_tokens": 100, "messages": []any{obj{"role": "user", "content": []any{obj{"type": "text", "text": text}, obj{"type": "tool_result", "tool_use_id": "plain", "content": piPartial}}}}})
			same(t, "Messages text", str(b, "messages", 0, "content", 0, "text"), escaped)
			same(t, "Messages tool output", str(b, "messages", 0, "content", 1, "content"), piPartial)
			c := send(t, chat, obj{"model": "kimi-k3", "messages": []any{obj{"role": "user", "content": text}, obj{"role": "tool", "tool_call_id": "c1", "content": piPartial}}})
			same(t, "chat text", str(c, "messages", 1, "content"), escaped)
			same(t, "chat tool output", str(c, "messages", 2, "content"), piPartial)
		}
	})
}
