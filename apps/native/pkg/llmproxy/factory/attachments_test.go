package factory

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// size.test.mjs (yetone/magpie#2082): a 413 is Factory's limit on a body's bytes.
func TestLeaveOutAttachments(t *testing.T) {
	leave := func(t *testing.T, path string, body any) (obj, int) {
		t.Helper()
		raw, _ := json.Marshal(body)
		out, n := LeaveOutAttachments(path, raw)
		if n == 0 {
			return nil, 0
		}
		var got obj
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		return got, n
	}
	png := func(n int) obj {
		return obj{"type": "image", "source": obj{"type": "base64", "media_type": "image/png", "data": strings.Repeat("A", n)}}
	}

	t.Run("Messages has its older images left out, tool results kept beside their calls", func(t *testing.T) {
		session := func() obj {
			return obj{"model": "claude-opus-5-5", "max_tokens": 1024, "stream": true, "messages": []any{
				obj{"role": "user", "content": []any{obj{"type": "text", "text": "look"}, png(30)}},
				obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "t1", "name": "view_image", "input": obj{"path": "/a.png"}}}},
				obj{"role": "user", "content": []any{obj{"type": "tool_result", "tool_use_id": "t1", "content": []any{png(30)}}}},
				obj{"role": "assistant", "content": []any{obj{"type": "tool_use", "id": "t2", "name": "view_image", "input": obj{"path": "/b.png"}}}},
				obj{"role": "user", "content": []any{obj{"type": "tool_result", "tool_use_id": "t2", "content": []any{obj{"type": "text", "text": "b.png"}, mergeObj(png(30), obj{"cache_control": obj{"type": "ephemeral"}})}}}},
			}}
		}
		got, n := leave(t, messages, session())
		if n != 2 {
			t.Fatalf("left out %d", n)
		}
		want := session()
		msgs := want["messages"].([]any)
		msgs[0].(obj)["content"].([]any)[1] = obj{"type": "text", "text": LeftOut}
		msgs[2].(obj)["content"].([]any)[0].(obj)["content"].([]any)[0] = obj{"type": "text", "text": LeftOut}
		same(t, "messages", got["messages"], want["messages"])
		same(t, "the rest", got["max_tokens"], 1024)
		if _, n := leave(t, count, session()); n != 0 {
			t.Fatal("count_tokens has attachments to leave out")
		}
	})

	t.Run("Responses, chat completions and Gemini have their older images left out in their own words", func(t *testing.T) {
		img := obj{"type": "input_image", "image_url": "data:image/png;base64,AAAA"}
		got, n := leave(t, responses, obj{"input": []any{
			obj{"role": "user", "content": []any{obj{"type": "input_text", "text": "a"}, img}},
			obj{"type": "function_call", "call_id": "c1", "name": "view_image", "arguments": "{}"},
			obj{"type": "function_call_output", "call_id": "c1", "output": []any{img}},
			obj{"role": "user", "content": []any{img}},
		}})
		if n != 2 {
			t.Fatalf("left out %d", n)
		}
		in := got["input"].([]any)
		same(t, "first", in[0].(obj)["content"].([]any)[1], obj{"type": "input_text", "text": LeftOut})
		same(t, "output", in[2], obj{"type": "function_call_output", "call_id": "c1", "output": []any{obj{"type": "input_text", "text": LeftOut}}})
		same(t, "newest", in[3].(obj)["content"].([]any)[0], img)

		url := obj{"type": "image_url", "image_url": obj{"url": "data:image/png;base64,AAAA"}}
		got, _ = leave(t, chat, obj{"messages": []any{obj{"role": "user", "content": []any{url, obj{"type": "text", "text": "x"}}}, obj{"role": "user", "content": []any{url}}}})
		ms := got["messages"].([]any)
		same(t, "chat", ms[0].(obj)["content"], []any{obj{"type": "text", "text": LeftOut}, obj{"type": "text", "text": "x"}})
		same(t, "chat newest", ms[1].(obj)["content"].([]any)[0], url)

		inline := obj{"inlineData": obj{"mimeType": "image/png", "data": "AAAA"}}
		gem := obj{"contents": []any{
			obj{"role": "user", "parts": []any{inline}},
			obj{"role": "model", "parts": []any{obj{"functionCall": obj{"name": "view_image", "args": obj{}}}}},
			obj{"role": "user", "parts": []any{obj{"functionResponse": obj{"name": "view_image", "response": obj{}, "parts": []any{inline}}}}},
			obj{"role": "user", "parts": []any{inline}},
		}}
		for _, path := range []string{gemini, "/api/llm/g/v1/generate"} {
			got, _ = leave(t, path, gem)
			cs := got["contents"].([]any)
			same(t, "gemini", cs[0].(obj)["parts"], []any{obj{"text": LeftOut}})
			// a function response's parts take inline data alone: the image goes, the response stays
			same(t, "function response", cs[2].(obj)["parts"], []any{obj{"functionResponse": obj{"name": "view_image", "response": obj{}, "parts": []any{}}}})
			same(t, "gemini newest", cs[3].(obj)["parts"], []any{inline})
		}

		// one image, none to leave out; a body not JSON, or a route with none
		if _, n := leave(t, chat, obj{"messages": []any{obj{"role": "user", "content": []any{url}}}}); n != 0 {
			t.Fatal("one image was left out")
		}
		if _, n := LeaveOutAttachments(messages, []byte("not json")); n != 0 {
			t.Fatal("not json")
		}
	})

	t.Run("an older image's cache breakpoint stays on the text in its place", func(t *testing.T) {
		cached := mergeObj(png(3), obj{"cache_control": obj{"type": "ephemeral"}})
		got, n := leave(t, messages, obj{"messages": []any{obj{"role": "user", "content": []any{cached, png(3)}}}})
		if n != 1 {
			t.Fatalf("left out %d", n)
		}
		same(t, "first", got["messages"].([]any)[0].(obj)["content"].([]any)[0], obj{"type": "text", "text": LeftOut, "cache_control": obj{"type": "ephemeral"}})
	})

	t.Run("numbers keep their spelling", func(t *testing.T) {
		raw := `{"max_tokens":12345678901234567890,"messages":[{"role":"user","content":[{"type":"image","source":{}},{"type":"image","source":{}}]}]}`
		out, n := LeaveOutAttachments(messages, []byte(raw))
		if n != 1 || !strings.Contains(string(out), `"max_tokens":12345678901234567890`) {
			t.Fatalf("%d %s", n, out)
		}
	})

	t.Run("a 413 says it is Factory's byte limit, what was left out, and what to do", func(t *testing.T) {
		msg := TooLarge([]byte("Request Entity Too Large"), 2)
		for _, want := range []string{"(413: Request Entity Too Large)", "limits the size of a request's body in bytes", "2 older images or files left out", TooLargeHint} {
			if !strings.Contains(msg, want) {
				t.Errorf("%q lacks %q", msg, want)
			}
		}
		if strings.Contains(strings.ToLower(msg), "context window") || strings.Contains(strings.ToLower(msg), "exceed") {
			t.Errorf("%q speaks of the context", msg)
		}
		if msg := TooLarge([]byte("<html>"), 0); strings.Contains(msg, "left out") || !strings.Contains(msg, "(413)") {
			t.Errorf("%q", msg)
		}
	})
}

func mergeObj(a, b obj) obj {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// content-policy.test.mjs's last case (its explain half is the proxy's test), and the words a refusal is sent back in.
func TestRefusals(t *testing.T) {
	policy := func(where string) []byte {
		if where == "top" {
			return []byte(`{"code":"content_policy_rejected","detail":"This request was blocked by Factory's usage policy check."}`)
		}
		return []byte(`{"error":{"code":"content_policy_rejected","message":"This request was blocked by Factory's usage policy check."}}`)
	}
	upstreamErr := http.Header{"X-Factory-Upstream-Error": {"1"}}
	t.Run("the code is Factory's only without an upstream error header", func(t *testing.T) {
		if !ContentRefused(403, http.Header{}, policy("top")) || ContentRefused(403, upstreamErr, policy("top")) ||
			ContentRefused(400, http.Header{}, policy("top")) || ContentRefused(403, http.Header{}, []byte("Forbidden")) ||
			ContentRefused(403, http.Header{}, []byte(`{"error":{"code":"permission_denied"}}`)) {
			t.Fatal("ContentRefused")
		}
	})

	t.Run("Factory's words, and the ids to ask it about them", func(t *testing.T) {
		same(t, "detail", VendorError(policy("top"), "403 Forbidden"), "This request was blocked by Factory's usage policy check.")
		same(t, "error.message", VendorError(policy("error"), "403 Forbidden"), "This request was blocked by Factory's usage policy check.")
		same(t, "plain", VendorError([]byte("  Forbidden \n now"), "403 Forbidden"), "403 Forbidden: Forbidden now")
		same(t, "html", VendorError([]byte("<html>"), "403 Forbidden"), "403 Forbidden")
		same(t, "long", VendorError([]byte(strings.Repeat("é", 301)), "403"), "403: "+strings.Repeat("é", 300)+"…")
		same(t, "errors", VendorError([]byte(`{"errors":[{"message":"first"}]}`), "x"), "first")
		id, said := UpstreamIDs(http.Header{"X-Request-Id": {"req_1"}}, policy("error"))
		same(t, "id", id, "req_1")
		same(t, "said", said, " (upstream request id req_1, code content_policy_rejected)")
		id, _ = UpstreamIDs(http.Header{"X-Request-Id": {"req_1"}}, []byte(`{"request_id":"req_body"}`))
		same(t, "the body's id first", id, "req_body")
		if _, said := UpstreamIDs(http.Header{}, []byte("Forbidden")); said != "" {
			t.Fatalf("%q", said)
		}
	})

	t.Run("an error is worded as its route's API words one", func(t *testing.T) {
		var a obj
		_ = json.Unmarshal(ErrorBody(messages, "invalid_request_error", "m", "req_1", "content_policy_rejected"), &a)
		same(t, "anthropic", a, obj{"type": "error", "error": obj{"type": "invalid_request_error", "message": "m", "code": "content_policy_rejected"}, "request_id": "req_1"})
		var o obj
		_ = json.Unmarshal(ErrorBody(chat, "invalid_request_error", "m", "req_1", ""), &o)
		same(t, "openai", o, obj{"error": obj{"message": "m", "type": "invalid_request_error", "code": nil}})
	})
}
