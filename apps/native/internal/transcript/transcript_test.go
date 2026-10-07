package transcript

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

var seq int

// msg builds a message from a role, its content as JSON and extra fields.
func msg(role, content string, extra ...func(*protocol.FactoryDroidMessage)) protocol.FactoryDroidMessage {
	seq++
	m := protocol.FactoryDroidMessage{ID: fmt.Sprintf("%s-%d", role, seq), Role: protocol.MessageRole(role), CreatedAt: 1, UpdatedAt: 1}
	if err := json.Unmarshal([]byte(content), &m.Content); err != nil {
		panic(err)
	}
	for _, f := range extra {
		f(&m)
	}
	return m
}

func at(ms float64) func(*protocol.FactoryDroidMessage) {
	return func(m *protocol.FactoryDroidMessage) { m.CreatedAt, m.UpdatedAt = ms, ms }
}

func kinds(e *Entry) []any {
	out := []any{}
	for _, b := range e.Blocks {
		switch b.Kind {
		case Text:
			out = append(out, "text")
		case Thinking:
			out = append(out, "thinking")
		case Picture:
			out = append(out, "image")
		case Tools:
			out = append(out, len(b.Calls))
		case Subagent:
			out = append(out, "subagent")
		}
	}
	return out
}

func roles(es []*Entry) []string {
	out := []string{}
	for _, e := range es {
		if e.User {
			out = append(out, "user")
		} else {
			out = append(out, "assistant")
		}
	}
	return out
}

func callIDs(cs []*ToolCall) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Use.ID)
	}
	return out
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestAttachesResults(t *testing.T) {
	es := Build([]protocol.FactoryDroidMessage{
		msg("user", `[{"type":"text","text":"run it"}]`),
		msg("assistant", `[{"type":"thinking","thinking":"plan","signature":"","durationMs":42},{"type":"tool_use","id":"call-1","name":"Execute","input":{"command":"ls"}}]`),
		msg("tool", `[{"type":"tool_result","toolUseId":"call-1","content":"a\nb"}]`),
		msg("assistant", `[{"type":"text","text":"done"}]`),
	})
	eq(t, roles(es), []string{"user", "assistant"})
	eq(t, kinds(es[1]), []any{"thinking", 1, "text"})
	eq(t, *es[1].Blocks[0].DurationMs, 42.0)
	eq(t, ResultText(es[1].Blocks[1].Calls[0].Result), "a\nb")
}

func TestImagesAndTask(t *testing.T) {
	es := Build([]protocol.FactoryDroidMessage{msg("user", `[{"type":"text","text":"see"},{"type":"image","source":{"type":"base64","mediaType":"image/png","data":"AAAA"}}]`)})
	eq(t, es[0].Blocks[1].Image, Image{"image/png", "AAAA"})

	es = Build([]protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"a","name":"Read","input":{}},{"type":"tool_use","id":"t","name":"Task","input":{"subagent_type":"explorer"}},{"type":"tool_use","id":"b","name":"Grep","input":{}}]`),
		msg("tool", `[{"type":"tool_result","toolUseId":"t","content":"report"}]`),
	})
	eq(t, kinds(es[0]), []any{1, "subagent", 1})
	eq(t, ResultText(es[0].Blocks[1].Call.Result), "report")
}

func TestScriptCallsNest(t *testing.T) {
	nested := func(id, name string) protocol.FactoryDroidMessage {
		return msg("assistant", `[{"type":"tool_use","id":"`+id+`","name":"`+name+`","input":{},"scriptExecution":{"runId":"run","outerToolUseId":"run"}}]`)
	}
	es := Build([]protocol.FactoryDroidMessage{
		msg("user", `[{"type":"text","text":"go"}]`),
		msg("assistant", `[{"type":"tool_use","id":"run","name":"Script","input":{"script":""}},{"type":"tool_use","id":"other","name":"ToolSearch","input":{}}]`),
		nested("run-1", "Read"),
		msg("tool", `[{"type":"tool_result","toolUseId":"run-1","content":"text"}]`),
		nested("run-3", "Execute"),
		msg("tool", `[{"type":"tool_result","toolUseId":"run","content":"{\"toolCallId\":\"run\",\"status\":\"running\"}"}]`),
		msg("assistant", `[{"type":"text","text":"Still going; waiting."}]`),
		msg("assistant", `[{"type":"tool_use","id":"wait","name":"WaitForScript","input":{"toolCallId":"run"}}]`),
		nested("run-5", "Execute"),
	})
	eq(t, roles(es), []string{"user", "assistant"})
	bs := es[1].Blocks
	eq(t, kinds(es[1]), []any{2, "text", 1})
	eq(t, callIDs(bs[0].Calls), []string{"run", "other"})
	eq(t, callIDs(bs[0].Calls[0].Nested), []string{"run-1", "run-3"})
	eq(t, ResultText(bs[0].Calls[0].Nested[0].Result), "text")
	eq(t, bs[0].Calls[1].Lifecycle, false)
	eq(t, callIDs(bs[2].Calls[0].Nested), []string{"run-5"})

	es = Build([]protocol.FactoryDroidMessage{nested("n", "Read")})
	eq(t, callIDs(es[0].Blocks[0].Calls), []string{"n"})
}

func TestMergesAndSkips(t *testing.T) {
	es := Build([]protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"a","name":"Read","input":{}},{"type":"tool_use","id":"b","name":"Grep","input":{}},{"type":"text","text":"found it"},{"type":"tool_use","id":"c","name":"Edit","input":{}}]`),
	})
	eq(t, kinds(es[0]), []any{2, "text", 1})

	es = Build([]protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"a","name":"Read","input":{}}]`, at(1)),
		msg("assistant", `[{"type":"tool_use","id":"b","name":"Grep","input":{}}]`, at(2)),
		msg("assistant", `[{"type":"text","text":"done"}]`, at(3)),
		msg("user", `[{"type":"text","text":"thanks"}]`),
		msg("assistant", `[{"type":"text","text":"welcome"}]`),
	})
	eq(t, roles(es), []string{"assistant", "user", "assistant"})
	eq(t, kinds(es[0]), []any{2, "text"})
	eq(t, es[0].CreatedAt, 3.0)

	f := false
	es = Build([]protocol.FactoryDroidMessage{
		msg("assistant", `[]`),
		msg("user", `[{"type":"text","text":"hidden"}]`, func(m *protocol.FactoryDroidMessage) { m.IsUserVisible = &f }),
		msg("assistant", `[{"type":"text","text":""}]`),
		msg("user", `[]`, func(m *protocol.FactoryDroidMessage) { m.HookEventName = "UserPromptSubmit" }),
	})
	eq(t, len(es), 0)

	es = Build([]protocol.FactoryDroidMessage{
		msg("assistant", `[{"type":"tool_use","id":"a","name":"Read","input":{}}]`),
		msg("user", `[]`),
		msg("assistant", `[{"type":"text","text":"done"}]`),
	})
	eq(t, roles(es), []string{"assistant"})
}

func TestTheDaemonsNoticesShowOnTheDroidsSide(t *testing.T) {
	userOnly := func(m *protocol.FactoryDroidMessage) { m.Visibility = protocol.MessageVisibilityUserOnly }
	notice := `[{"type":"text","text":"You've reached your usage limit."}]`
	// Live, the Daemon sends a notice as a system message; a loaded Session
	// has it as a user message. Both are only for the user to read.
	for _, role := range []string{"system", "user"} {
		es := Build([]protocol.FactoryDroidMessage{
			msg("user", `[{"type":"text","text":"hi"}]`),
			msg("user", `[]`, userOnly, func(m *protocol.FactoryDroidMessage) { m.HookEventName = "UserPromptSubmit" }),
			msg(role, notice, userOnly),
			msg(role, notice, userOnly),
		})
		eq(t, roles(es), []string{"user", "assistant"})
		eq(t, kinds(es[1]), []any{"text", "text"})
		eq(t, es[1].Blocks[0].Text, "You've reached your usage limit.")
		if _, ok := TurnEnds(es, false)[es[1].ID]; !ok {
			t.Fatalf("%s notice: the turn does not end", role)
		}
	}

	es := Build([]protocol.FactoryDroidMessage{msg("system", notice)})
	eq(t, len(es), 0)
}

func TestResultTextAndImages(t *testing.T) {
	r := &protocol.ToolResult{Content: json.RawMessage(`[{"type":"text","text":"Image file: shot.png"},{"type":"image","source":{"type":"base64","mediaType":"image/png","data":"AAAA"}},{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]`)}
	eq(t, ResultText(r), "Image file: shot.png")
	eq(t, ResultImages(r), []Image{{"image/png", "AAAA"}})
	eq(t, len(ResultImages(&protocol.ToolResult{Content: json.RawMessage(`"x"`)})), 0)
	eq(t, len(ResultImages(nil)), 0)
	eq(t, ResultText(&protocol.ToolResult{Content: json.RawMessage(`[{"type":"text","text":"hi"}]`)}), "hi")
}

func TestTurnEnds(t *testing.T) {
	text := func(s string) string { return `[{"type":"text","text":"` + s + `"}]` }
	tool := func(id string) string { return `[{"type":"tool_use","id":"` + id + `","name":"Read","input":{}}]` }
	es := Build([]protocol.FactoryDroidMessage{
		msg("user", text("go"), at(1000)), msg("assistant", text("done"), at(5000)),
		msg("user", text("thanks"), at(9000)), msg("assistant", text("welcome"), at(9500)),
	})
	ends := TurnEnds(es, true)
	eq(t, ends[es[1].ID], TurnEnd{5000, 1000, true})
	_, ok := ends[es[3].ID]
	eq(t, ok, false)
	eq(t, TurnEnds(es, false)[es[3].ID], TurnEnd{9500, 9000, true})

	es = Build([]protocol.FactoryDroidMessage{
		msg("user", text("go"), at(1000)), msg("assistant", tool("a"), at(2000)),
		msg("user", text("also"), at(3000)),
		msg("assistant", `[{"type":"tool_use","id":"b","name":"Read","input":{}},{"type":"text","text":"both done"}]`, at(8000)),
	})
	ends = TurnEnds(es, false)
	_, ok = ends[es[1].ID]
	eq(t, ok, false)
	eq(t, ends[es[3].ID], TurnEnd{8000, 1000, true})

	es = Build([]protocol.FactoryDroidMessage{msg("user", text("go"), at(1000)), msg("assistant", tool("a"), at(2000))})
	eq(t, len(TurnEnds(es, true)), 0)
	eq(t, TurnEnds(es, false)[es[1].ID], TurnEnd{2000, 1000, true})

	es = Build([]protocol.FactoryDroidMessage{msg("assistant", text("hi"), at(2000))})
	eq(t, TurnEnds(es, false)[es[0].ID], TurnEnd{EndedAt: 2000})
}

func TestFormat(t *testing.T) {
	now := time.Date(2026, 9, 29, 23, 30, 0, 0, time.Local)
	ended := float64(time.Date(2026, 9, 29, 23, 13, 0, 0, time.Local).UnixMilli())
	eq(t, FormatTurnEnd(TurnEnd{EndedAt: ended}, now), "11:13 PM")
	eq(t, FormatTurnEnd(TurnEnd{ended, ended - 400, true}, now), "11:13 PM")
	eq(t, FormatTurnEnd(TurnEnd{ended, ended - 252_000, true}, now), "11:13 PM · 4m 12s")
	eq(t, FormatTimestamp(ended-86_400_000, now), "Sep 28 11:13 PM")
	for ms, want := range map[float64]string{640: "640 ms", 12_400: "12s", 252_000: "4m 12s", 3_780_000: "1h 03m"} {
		eq(t, FormatDuration(ms), want)
	}
}

func TestReuseUnchanged(t *testing.T) {
	asked := msg("user", `[{"type":"text","text":"look"}]`)
	reading := msg("assistant", `[{"type":"tool_use","id":"call-1","name":"Read","input":{}}]`)
	answered := msg("tool", `[{"type":"tool_result","toolUseId":"call-1","content":"x"}]`)
	first := Build([]protocol.FactoryDroidMessage{asked, reading, answered})
	again, changed := ReuseUnchanged(first, Build([]protocol.FactoryDroidMessage{asked, reading, answered}))
	eq(t, changed, false)
	eq(t, &again[0], &first[0])

	first = Build([]protocol.FactoryDroidMessage{asked, reading})
	next, changed := ReuseUnchanged(first, Build([]protocol.FactoryDroidMessage{asked, reading, answered}))
	eq(t, changed, true)
	if next[0] != first[0] || next[1] == first[1] {
		t.Fatal("only the changed entry is new")
	}
}

func call(name, input string, result ...string) *ToolCall {
	b := protocol.ContentBlock{Type: "tool_use", Raw: json.RawMessage(`{"type":"tool_use","id":"run-1","name":"` + name + `","input":` + input + `}`)}
	use, _ := decode[protocol.ToolUse](b)
	var r *protocol.ToolResult
	if len(result) > 0 {
		r = &protocol.ToolResult{ToolUseID: "run-1", Content: json.RawMessage(result[0])}
		if len(result) > 1 {
			true_ := true
			r.IsError = &true_
		}
	}
	c := newCall(b, use, r)
	c.Lifecycle = ScriptRunOf(use, c.Input) != ""
	return c
}

func TestToolNameAndSummary(t *testing.T) {
	s, n := ToolName("droi-memory___memory_list")
	eq(t, []string{s, n}, []string{"droi-memory", "memory_list"})
	for _, name := range []string{"Execute", "___odd", "odd___"} {
		s, n = ToolName(name)
		eq(t, []string{s, n}, []string{"", name})
	}
	eq(t, SummaryParts(call("Execute", `{"command":"ls -la\npwd","summary":"List"}`)), []SummaryPart{{Value: "List"}})
	eq(t, SummaryParts(call("Read", `{"file_path":"/w/a.ts","limit":40}`)), []SummaryPart{{Value: "/w/a.ts"}})
	long := strings.Repeat("x", 70)
	eq(t, SummaryParts(call("m___add", `{"scope":"project","category":"insight","text":"`+long+`\nsecond","extra":"no"}`)),
		[]SummaryPart{{"scope", "project"}, {"category", "insight"}, {"text", strings.Repeat("x", 57) + "…"}})
	eq(t, SummaryParts(call("t", `{"a":"  ","b":{"c":1},"d":[1],"limit":10,"block":false}`)), []SummaryPart{{"limit", "10"}, {"block", "false"}})
	eq(t, SummaryParts(call("t", `{}`)), []SummaryPart{})
	eq(t, Summary(call("m___list", `{"scope":"project","category":"insight"}`)), "scope: project · category: insight")
}

func TestStatusAndDiff(t *testing.T) {
	eq(t, ParseStatus(`{"success":true,"file_path":"/w/a.ts"}`), &Status{Success: true})
	eq(t, ParseStatus(`{"success":false,"error":"EACCES"}`), &Status{Message: "EACCES"})
	for _, s := range []string{`{"success":true,"stdout":"hi"}`, `[Process exited with code 0]`, `{"success":"yes"}`, `{"success": not json`} {
		if ParseStatus(s) != nil {
			t.Error(s)
		}
	}
	d := ParseDiff(`{"success":true,"diffLines":[{"type":"unchanged","content":"a","lineNumber":{"old":1,"new":1}},{"type":"removed","content":"b","lineNumber":{"old":2}},{"type":"added","content":"c","lineNumber":{"new":2}},{"type":"added","content":"d","lineNumber":{"new":3}}]}`)
	eq(t, d, &Diff{Added: 2, Removed: 1, Lines: []DiffLine{{"unchanged", "a", 1, 1}, {"removed", "b", 2, 0}, {"added", "c", 0, 2}, {"added", "d", 0, 3}}})
	for _, s := range []string{`[Process exited]`, `{"success":true}`, `{"diffLines": not json`} {
		if ParseDiff(s) != nil {
			t.Error(s)
		}
	}
	eq(t, CreatedFileDiff(call("Create", `{"file_path":"a.ts","content":"one\ntwo\n"}`)), &Diff{Added: 2, Lines: []DiffLine{{"added", "one", 0, 1}, {"added", "two", 0, 2}}})
	if CreatedFileDiff(call("Edit", `{"content":"x"}`)) != nil || CreatedFileDiff(call("Create", `{"file_path":"a"}`)) != nil {
		t.Fatal("not a Create")
	}
}

func TestPermissions(t *testing.T) {
	j := func(s string) json.RawMessage { return json.RawMessage(s) }
	eq(t, PermissionDetail(j(`{"fullCommand":"npm test -- --watch"}`), j(`{"command":"npm test"}`)), "$ npm test -- --watch")
	eq(t, PermissionDetail(j(`{"filePath":"/w/a.ts"}`), j(`{}`)), "/w/a.ts")
	eq(t, PermissionDetail(nil, j(`{"command":"ls"}`)), "$ ls")
	eq(t, PermissionDetail(j(`null`), j(`{"url": "https://x"}`)), `{"url":"https://x"}`)

	details := j(`{"type":"script","impactLevel":"medium","calls":[{"toolName":"Execute","line":3,"toolInput":{"command":"sleep 70 && echo slept"},"confirmation":{"type":"exec","fullCommand":"sleep 70 && echo slept","impactLevel":"low"}},{"toolName":"droi-memory___memory_add","line":5,"toolInput":{"text":"x"},"confirmation":{"type":"mcp_tool"}}]}`)
	eq(t, ParseScriptPermission(details), &ScriptPermission{Impact: "medium", Calls: []ScriptPermissionCall{
		{3, "Execute", "$ sleep 70 && echo slept", "low"}, {5, "memory_add", `{"text":"x"}`, ""},
	}})
	if ParseScriptPermission(j(`{"type":"exec"}`)) != nil || ParseScriptPermission(nil) != nil {
		t.Fatal("not a Script request")
	}
}

func TestClusterLabel(t *testing.T) {
	done := func(name string, nested ...*ToolCall) *ToolCall {
		c := call(name, `{"script":""}`, `"ok"`)
		c.Nested = nested
		return c
	}
	l, p := ClusterLabel([]*ToolCall{done("Script", done("Read"), done("Grep")), done("LS")})
	eq(t, []any{l, p}, []any{"Used 3 tools", false})
	l, _ = ClusterLabel([]*ToolCall{done("Script", done("Read"))})
	eq(t, l, "Used Read")
	l, _ = ClusterLabel([]*ToolCall{done("Script")})
	eq(t, l, "Used Script")
	l, p = ClusterLabel([]*ToolCall{done("Script", call("Execute", `{}`))})
	eq(t, []any{l, p}, []any{"Running Execute", true})
}

const logPath = "/home/dev/.factory/artifacts/scripts/s1/run-1.log"

func TestReadScriptRun(t *testing.T) {
	parts := func(ps ...string) string {
		b, _ := json.Marshal(func() []map[string]string {
			var out []map[string]string
			for _, p := range ps {
				out = append(out, map[string]string{"type": "text", "text": p})
			}
			return out
		}())
		return string(b)
	}
	run := ReadScriptRun(call("WaitForScript", `{"toolCallId":"run-1"}`, parts(
		"# probe workspace\nline two\n\nslept",
		"[Script completed · 3 calls · 86 B in sandbox · 33 B emitted (38%) · retained: r1 Read README.md, r2 Execute Sleep · log: "+logPath+"]",
		`{"toolCallId":"run-1","status":"completed","result":null}`)))
	eq(t, run, ScriptRun{Output: "# probe workspace\nline two\n\nslept", Status: "completed", Stats: "3 calls · 86 B in sandbox · 33 B emitted (38%)", LogPath: logPath})

	run = ReadScriptRun(call("Script", `{"script":""}`, `"{\"toolCallId\":\"run-1\",\"status\":\"running\"}"`))
	eq(t, run, ScriptRun{Status: "running"})

	run = ReadScriptRun(call("Script", `{"script":""}`, parts("[Script failed at 2:5 · 1 call completed · 0 B emitted · log: /l]", `{"toolCallId":"run-1","status":"failed","error":"ReferenceError: x is not defined"}`)))
	eq(t, run, ScriptRun{Status: "failed", Error: "ReferenceError: x is not defined", Stats: "1 call completed · 0 B emitted", LogPath: "/l"})

	run = ReadScriptRun(call("Script", `{"script":""}`, parts(`{"toolCallId":"run-1","status":"completed","result":{"files":2}}`)))
	eq(t, run.Value, "{\n  \"files\": 2\n}")

	run = ReadScriptRun(call("Script", `{"script":""}`, `"Script is not available"`, "error"))
	eq(t, run, ScriptRun{Status: "failed", Error: "Script is not available"})
	eq(t, ReadScriptRun(call("Script", `{"script":""}`)), ScriptRun{})
}

func TestScriptSource(t *testing.T) {
	script, ins, ok := ScriptSource(call("Script", `{"script":"text(inputs.body)","inputs":{"body":"hi","n":3}}`))
	eq(t, []any{script, ins, ok}, []any{"text(inputs.body)", []ScriptInput{{"body", "hi"}}, true})
	_, _, ok = ScriptSource(call("WaitForScript", `{"toolCallId":"run-1"}`))
	eq(t, ok, false)
	eq(t, ScriptToolNames("const a = await tools.Read({})\nawait tools.Execute({})\nawait tools.droi_memory.memory_search({})\nawait tools.Read({})"), []string{"Read", "Execute", "memory_search"})
	eq(t, ScriptSummary(call("Script", `{"script":"await tools.Grep({}); await tools.Edit({})"}`)), "Grep · Edit")
	eq(t, ScriptSummary(call("WaitForScript", `{"toolCallId":"run-1"}`)), "continued")
	eq(t, ScriptSummary(call("WaitForScript", `{"toolCallId":"run-1","kill":true}`)), "stopped")
}

func TestReplyTextJoinsTheTurnsTextOnly(t *testing.T) {
	turn := []*Entry{
		{User: true, Blocks: []Block{{Kind: Text, Text: "Why?"}}},
		{Blocks: []Block{{Kind: Thinking, Text: "hmm"}, {Kind: Text, Text: "Let me look.\n"}, {Kind: Tools}}},
		{Blocks: []Block{{Kind: Text, Text: "It is the cache.\n\n```go\nx := 1\n```"}}},
	}
	eq(t, ReplyText(turn), "Let me look.\n\nIt is the cache.\n\n```go\nx := 1\n```")
	eq(t, ReplyText(turn[:1]), "")
}
