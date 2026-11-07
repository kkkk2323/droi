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
		name: "gemini: the system instruction opens with the line, its role goes",
		path: gemini,
		in:   `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"systemInstruction":{"role":"system","parts":[{"text":"You are a coding agent."}]}}`,
		want: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"systemInstruction":{"parts":[{"text":"«line»\nYou are a coding agent."}]}}`,
	},
	{
		name: "gemini: no system instruction is the line alone",
		path: gemini,
		in:   `{"contents":[]}`,
		want: `{"contents":[],"systemInstruction":{"parts":[{"text":"«line»"}]}}`,
	},
	{
		name: "gemini: a string system instruction becomes one text part",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":"You are a coding agent."}`,
		want: `{"contents":[],"systemInstruction":{"parts":[{"text":"«line»\nYou are a coding agent."}]}}`,
	},
	{
		name: "gemini: an instruction that opens with the line goes as it came",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":{"parts":[{"text":"«line»"},{"text":"Be brief."}]}}`,
	},
	{
		name: "gemini: the role goes even when the line is already first",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":{"role":"system","parts":[{"text":"«line»"}]}}`,
		want: `{"contents":[],"systemInstruction":{"parts":[{"text":"«line»"}]}}`,
	},
	{
		name: "gemini: a system instruction of another shape goes as it came",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":{"parts":"hi"}}`,
	}, {
		name: "gemini: droid's own request goes as it came",
		path: gemini,
		in:   `{"contents":[],"systemInstruction":{"parts":[{"text":"«line»\nYou work here."}]}}`,
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

func TestDroidBody(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.ReplaceAll(tc.in, "«line»", DroidLine)
			got, changed := DroidBody(tc.path, []byte(in))
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
// droid's own requests reach Factory byte for byte.
func TestDroidBodyReturnsTheSameSlice(t *testing.T) {
	body := []byte(`{"model":"glm-5.3","messages":[{"role":"system","content":"` + DroidLine + `"}]}`)
	got, changed := DroidBody(chat, body)
	if changed || &got[0] != &body[0] {
		t.Fatalf("changed=%v, same slice=%v", changed, &got[0] == &body[0])
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
