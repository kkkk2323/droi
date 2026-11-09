package factory

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const (
	chat      = "/api/llm/o/v1/chat/completions"
	responses = "/api/llm/o/v1/responses"
	messages  = "/api/llm/a/v1/messages"
	count     = "/api/llm/a/v1/messages/count_tokens"
	gemini    = "/api/llm/g/v1/models/gemini-3-pro:streamGenerateContent"
)

// Cases are the reference's own droid cases (droid.test.mjs) on the routes
// Droi sends, plus what the Droi Kernel's prompts look like. "«line»" stands
// for DroidLine: a want of "" says the body goes as it came.
var cases = []struct {
	name string
	path string
	in   string
	want string
}{
	{
		name: "chat: the first system message gets the line first",
		path: chat,
		in:   `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"system","content":"You are Codex, a coding agent."},{"role":"user","content":"hi"}],"stream":true,"temperature":0.7}`,
		want: `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"system","content":"«line»\nYou are Codex, a coding agent."},{"role":"user","content":"hi"}],"stream":true,"temperature":0.7}`,
	},
	{
		name: "chat: the Kernel's own prompt, which Factory refused",
		path: chat,
		in:   `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"system","content":"<environment>\nYou are running as the Kernel of Droi, on macOS.\n</environment>"},{"role":"user","content":"hi"}]}`,
		want: `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"system","content":"«line»\n<environment>\nYou are running as the Kernel of Droi, on macOS.\n</environment>"},{"role":"user","content":"hi"}]}`,
	},
	{
		name: "chat: text-only blocks become the one string droid sends",
		path: chat,
		in:   `{"model":"kimi-k3","messages":[{"role":"system","content":[{"type":"text","text":"You are Claude Code."},{"type":"text","text":"Be brief."}]},{"role":"user","content":"hi"}]}`,
		want: `{"model":"kimi-k3","messages":[{"role":"system","content":"«line»\nYou are Claude Code.\nBe brief."},{"role":"user","content":"hi"}]}`,
	},
	{
		name: "chat: a part that is not text keeps the blocks, line before them",
		path: chat,
		in:   `{"model":"glm-5.3-flash","messages":[{"role":"system","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA"}},{"type":"text","text":"what is this"}]}]}`,
		want: `{"model":"glm-5.3-flash","messages":[{"role":"system","content":[{"type":"text","text":"«line»"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA"}},{"type":"text","text":"what is this"}]}]}`,
	},
	{
		name: "chat: no system message gets the line alone",
		path: chat,
		in:   `{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"glm-5.3","messages":[{"role":"system","content":"«line»"},{"role":"user","content":"hi"}]}`,
	},
	{
		name: "chat: an empty system message is the line alone",
		path: chat,
		in:   `{"model":"glm-5.3","messages":[{"role":"system","content":"  \n "},{"role":"user","content":"hi"}]}`,
		want: `{"model":"glm-5.3","messages":[{"role":"system","content":"«line»"},{"role":"user","content":"hi"}]}`,
	},
	{
		name: "chat: droid's own request goes as it came",
		path: chat,
		in:   `{"model":"glm-5.3-flash","messages":[{"role":"system","content":"«line»\nYou work in the user's terminal."},{"role":"user","content":"hi"}],"stream":true,"n":1.0}`,
	},
	{
		name: "chat: a first block that is the line goes as it came",
		path: chat,
		in:   `{"model":"glm-5.3","messages":[{"role":"system","content":[{"type":"text","text":"«line»"},{"type":"text","text":"Be brief."}]}]}`,
	},
	{
		name: "chat: a body without messages goes as it came",
		path: chat,
		in:   `{"model":"glm-5.3","input":"hi"}`,
	},
	{
		name: "chat: messages that are not an array go as they came",
		path: chat,
		in:   `{"model":"glm-5.3","messages":"hi"}`,
	},
	{
		name: "chat: a null messages list goes as it came",
		path: chat,
		in:   `{"model":"glm-5.3","messages":null}`,
	},
	{
		name: "responses: instructions open with the line",
		path: responses,
		in:   `{"model":"gpt-5.5","instructions":"You are Codex, a coding agent.","input":[{"role":"user","content":"hi"}],"stream":true}`,
		want: `{"model":"gpt-5.5","instructions":"«line»\nYou are Codex, a coding agent.","input":[{"role":"user","content":"hi"}],"stream":true}`,
	},
	{
		name: "responses: no instructions is the line alone",
		path: responses,
		in:   `{"model":"grok-4.7","input":"hi"}`,
		want: `{"model":"grok-4.7","input":"hi","instructions":"«line»"}`,
	},
	{
		name: "responses: null instructions are the line alone",
		path: responses,
		in:   `{"model":"grok-4.7","instructions":null,"input":"hi"}`,
		want: `{"model":"grok-4.7","instructions":"«line»","input":"hi"}`,
	},
	{
		name: "responses: instructions of another shape go as they came",
		path: responses,
		in:   `{"model":"gpt-5.5","instructions":[1],"input":"hi"}`,
	},
	{
		name: "responses: droid's own request goes as it came",
		path: responses,
		in:   `{"model":"gpt-5.5","input":[],"store":false,"instructions":"«line»\nYou work in the user's terminal.","stream":true}`,
	},
	{
		name: "messages: a string system becomes blocks, the line first",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":"You are Claude Code.","messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"},{"type":"text","text":"You are Claude Code."}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "messages: blocks get the line first",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":[{"type":"text","text":"You are Claude Code."}],"messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"},{"type":"text","text":"You are Claude Code."}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "messages: the line moves to the front",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":[{"type":"text","text":"You are Claude Code."},{"type":"text","text":"«line»"}],"messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"},{"type":"text","text":"You are Claude Code."}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "messages: a duplicate line alone goes",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"},{"type":"text","text":"«line»"},{"type":"text","text":"Be brief."}],"messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"},{"type":"text","text":"Be brief."}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "messages: an empty system block goes",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":[{"type":"text","text":"  "},{"type":"text","text":"«line»"}],"messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "messages: no system is the line alone",
		path: messages,
		in:   `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "messages: droid's own request goes as it came",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "messages: a block of another shape goes as they came",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":[{"type":"text","text":7}],"messages":[]}`,
	},
	{
		name: "messages: system of another type goes as it came",
		path: messages,
		in:   `{"model":"claude-opus-5-5","system":7,"messages":[]}`,
	},
	{
		name: "count_tokens takes the same shape",
		path: count,
		in:   `{"model":"claude-opus-5-5","system":[{"type":"text","text":"You are Claude Code."}],"messages":[{"role":"user","content":"hi"}]}`,
		want: `{"model":"claude-opus-5-5","system":[{"type":"text","text":"«line»"},{"type":"text","text":"You are Claude Code."}],"messages":[{"role":"user","content":"hi"}]}`,
	},
	{
		name: "gemini: the system instruction opens with the line, its role goes, the model is in the body",
		path: gemini,
		in:   `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"systemInstruction":{"role":"system","parts":[{"text":"You are a coding agent."}]}}`,
		want: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"model":"gemini-3-pro","systemInstruction":{"parts":[{"text":"«line»\nYou are a coding agent."}]}}`,
	},
	{
		name: "gemini: no system instruction is the line alone",
		path: gemini,
		in:   `{"contents":[],"stream":true}`,
		want: `{"contents":[],"model":"gemini-3-pro","systemInstruction":{"parts":[{"text":"«line»"}]}}`,
	},
	{
		name: "gemini: a string system instruction becomes one text part",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":"You are a coding agent."}`,
		want: `{"contents":[],"model":"gemini-3-pro","systemInstruction":{"parts":[{"text":"«line»\nYou are a coding agent."}]}}`,
	},
	{
		name: "gemini: an instruction that opens with the line keeps its parts",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":{"parts":[{"text":"«line»"},{"text":"Be brief."}]}}`,
		want: `{"contents":[],"model":"gemini-3-pro","systemInstruction":{"parts":[{"text":"«line»"},{"text":"Be brief."}]}}`,
	},
	{
		name: "gemini: the role goes even when the line is already first",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":{"role":"system","parts":[{"text":"«line»"}]}}`,
		want: `{"contents":[],"model":"gemini-3-pro","systemInstruction":{"parts":[{"text":"«line»"}]}}`,
	},
	{
		name: "gemini: parts of another shape are none",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":{"parts":"hi"}}`,
		want: `{"contents":[],"model":"gemini-3-pro","systemInstruction":{"parts":[{"text":"«line»"}]}}`,
	},
	{
		name: "gemini: droid's own request, on Factory's route, goes as it came",
		path: "/api/llm/g/v1/generate",
		in:   `{"model":"gemini-3-pro","contents":[],"systemInstruction":{"parts":[{"text":"«line»\nYou work here."}]}}`,
	},
	{
		name: "a route with no shape goes as it came",
		path: "/api/llm/o/v1/models",
		in:   `{"model":"glm-5.3"}`,
	},
	{
		name: "a body that is not JSON goes as it came",
		path: responses,
		in:   `not json`,
	},
	{
		name: "a body that is a JSON array goes as it came",
		path: chat,
		in:   `[{"role":"user","content":"hi"}]`,
	},
}

func TestRequestBody(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.ReplaceAll(tc.in, "«line»", DroidLine)
			_, got, changed := Request(tc.path, []byte(in))
			if tc.want == "" {
				if changed || string(got) != in {
					t.Fatalf("changed a body that should go as it came:\n got %s\nwant %s", got, in)
				}
				return
			}
			if !changed {
				t.Fatalf("left the body as it came: %s", got)
			}
			if want := decodeBody(t, strings.ReplaceAll(tc.want, "«line»", DroidLine)); !reflect.DeepEqual(decodeBody(t, string(got)), want) {
				t.Fatalf("got\n %s\nwant\n %s", got, tc.want)
			}
		})
	}
}

// A body that needs no change comes back as the very bytes it came with, so
// droid's own requests reach Factory byte for byte, a large integer, a
// float's own spelling and an escape included.
func TestRequestReturnsTheSameSlice(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"model":"glm-5.3","messages":[{"role":"system","content":"` + DroidLine + `"}]}`),
		[]byte(`{"model":"claude-opus-5-5","system":[{"type":"text","text":"` + DroidLine + `"}],"messages":[{"role":"user","content":"\u003cb\u003e"}],"max_tokens":9007199254740993,"temperature":1.0}`),
	} {
		_, got, changed := Request(messagesOrChat(body), body)
		if changed || &got[0] != &body[0] {
			t.Fatalf("changed=%v, same slice=%v: %s", changed, &got[0] == &body[0], body)
		}
	}
}

func messagesOrChat(body []byte) string {
	if strings.Contains(string(body), "claude") {
		return messages
	}
	return chat
}

// Request on a Gemini request in Google's shape: Factory's route, the query
// the caller drops, the model in the body as Google's clients escape it.
func TestAGeminiRequestGoesToFactorysRoute(t *testing.T) {
	for _, tc := range []struct{ in, path, model string }{
		{"/api/llm/g/v1/models/gemini-4-argon:streamGenerateContent", "/api/llm/g/v1/generate", "gemini-4-argon"},
		{"/api/llm/g/v1/models/gemini-3.5-flash:generateContent", "/api/llm/g/v1/generate", "gemini-3.5-flash"},
		{"/api/llm/g/v1/models/gemini%2D3:StreamGenerateContent", "/api/llm/g/v1/generate", "gemini-3"},
	} {
		path, body, changed := Request(tc.in, []byte(`{"contents":[]}`))
		if !changed || path != tc.path {
			t.Fatalf("%s: went to %s, changed %v", tc.in, path, changed)
		}
		if got := decodeBody(t, string(body)).(map[string]any)["model"]; got != tc.model {
			t.Fatalf("%s: the model in the body is %v", tc.in, got)
		}
	}
	// Only Gemini's route: a path elsewhere that ends so is left alone.
	if path, _, _ := Request("/api/llm/o/v1/models/x:generateContent", []byte(`{}`)); path != "/api/llm/o/v1/models/x:generateContent" {
		t.Fatalf("an OpenAI path went to %s", path)
	}
}

func decodeBody(t *testing.T, body string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, body)
	}
	return v
}
