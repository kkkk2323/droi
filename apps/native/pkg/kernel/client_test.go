package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeKernel serves one connection the way Kernel does: the test reads what
// the Client sent and answers it when it likes, so the order, the timing and
// the failures are the test's to decide.
type fakeKernel struct {
	t      *testing.T
	server *httptest.Server
	frames chan rpcFrame
	hello  chan hello

	mu   sync.Mutex
	conn *websocket.Conn
}

// hello is how the Client dialled.
type hello struct {
	authorization string
	token         string
}

func startFakeKernel(t *testing.T) *fakeKernel {
	t.Helper()
	f := &fakeKernel{t: t, frames: make(chan rpcFrame, 128), hello: make(chan hello, 1)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case f.hello <- hello{authorization: r.Header.Get("Authorization"), token: r.URL.Query().Get("token")}:
		default:
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var frame rpcFrame
			if json.Unmarshal(data, &frame) != nil {
				continue
			}
			f.frames <- frame
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeKernel) url() string { return "ws" + strings.TrimPrefix(f.server.URL, "http") + "/rpc" }

// next is the next frame the Client sent.
func (f *fakeKernel) next(t *testing.T) rpcFrame {
	t.Helper()
	select {
	case frame := <-f.frames:
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("the client sent nothing")
		return rpcFrame{}
	}
}

// outFrame is a frame the test sends the other way.
type outFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func (f *fakeKernel) write(t *testing.T, frame outFrame) {
	t.Helper()
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	f.writeRaw(t, b)
}

func (f *fakeKernel) writeRaw(t *testing.T, b []byte) {
	t.Helper()
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func (f *fakeKernel) reply(t *testing.T, id json.RawMessage, result any) {
	t.Helper()
	f.write(t, outFrame{JSONRPC: "2.0", ID: id, Result: result})
}

func (f *fakeKernel) fail(t *testing.T, id json.RawMessage, code int, message string) {
	t.Helper()
	f.write(t, outFrame{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}})
}

func (f *fakeKernel) notify(t *testing.T, method string, params any) {
	t.Helper()
	f.write(t, outFrame{JSONRPC: "2.0", Method: method, Params: params})
}

// hangUp ends the connection as a crashed Kernel would.
func (f *fakeKernel) hangUp() {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	_ = conn.CloseNow()
}

func dialFake(t *testing.T, f *fakeKernel) *Client {
	t.Helper()
	c, err := Dial(context.Background(), Options{URL: f.url(), Token: "t-test", RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// caller runs one method with params from a fixture, and answers its result.
type caller struct {
	method string
	do     func(t *testing.T, ctx context.Context, c *Client, params json.RawMessage) (any, error)
}

// decodeParams reads a fixture's params into the method's type.
func decodeParams[T any](t *testing.T, raw json.RawMessage) *T {
	t.Helper()
	p := new(T)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, p); err != nil {
			t.Fatalf("params: %v", err)
		}
	}
	return p
}

// callers is every method of the contract, called the way the window calls it.
var callers = []caller{
	{MethodHarnessInfo, func(t *testing.T, ctx context.Context, c *Client, _ json.RawMessage) (any, error) {
		return c.Harness(ctx)
	}},
	{MethodConversationCreate, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Create(ctx, *decodeParams[CreateParams](t, raw))
	}},
	{MethodConversationList, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.List(ctx, *decodeParams[ListParams](t, raw))
	}},
	{MethodConversationOpen, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Open(ctx, *decodeParams[OpenParams](t, raw))
	}},
	{MethodConversationUnwatch, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return struct{}{}, c.Unwatch(ctx, decodeParams[IDParams](t, raw).ID)
	}},
	{MethodConversationSubmit, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Submit(ctx, *decodeParams[SubmitParams](t, raw))
	}},
	{MethodConversationAbort, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return struct{}{}, c.Abort(ctx, *decodeParams[AbortParams](t, raw))
	}},
	{MethodSubmissionAbort, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.AbortSubmission(ctx, *decodeParams[AbortSubmissionParams](t, raw))
	}},
	{MethodConversationConfig, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Configure(ctx, *decodeParams[ConfigureParams](t, raw))
	}},
	{MethodConversationCompact, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Compact(ctx, *decodeParams[CompactParams](t, raw))
	}},
	{MethodConversationFork, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Fork(ctx, *decodeParams[ForkParams](t, raw))
	}},
	{MethodConversationEntries, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Entries(ctx, *decodeParams[EntriesParams](t, raw))
	}},
	{MethodConversationUsage, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Usage(ctx, decodeParams[UsageParams](t, raw).ID)
	}},
	{MethodTaskAbort, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.AbortTask(ctx, *decodeParams[AbortTaskParams](t, raw))
	}},
	{MethodIndexUpdate, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return struct{}{}, c.UpdateIndex(ctx, *decodeParams[IndexUpdateParams](t, raw))
	}},
	{MethodProcessList, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.Processes(ctx, decodeParams[ProcessesParams](t, raw).ID)
	}},
	{MethodProcessStop, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.StopProcess(ctx, *decodeParams[StopProcessParams](t, raw))
	}},
	{MethodApprovalRespond, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.RespondApproval(ctx, *decodeParams[ApprovalRespondParams](t, raw))
	}},
	{MethodAskRespond, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.RespondAsk(ctx, *decodeParams[AskRespondParams](t, raw))
	}},
	{MethodMCPList, func(t *testing.T, ctx context.Context, c *Client, _ json.RawMessage) (any, error) { return c.MCP(ctx) }},
	{MethodMCPSet, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.SetMCP(ctx, *decodeParams[MCPSetParams](t, raw))
	}},
	{MethodMCPAuthorize, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.AuthorizeMCP(ctx, *decodeParams[MCPAuthorizeParams](t, raw))
	}},
	{MethodMCPAuthorizeCancel, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return struct{}{}, c.CancelMCPAuthorize(ctx, *decodeParams[MCPAuthorizeParams](t, raw))
	}},
	{MethodSkillsList, func(t *testing.T, ctx context.Context, c *Client, _ json.RawMessage) (any, error) {
		return c.Skills(ctx)
	}},
	{MethodSkillsSetDisabled, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.SetSkillDisabled(ctx, *decodeParams[SkillsSetDisabledParams](t, raw))
	}},
}

// Every method goes over the wire in the contract's shape: the params the
// client sends are the sample's, and the result it decodes is the sample's.
func TestClientCallsEveryMethodFromItsFixture(t *testing.T) {
	byMethod := map[string]caller{}
	for _, c := range callers {
		byMethod[c.method] = c
	}
	if len(byMethod) != len(Methods) {
		t.Fatalf("the callers cover %d methods, the contract has %d", len(byMethod), len(Methods))
	}
	idx := readIndex(t)
	f := startFakeKernel(t)
	c := dialFake(t, f)
	for _, name := range idx.Methods {
		fx := readFixture(t, name)
		method := fx.Method
		run, ok := byMethod[method]
		if !ok {
			t.Fatalf("no Go call for %s", method)
		}
		t.Run(method, func(t *testing.T) {
			var request rpcFrame
			if err := json.Unmarshal(fx.Request, &request); err != nil {
				t.Fatal(err)
			}
			var response rpcFrame
			if err := json.Unmarshal(fx.Response, &response); err != nil {
				t.Fatal(err)
			}
			type outcome struct {
				value any
				err   error
			}
			done := make(chan outcome, 1)
			go func() {
				value, err := run.do(t, context.Background(), c, request.Params)
				done <- outcome{value, err}
			}()
			sent := f.next(t)
			if sent.Method != run.method {
				t.Fatalf("sent %s, want %s", sent.Method, run.method)
			}
			if sent.JSONRPC != "2.0" || len(sent.ID) == 0 {
				t.Fatalf("sent %s id=%s, want a JSON-RPC 2.0 request", sent.JSONRPC, sent.ID)
			}
			assertSameJSON(t, "params", request.Params, sent.Params)
			f.write(t, outFrame{JSONRPC: "2.0", ID: sent.ID, Result: json.RawMessage(response.Result)})
			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("%s: %v", run.method, got.err)
				}
				encoded, err := json.Marshal(got.value)
				if err != nil {
					t.Fatal(err)
				}
				assertSameJSON(t, "result", response.Result, encoded)
			case <-time.After(5 * time.Second):
				t.Fatalf("%s never answered", run.method)
			}
		})
	}
}

func loggedValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return out
}

func TestClientSendsTheToken(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	go func() { _, _ = c.Harness(context.Background()) }()
	select {
	case h := <-f.hello:
		if h.authorization != "Bearer t-test" {
			t.Fatalf("Authorization = %q", h.authorization)
		}
		if h.token != "t-test" {
			t.Fatalf("?token = %q", h.token)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the client never dialled")
	}
}

func TestClientDeliversNotificationsInOrder(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	var want []ID
	for i := ID(1); i <= 3; i++ {
		want = append(want, i)
		f.notify(t, NotifyConversationView, map[string]any{"id": i, "kind": "value", "value": map[string]any{"conversation": map[string]any{"id": i}}})
	}
	var got []ID
	for range want {
		select {
		case n := <-c.Notifications():
			if n.Method != NotifyConversationView {
				t.Fatalf("notification %s", n.Method)
			}
			var view ViewNotification
			if err := n.Decode(&view); err != nil {
				t.Fatal(err)
			}
			got = append(got, view.ID)
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d notifications arrived", len(got))
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("notifications = %v, want %v", got, want)
	}
}

func TestClientReadsABatch(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	done := make(chan error, 2)
	go func() {
		_, err := c.Harness(context.Background())
		done <- err
	}()
	go func() {
		_, err := c.Usage(context.Background(), 7)
		done <- err
	}()
	first, second := f.next(t), f.next(t)
	batch, err := json.Marshal([]outFrame{
		{JSONRPC: "2.0", ID: first.ID, Result: map[string]any{"version": "kernel-1.5.0-1"}},
		{JSONRPC: "2.0", ID: second.ID, Result: map[string]any{"context": map[string]any{"used": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.writeRaw(t, batch)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("batch answer: %v", err)
		}
	}
}

func TestClientTakesANumericId(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := c.Harness(context.Background())
		done <- err
	}()
	sent := f.next(t)
	// The same id, as a number: JSON-RPC allows either spelling.
	numeric := json.RawMessage(strings.Trim(string(sent.ID), `"`))
	f.write(t, outFrame{JSONRPC: "2.0", ID: numeric, Result: map[string]any{"version": "kernel-1.5.0-1"}})
	if err := <-done; err != nil {
		t.Fatalf("a numeric id lost the call: %v", err)
	}
}

func TestClientAnswersCallsOutOfOrder(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)

	const calls = 12
	results := make([]ID, calls)
	var wg sync.WaitGroup
	wg.Add(calls)
	for i := 0; i < calls; i++ {
		go func() {
			defer wg.Done()
			created, err := c.Create(context.Background(), CreateParams{Cwd: "/tmp/w"})
			if err != nil {
				t.Error(err)
				return
			}
			results[i] = created.ConversationID
		}()
	}
	frames := make([]rpcFrame, 0, calls)
	for i := 0; i < calls; i++ {
		frames = append(frames, f.next(t))
	}
	// Answer in reverse: each call must still get its own answer.
	for i := calls - 1; i >= 0; i-- {
		f.reply(t, frames[i].ID, map[string]any{"conversationId": 100 + i, "view": map[string]any{"conversation": map[string]any{"id": 100 + i}}})
	}
	wg.Wait()
	seen := map[ID]bool{}
	for _, id := range results {
		if id == 0 || seen[id] {
			t.Fatalf("results = %v", results)
		}
		seen[id] = true
	}
}

func TestClientReportsAnRPCError(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := c.Create(context.Background(), CreateParams{Cwd: "/tmp/w"})
		done <- err
	}()
	sent := f.next(t)
	f.fail(t, sent.ID, CodeInvalidParams, "params.id is required")
	err := <-done
	var rpc *RPCError
	if !errors.As(err, &rpc) {
		t.Fatalf("err = %v, want *RPCError", err)
	}
	if rpc.Code != CodeInvalidParams || !Is(err, CodeInvalidParams) || Is(err, CodeConversationGone) {
		t.Fatalf("rpc = %+v", rpc)
	}
}

func TestClientTimesOut(t *testing.T) {
	f := startFakeKernel(t)
	c, err := Dial(context.Background(), Options{URL: f.url(), RequestTimeout: 80 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Harness(context.Background()); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("err = %v, want the deadline", err)
	}
}

func TestClientClosesWaitingCalls(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := c.Harness(context.Background())
		done <- err
	}()
	f.next(t)
	if err := c.Close(); err != nil && !errors.Is(err, ErrClosed) {
		t.Logf("close: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the waiting call succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting call never ended")
	}
	if _, err := c.Harness(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("a call after Close: %v", err)
	}
}

func TestClientEndsWhenKernelGoesAway(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	f.hangUp()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the client did not notice")
	}
	if c.Err() == nil {
		t.Fatal("Err() is nil after a hang-up")
	}
	if _, err := c.Harness(context.Background()); err == nil || errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want the read loop's error", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-c.Notifications():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the notification channel did not close")
		}
	}
}

// A Client watches what it opened, so a Supervisor that reconnects a new
// Client knows what to open again.
func TestClientTracksWhatItWatched(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	if got := c.Opened(); len(got) != 0 {
		t.Fatalf("Opened() = %v before any open", got)
	}
	open := func(id ID) {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := c.Open(context.Background(), OpenParams{ID: id})
			done <- err
		}()
		sent := f.next(t)
		if sent.Method != MethodConversationOpen {
			t.Fatalf("sent %s", sent.Method)
		}
		f.reply(t, sent.ID, map[string]any{"view": map[string]any{"conversation": map[string]any{"id": id}}})
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	open(7)
	open(8)
	if got := c.Opened(); len(got) != 2 {
		t.Fatalf("Opened() = %v", got)
	}
	done := make(chan error, 1)
	go func() { done <- c.Unwatch(context.Background(), 7) }()
	sent := f.next(t)
	if sent.Method != MethodConversationUnwatch {
		t.Fatalf("sent %s", sent.Method)
	}
	if !reflect.DeepEqual(loggedValue(t, sent.Params), map[string]any{"id": float64(7)}) {
		t.Fatalf("unwatch params %s", sent.Params)
	}
	f.reply(t, sent.ID, map[string]any{})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	opened := c.Opened()
	if len(opened) != 1 || opened[0] != 8 {
		t.Fatalf("Opened() = %v after unwatching 7", opened)
	}
}
