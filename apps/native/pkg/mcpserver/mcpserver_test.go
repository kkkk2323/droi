package mcpserver

import (
	"errors"
	"strings"
	"testing"
)

type call struct {
	name      string
	args      map[string]any
	sessionID string
}

func serve(t *testing.T, input string, s Server) string {
	t.Helper()
	var out strings.Builder
	if err := Serve(strings.NewReader(input), &out, s); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func testServer(calls *[]call) Server {
	return Server{
		Name:    "demo",
		Title:   "Demo <&>",
		Version: "2.0.0",
		Tools:   []byte(`[{"name":"echo"}]`),
		Call: func(name string, args map[string]any, sessionID string) (Result, error) {
			*calls = append(*calls, call{name, args, sessionID})
			switch name {
			case "fail":
				return Result{}, errors.New("the store is busy")
			case "refuse":
				return Text("not here", true), nil
			}
			return Text("<ok>", false), nil
		},
	}
}

func eq(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestInitializeAnswersServerInfoAndEchoesTheProtocolVersion(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`+"\n", testServer(&calls))
	eq(t, got, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"demo","title":"Demo <&>","version":"2.0.0"}}}`+"\n")
}

func TestInitializeWithoutAVersionAnswersTheDefault(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":"a","method":"initialize"}`+"\n", testServer(&calls))
	if !strings.Contains(got, `"protocolVersion":"2025-06-18"`) || !strings.HasPrefix(got, `{"jsonrpc":"2.0","id":"a",`) {
		t.Errorf("got %s", got)
	}
}

func TestPingAnswersAnEmptyResult(t *testing.T) {
	var calls []call
	eq(t, serve(t, `{"jsonrpc":"2.0","id":2,"method":"ping"}`+"\n", testServer(&calls)), `{"jsonrpc":"2.0","id":2,"result":{}}`+"\n")
}

func TestToolsListAnswersTheToolsAsGiven(t *testing.T) {
	var calls []call
	eq(t, serve(t, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`+"\n", testServer(&calls)), `{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"echo"}]}}`+"\n")
}

func TestToolsCallPassesTheNameArgumentsAndTheSessionFromMeta(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"q":"x"},"_meta":{"assemblySessionId":"s-1"}}}`+"\n", testServer(&calls))
	eq(t, got, `{"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"<ok>"}]}}`+"\n")
	if len(calls) != 1 || calls[0].name != "echo" || calls[0].args["q"] != "x" || calls[0].sessionID != "s-1" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestToolsCallWithoutArgumentsOrMetaGetsAnEmptyMapAndNoSession(t *testing.T) {
	var calls []call
	serve(t, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo"}}`+"\n", testServer(&calls))
	if len(calls) != 1 || calls[0].args == nil || len(calls[0].args) != 0 || calls[0].sessionID != "" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestToolsCallMarksAnErrorResult(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"refuse"}}`+"\n", testServer(&calls))
	eq(t, got, `{"jsonrpc":"2.0","id":5,"result":{"content":[{"type":"text","text":"not here"}],"isError":true}}`+"\n")
}

func TestACallErrorBecomesAnErrorResult(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"fail"}}`+"\n", testServer(&calls))
	eq(t, got, `{"jsonrpc":"2.0","id":6,"result":{"content":[{"type":"text","text":"the store is busy"}],"isError":true}}`+"\n")
}

func TestToolsCallWithoutANameIsInvalidParams(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`+"\n", testServer(&calls))
	eq(t, got, `{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"tools/call needs a name"}}`+"\n")
	if len(calls) != 0 {
		t.Errorf("calls = %+v", calls)
	}
}

func TestParseErrorAnswersANullID(t *testing.T) {
	var calls []call
	eq(t, serve(t, "not json\n", testServer(&calls)), `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`+"\n")
}

func TestJSONThatIsNoObjectGetsNoAnswer(t *testing.T) {
	var calls []call
	eq(t, serve(t, "[1]\n42\n", testServer(&calls)), "")
}

func TestUnknownMethodIsNotFound(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":8,"method":"resources/list"}`+"\n", testServer(&calls))
	eq(t, got, `{"jsonrpc":"2.0","id":8,"error":{"code":-32601,"message":"Method not found: resources/list"}}`+"\n")
}

func TestARequestWithoutAMethodIsNotFoundAsUndefined(t *testing.T) {
	var calls []call
	got := serve(t, `{"jsonrpc":"2.0","id":9}`+"\n", testServer(&calls))
	eq(t, got, `{"jsonrpc":"2.0","id":9,"error":{"code":-32601,"message":"Method not found: undefined"}}`+"\n")
}

func TestNotificationsGetNoReply(t *testing.T) {
	var calls []call
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","method":"unknown/thing"}` + "\n" +
		`{"jsonrpc":"2.0","id":null,"method":"unknown/thing"}` + "\n"
	eq(t, serve(t, input, testServer(&calls)), "")
}

func TestBlankLinesAndCRLFAreTolerated(t *testing.T) {
	var calls []call
	input := "\n  \r\n" + `{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\r\n"
	eq(t, serve(t, input, testServer(&calls)), `{"jsonrpc":"2.0","id":2,"result":{}}`+"\n")
}

func TestALastLineWithoutANewlineIsServed(t *testing.T) {
	var calls []call
	eq(t, serve(t, `{"jsonrpc":"2.0","id":2,"method":"ping"}`, testServer(&calls)), `{"jsonrpc":"2.0","id":2,"result":{}}`+"\n")
}
