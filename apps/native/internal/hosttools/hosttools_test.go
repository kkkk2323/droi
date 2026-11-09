package hosttools

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/mcpserver"
)

// mcp is a tool server run by serveMain, spoken to as the Daemon does.
type mcp struct {
	t      *testing.T
	in     *io.PipeWriter
	out    *bufio.Reader
	errOut *strings.Builder
	done   chan int
	next   int
}

func startServer(t *testing.T, args []string, env map[string]string) *mcp {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	m := &mcp{t: t, in: inW, out: bufio.NewReader(outR), errOut: &strings.Builder{}, done: make(chan int, 1)}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	go func() {
		code := serveMain(args, lookup, inR, outW, m.errOut)
		_ = outW.Close()
		m.done <- code
	}()
	t.Cleanup(func() { _ = inW.Close() })
	return m
}

// request sends one request and reads its answer's result, or error.
func (m *mcp) request(method string, params any) (result, rpcErr json.RawMessage) {
	m.t.Helper()
	m.next++
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.next, "method": method, "params": params})
	got := make(chan string, 1)
	go func() {
		s, _ := m.out.ReadString('\n')
		got <- s
	}()
	if _, err := m.in.Write(append(line, '\n')); err != nil {
		m.t.Fatal(err)
	}
	select {
	case s := <-got:
		var answer struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(s), &answer); err != nil {
			m.t.Fatalf("answer %q: %v", s, err)
		}
		return answer.Result, answer.Error
	case <-time.After(10 * time.Second):
		m.t.Fatalf("no answer to %s", method)
	}
	return nil, nil
}

func (m *mcp) call(tool string, args map[string]any, sessionID string) (text string, isError bool) {
	m.t.Helper()
	params := map[string]any{"name": tool, "arguments": args}
	if sessionID != "" {
		params["_meta"] = map[string]any{"assemblySessionId": sessionID}
	}
	result, rpcErr := m.request("tools/call", params)
	if rpcErr != nil {
		m.t.Fatalf("tools/call failed: %s", rpcErr)
	}
	var r mcpserver.Result
	if err := json.Unmarshal(result, &r); err != nil || len(r.Content) != 1 {
		m.t.Fatalf("tools/call result %s", result)
	}
	return r.Content[0].Text, r.IsError
}

const echoTools = `[{"name":"echo","description":"Says it back.","inputSchema":{"type":"object"}}]`

type seen struct {
	tool, sessionID string
	args            map[string]any
}

func newBridge(t *testing.T, calls chan<- seen) *Bridge {
	t.Helper()
	b, err := New("/Applications/Droi.app/Contents/MacOS/Droi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	b.Register("droi-test", Server{
		Tools: func() json.RawMessage { return json.RawMessage(echoTools) },
		Call: func(_ context.Context, tool string, args map[string]any, sessionID string) (mcpserver.Result, error) {
			calls <- seen{tool, sessionID, args}
			r := mcpserver.Text("said "+args["text"].(string), false)
			r.StructuredContent = map[string]any{"said": args["text"]}
			return r, nil
		},
		Autonomy: "low",
	})
	return b
}

func envOf(b *Bridge) map[string]string {
	env := b.Overlay().MCPServers["droi-test"].Config["env"].(map[string]string)
	return map[string]string{EnvURL: env[EnvURL], EnvToken: env[EnvToken]}
}

func TestAToolServerListsAndForwardsCallsWithTheSession(t *testing.T) {
	calls := make(chan seen, 1)
	b := newBridge(t, calls)
	m := startServer(t, []string{"droi-test"}, envOf(b))

	result, _ := m.request("tools/list", map[string]any{})
	var list struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(result, &list); err != nil || string(list.Tools) != echoTools {
		t.Fatalf("tools/list %s", result)
	}
	result, _ = m.request("tools/call", map[string]any{
		"name": "echo", "arguments": map[string]any{"text": "hi"},
		"_meta": map[string]any{"assemblySessionId": "session-1"},
	})
	var r struct {
		Content           []mcpserver.Content `json:"content"`
		StructuredContent map[string]any      `json:"structuredContent"`
		IsError           bool                `json:"isError"`
	}
	if err := json.Unmarshal(result, &r); err != nil || r.IsError || r.Content[0].Text != "said hi" || r.StructuredContent["said"] != "hi" {
		t.Fatalf("tools/call %s", result)
	}
	if got := <-calls; got.tool != "echo" || got.sessionID != "session-1" || !reflect.DeepEqual(got.args, map[string]any{"text": "hi"}) {
		t.Fatalf("the Host got %+v", got)
	}
}

func TestAToolErrorIsAnErrorResult(t *testing.T) {
	b, err := New("/droi")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Register("droi-test", Server{
		Tools: func() json.RawMessage { return json.RawMessage(echoTools) },
		Call: func(context.Context, string, map[string]any, string) (mcpserver.Result, error) {
			return mcpserver.Result{}, context.DeadlineExceeded
		},
	})
	env := b.Overlay().MCPServers["droi-test"].Config["env"].(map[string]string)
	m := startServer(t, []string{"droi-test"}, env)
	if text, isError := m.call("echo", nil, ""); !isError || text != context.DeadlineExceeded.Error() {
		t.Fatalf("%q %v", text, isError)
	}
}

func TestAWrongTokenAnswersEveryCallWithTheRefusal(t *testing.T) {
	calls := make(chan seen, 1)
	b := newBridge(t, calls)
	env := envOf(b)
	env[EnvToken] = "not-the-token"
	m := startServer(t, []string{"droi-test"}, env)
	result, _ := m.request("tools/list", map[string]any{})
	if !strings.Contains(string(result), `"tools":[]`) {
		t.Fatalf("tools/list %s", result)
	}
	if text, isError := m.call("echo", map[string]any{"text": "hi"}, "s"); !isError || !strings.Contains(text, "401") {
		t.Fatalf("%q %v", text, isError)
	}
	if len(calls) != 0 {
		t.Fatal("a call with a wrong token ran")
	}
	if !strings.Contains(m.errOut.String(), "401") {
		t.Fatalf("stderr %q", m.errOut.String())
	}
}

func TestAnUnknownServerSaysSo(t *testing.T) {
	b := newBridge(t, make(chan seen, 1))
	m := startServer(t, []string{"droi-other"}, envOf(b))
	if text, isError := m.call("echo", map[string]any{"text": "hi"}, ""); !isError || !strings.Contains(text, "no tool server named droi-other") {
		t.Fatalf("%q %v", text, isError)
	}
}

func TestACallAfterTheHostQuitSaysItCannotBeReached(t *testing.T) {
	b := newBridge(t, make(chan seen, 1))
	m := startServer(t, []string{"droi-test"}, envOf(b))
	if _, isError := m.call("echo", map[string]any{"text": "hi"}, ""); isError {
		t.Fatal("the first call failed")
	}
	_ = b.Close()
	if text, isError := m.call("echo", map[string]any{"text": "hi"}, ""); !isError || !strings.Contains(text, "cannot be reached") || !strings.Contains(text, "echo did not run") {
		t.Fatalf("%q %v", text, isError)
	}
	_ = m.in.Close()
	if code := <-m.done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestServeMainNeedsItsEnvironmentAndAName(t *testing.T) {
	for _, c := range []struct {
		args []string
		env  map[string]string
	}{
		{nil, map[string]string{EnvURL: "http://127.0.0.1:1", EnvToken: "t"}},
		{[]string{"droi-test"}, map[string]string{EnvURL: "http://127.0.0.1:1"}},
	} {
		var errOut strings.Builder
		lookup := func(k string) (string, bool) { v, ok := c.env[k]; return v, ok }
		if code := serveMain(c.args, lookup, strings.NewReader(""), io.Discard, &errOut); code != 2 || errOut.Len() == 0 {
			t.Errorf("%v: exit %d, %q", c.args, code, errOut.String())
		}
	}
}

func TestTheBridgeRefusesRequestsWithoutItsToken(t *testing.T) {
	b := newBridge(t, make(chan seen, 1))
	f := &forwarder{base: b.URL() + "/servers/droi-test", token: "wrong", http: newForwarderClient()}
	if _, err := f.do(context.Background(), "GET", "/tools", nil); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
}

func TestOverlayRunsEachServerApprovedThroughTheExecutable(t *testing.T) {
	b := newBridge(t, make(chan seen, 1))
	b.Register("droi-two", Server{Autonomy: "medium", ToolAutonomy: map[string]string{"run": "high"}})
	o := b.Overlay()
	if len(o.MCPServers) != 2 || !o.BlockOnMCPLoad {
		t.Fatalf("%+v", o)
	}
	s := o.MCPServers["droi-two"]
	env := s.Config["env"].(map[string]string)
	if s.Config["type"] != "stdio" || s.Config["command"] != "/Applications/Droi.app/Contents/MacOS/Droi" ||
		!reflect.DeepEqual(s.Config["args"], []string{"host-tools", "droi-two"}) ||
		env[EnvURL] != b.URL() || len(env[EnvToken]) != 64 ||
		!s.Approved || s.Autonomy != "medium" || s.Tools["run"] != "high" {
		t.Fatalf("%+v", s)
	}
	if o.MCPServers["droi-test"].Autonomy != "low" {
		t.Fatalf("%+v", o.MCPServers["droi-test"])
	}
}

func TestOverlayIsEmptyWithoutAnExecutable(t *testing.T) {
	b, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Register("droi-test", Server{})
	if o := b.Overlay(); len(o.MCPServers) != 0 || o.BlockOnMCPLoad {
		t.Fatalf("%+v", o)
	}
}

func TestSelector(t *testing.T) {
	if got := Selector("droi-workstream"); !reflect.DeepEqual(got, []string{"mcp:droi-workstream"}) {
		t.Error(got)
	}
	if got := Selector("droi-workstream", "report", "spawn_agent"); !reflect.DeepEqual(got, []string{"mcp:droi-workstream/report", "mcp:droi-workstream/spawn_agent"}) {
		t.Error(got)
	}
}
