package factory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The phrase lists are upstream's client-phrases.json, entry for entry and in order: a change upstream fails here.
func TestThePhrasesAreUpstreams(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "client-phrases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var upstream struct {
		Anywhere []string `json:"anywhere"`
		ToolOnly []string `json:"toolOnly"`
	}
	if err := json.Unmarshal(raw, &upstream); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(upstream.Anywhere, anywherePhrases) || !reflect.DeepEqual(upstream.ToolOnly, toolOnlyPhrases) {
		t.Fatalf("phrases.go differs from client-phrases.json:\n anywhere %q\n toolOnly %q", upstream.Anywhere, upstream.ToolOnly)
	}
	// Longest first, so no phrase is escaped before a longer one that holds it.
	for i := 1; i < len(escapeInTool); i++ {
		if len(escapeInTool[i]) > len(escapeInTool[i-1]) {
			t.Fatalf("escapeInTool is not longest first at %q", escapeInTool[i])
		}
	}
}

// No source of this package holds a refused phrase as it is: a session on Factory that reads one is not refused.
func TestNoSourceHoldsARefusedPhrase(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range refusedPhrases {
			if strings.Contains(string(src), phrase) {
				t.Errorf("%s holds %q", file, escapeFirst(phrase))
			}
		}
	}
}

// request.test.mjs #26–#28: tool output that quotes a refused phrase goes as a JSON string the model can decode,
// on Messages and its token count alike; ordinary output, and a phrase outside tool output, stay as they came.
func TestToolOutput(t *testing.T) {
	identity := toolOnlyPhrases[0]
	t.Run("losslessly quotes fixed client metadata in tool output on inference and counting", func(t *testing.T) {
		source := "576: \"" + identity + "\"\n583: /^<system-reminder>\\n" + toolOnlyPhrases[3] + "/\nQuotes: \"\\\\\"; Unicode: 中文 😀; literal escape: \\u0059 <b> & co"
		outputs := []any{
			obj{"type": "tool_result", "tool_use_id": "bash_1", "content": source, "is_error": false, "cache_control": obj{"type": "ephemeral"}},
			obj{"type": "tool_result", "tool_use_id": "read_1", "content": []any{obj{"type": "text", "text": source, "cache_control": obj{"type": "ephemeral"}}, obj{"type": "image", "source": obj{"type": "base64", "media_type": "image/png", "data": "image-data"}}}, "is_error": true},
		}
		for _, path := range []string{messages, count} {
			request := obj{"system": DroidLine, "messages": []any{obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "bash_1", "name": "Bash", "input": obj{"command": "cat index.mjs"}}}}, obj{"role": "user", "content": outputs}}, "tools": []any{obj{"name": "Bash", "input_schema": obj{"type": "object"}}}, "stream": true}
			raw, _ := json.Marshal(request)
			_, once, _ := Request(path, raw)
			var sent obj
			_ = json.Unmarshal(once, &sent)
			encoded := str(sent, "messages", 1, "content", 0, "content")
			same(t, "decoded", quoted(t, encoded), source)
			for _, gone := range []string{"\u0059ou are Claude Code", "\u0059ou have been invoked", "system-reminder"} {
				if strings.Contains(encoded, gone) {
					t.Fatalf("%s still holds %q", path, escapeFirst(gone))
				}
			}
			if !strings.Contains(encoded, "<b> & co") {
				t.Fatalf("the quoted output is escaped for HTML: %s", encoded)
			}
			same(t, "the second result's text", str(sent, "messages", 1, "content", 1, "content", 0, "text"), encoded)
			same(t, "the tool call", at(sent, "messages", 0), at(request, "messages", 0))
			same(t, "the tools", sent["tools"], request["tools"])
			if _, twice, changed := Request(path, once); changed || string(twice) != string(once) {
				t.Fatalf("%s: a second pass changed the body", path)
			}
		}
	})

	t.Run("losslessly quotes compaction openings inside historical JSONL tool output (#1132)", func(t *testing.T) {
		record, _ := json.Marshal(obj{"role": "assistant", "content": compactOpening + " The summary below covers the earlier portion of the conversation.\n\nSummary:\nKeep 中文 😀, quotes and literal \\u0054 escapes.\n" + compactOpening})
		source := "Historical session output:\n{\"role\":\"user\",\"content\":\"Keep this record\"}\n" + string(record) + "\nEnd of file."
		for _, path := range []string{messages, count} {
			request := obj{"system": DroidLine, "messages": []any{obj{"role": "user", "content": []any{obj{"type": "tool_result", "tool_use_id": "bash_1", "content": source}}}}}
			raw, _ := json.Marshal(request)
			_, once, _ := Request(path, raw)
			var sent obj
			_ = json.Unmarshal(once, &sent)
			encoded := str(sent, "messages", 0, "content", 0, "content")
			if strings.Contains(encoded, compactOpening) {
				t.Fatal("the opening is still in the output")
			}
			same(t, "decoded", quoted(t, encoded), source)
			if _, twice, changed := Request(path, once); changed || string(twice) != string(once) {
				t.Fatalf("%s: a second pass changed the body", path)
			}
		}
		// The same text outside tool output, and a fragment in it, go as they came.
		plain := "Quoted fragment: " + compactOpening[:len(compactOpening)-1]
		body, _ := json.Marshal(obj{"system": DroidLine, "messages": []any{
			obj{"role": "user", "content": []any{obj{"type": "text", "text": source}, obj{"type": "tool_result", "tool_use_id": "plain", "content": plain}}},
			obj{"role": "assistant", "content": []any{obj{"type": "text", "text": source}}},
		}})
		if _, out, changed := Request(messages, body); changed || &out[0] != &body[0] {
			t.Fatal("a body with nothing to quote changed")
		}
	})

	t.Run("leaves ordinary tool results and fixed phrases outside tool-result content untouched", func(t *testing.T) {
		body, _ := json.Marshal(obj{"system": DroidLine, "messages": []any{
			obj{"role": "user", "content": []any{obj{"type": "text", "text": identity}, obj{"type": "tool_result", "tool_use_id": "plain", "content": "Claude Code docs; \u0059ou are Claude Code is an incomplete fragment."}, obj{"type": "tool_result", "tool_use_id": "object", "content": obj{"text": identity}}}},
			obj{"role": "assistant", "content": []any{obj{"type": "text", "text": identity}}},
		}})
		if _, out, changed := Request(messages, body); changed || &out[0] != &body[0] {
			t.Fatalf("an ordinary body changed: %s", out)
		}
	})
}

// sanitizeBody: run twice is run once; a number keeps its spelling; a phrase a JSON escape spells is found too.
func TestSanitizeBody(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"` + piCompacted + `"}],"max_tokens":9007199254740993,"temperature":1.0}`)
	once, changed := sanitizeBody(body, anywherePhrases)
	if !changed {
		t.Fatal("the phrase was not escaped")
	}
	for _, kept := range []string{`9007199254740993`, `1.0`} {
		if !strings.Contains(string(once), kept) {
			t.Fatalf("%s lost its spelling: %s", kept, once)
		}
	}
	if twice, again := sanitizeBody(once, anywherePhrases); again || string(twice) != string(once) {
		t.Fatal("a second pass changed the body")
	}
	spelled := []byte(`{"content":"\u0054` + piCompacted[1:] + `"}`)
	if out, changed := sanitizeBody(spelled, anywherePhrases); !changed || strings.Contains(string(out), piCompacted) {
		t.Fatalf("a phrase spelled with an escape went through: %s", out)
	}
	for _, other := range []string{`not json`, `"` + piCompacted + `"`, `{"a":1} {"b":2}`} {
		if out, changed := sanitizeBody([]byte(other), anywherePhrases); changed || string(out) != other {
			t.Fatalf("%q changed", other)
		}
	}
}
