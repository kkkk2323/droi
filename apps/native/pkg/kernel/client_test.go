package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
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
	rawQuery      string
	target        string
}

func startFakeKernel(t *testing.T) *fakeKernel {
	t.Helper()
	f := &fakeKernel{t: t, frames: make(chan rpcFrame, 128), hello: make(chan hello, 1)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case f.hello <- hello{authorization: r.Header.Get("Authorization"), token: r.URL.Query().Get("token"), rawQuery: r.URL.RawQuery, target: r.RequestURI}:
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
	conn := f.connFor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// connFor is the accepted connection, waiting for the dial when it is still in
// flight. A test that writes before the Client's dial landed — or a server that
// has read a frame but not yet stored the connection — is a race in this
// helper, not a Kernel that lost something.
func (f *fakeKernel) connFor(t *testing.T) *websocket.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		conn := f.conn
		f.mu.Unlock()
		if conn != nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatal("the client never connected")
		}
		time.Sleep(time.Millisecond)
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
func (f *fakeKernel) hangUp(t *testing.T) {
	t.Helper()
	_ = f.connFor(t).CloseNow()
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
	{MethodModelComplete, func(t *testing.T, ctx context.Context, c *Client, raw json.RawMessage) (any, error) {
		return c.ModelComplete(ctx, *decodeParams[ModelCompleteParams](t, raw))
	}},
	{MethodKernelShutdown, func(t *testing.T, ctx context.Context, c *Client, _ json.RawMessage) (any, error) {
		return c.Shutdown(ctx)
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

// The token goes in the one place the contract names, and nowhere else: not in
// the URL, where a query string reaches a request log.
func TestClientSendsTheToken(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	go func() { _, _ = c.Harness(context.Background()) }()
	select {
	case h := <-f.hello:
		if h.authorization != "Bearer t-test" {
			t.Fatalf("Authorization = %q", h.authorization)
		}
		if h.token != "" {
			t.Fatalf("the token was in the URL: ?token = %q", h.token)
		}
		if h.rawQuery != "" {
			t.Fatalf("the dial sent the query %q", h.rawQuery)
		}
		if strings.Contains(h.target, "t-test") {
			t.Fatalf("the token was in the request target %q", h.target)
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
	f.hangUp(t)
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

// A call cancelled while its frame is being written must fail alone. The
// earlier version of this test cancelled before the call began, which
// coder/websocket never sees: it passed on the code the review broke. Here the
// peer does not read for a second, the payload is far larger than any socket
// buffer, and the caller gives up in the middle of the write — the case where
// the write's context was the caller's, and that context closing the
// connection took every other call with it.
func TestCancelledCallLeavesTheConnectionUp(t *testing.T) {
	release := make(chan struct{})
	reading := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(64 << 20)
		// Do not read for a second: the client's write fills the socket and
		// then blocks inside the write.
		close(reading)
		time.Sleep(time.Second)
		close(release)
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	c, err := Dial(context.Background(), Options{
		URL:            "ws" + strings.TrimPrefix(server.URL, "http") + "/rpc",
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-reading

	// A payload far larger than the socket buffer, with a context the caller
	// cancels 100 ms in — while the write is still going.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.Call(ctx, MethodIndexUpdate, map[string]any{"id": 1, "title": strings.Repeat("x", 16<<20)}, nil)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a call cancelled mid-write succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled call never returned: the writer waited out the payload")
	}
	select {
	case <-c.Done():
		t.Fatalf("one cancelled call ended the connection: %v", c.Err())
	case <-time.After(100 * time.Millisecond):
	}
	if err := c.Err(); err != nil {
		t.Fatalf("the connection is broken after a cancelled call: %v", err)
	}
}

// A whole view supersedes an earlier one of the same conversation that the
// reader has not taken yet, and only that: a delta push's ops and a "full"
// push are steps a reader must apply in order, so they are neither merged nor
// moved.
func TestNotificationOrderSurvivesFullAndDeltaPushes(t *testing.T) {
	f := startFakeKernel(t)
	// One slot and no reader: the push being delivered blocks, and everything
	// after it waits in the queue.
	c, err := Dial(context.Background(), Options{URL: f.url(), Token: "t-test", RequestTimeout: 5 * time.Second, NotesBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	full := map[string]any{"id": 7, "kind": ViewKindFull, "value": map[string]any{"conversation": map[string]any{"id": 7}, "entries": []any{}}}
	delta := func(op string) map[string]any {
		return map[string]any{"id": 7, "kind": ViewKindValue, "value": nil, "ops": []any{op}}
	}
	f.notify(t, NotifyConversationView, full)
	f.notify(t, NotifyWarning, map[string]any{"kind": "extension_error", "message": "boom"})
	f.notify(t, NotifyConversationView, delta("op1"))
	f.notify(t, NotifyConversationView, delta("op2"))

	type got struct {
		method string
		kind   string
		ops    string
	}
	var seen []got
	for len(seen) < 4 {
		select {
		case n := <-c.Notifications():
			var view struct {
				Kind string          `json:"kind"`
				Ops  json.RawMessage `json:"ops"`
			}
			_ = json.Unmarshal(n.Params, &view)
			seen = append(seen, got{n.Method, view.Kind, string(view.Ops)})
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of 4 notifications arrived: %+v", len(seen), seen)
		}
	}
	want := []got{
		{NotifyConversationView, ViewKindFull, ""},
		{NotifyWarning, "", ""},
		{NotifyConversationView, ViewKindValue, `["op1"]`},
		{NotifyConversationView, ViewKindValue, `["op2"]`},
	}
	for i := range want {
		if seen[i].method != want[i].method {
			t.Fatalf("notification %d is %s, want %s (got %+v)", i, seen[i].method, want[i].method, seen)
		}
		if want[i].kind != "" && seen[i].kind != want[i].kind {
			t.Fatalf("notification %d has kind %q, want %q (got %+v)", i, seen[i].kind, want[i].kind, seen)
		}
		if want[i].ops != "" && seen[i].ops != want[i].ops {
			t.Fatalf("notification %d has ops %s, want %s (got %+v)", i, seen[i].ops, want[i].ops, seen)
		}
	}
}

// A whole view supersedes an earlier queued whole view of the same
// conversation, and only such a push: coalescing anything else loses a step
// the reader cannot rebuild. The queue is held still here, so the rule is
// checked rather than raced.
func TestWholeViewsCoalesceInTheQueue(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	note := func(params map[string]any) Notification {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		return Notification{Method: NotifyConversationView, Params: raw}
	}
	whole := func(id int, marker string) Notification {
		return note(map[string]any{"id": id, "kind": ViewKindValue, "value": map[string]any{"before": marker}})
	}
	delta := func(op string) Notification {
		return note(map[string]any{"id": 7, "kind": ViewKindValue, "value": nil, "ops": []any{op}})
	}
	full := note(map[string]any{"id": 7, "kind": ViewKindFull, "value": map[string]any{"before": "full"}})

	c.queuedMu.Lock()
	defer c.queuedMu.Unlock()

	c.queued = []Notification{whole(7, "a")}
	if _, ok := c.supersedesLocked(whole(7, "b")); !ok {
		t.Fatal("a whole view did not supersede an earlier one of its conversation")
	}
	if got := len(c.queued); got != 1 {
		t.Fatalf("the queue holds %d entries after a supersede, want 1", got)
	}
	// Another conversation's view is a different state and keeps its place.
	c.queued = []Notification{whole(8, "other")}
	if _, ok := c.supersedesLocked(whole(7, "b")); ok {
		t.Fatal("a view superseded another conversation's")
	}

	// A delta push's ops are a sequence: neither may be replaced.
	c.queued = []Notification{delta("op1")}
	if _, ok := c.supersedesLocked(delta("op2")); ok {
		t.Fatal("a delta push superseded another: its ops would be lost")
	}
	// A "full" push is a reset the reader must see.
	c.queued = []Notification{full}
	if _, ok := c.supersedesLocked(whole(7, "after-full")); ok {
		t.Fatal("a whole view replaced a full push, which resets a delta reader")
	}
	// And a whole view is not replaced by a delta push or a full push either.
	c.queued = []Notification{whole(7, "a")}
	if _, ok := c.supersedesLocked(full); ok {
		t.Fatal("a full push replaced a queued whole view")
	}
}

// Nothing that reaches the queue is dropped for want of room: a notification
// that cannot be rebuilt — an approval, an ask, a lifecycle change — would leave
// a session waiting forever, and a whole view is the only thing a reader that
// fell behind cannot rebuild either. The cap is a backlog threshold, and past it
// the queue keeps everything.
func TestMustArriveNotificationsAreNeverDropped(t *testing.T) {
	f := startFakeKernel(t)
	c, err := Dial(context.Background(), Options{
		URL: f.url(), Token: "t-test", RequestTimeout: 5 * time.Second,
		NotesBuffer: 1, MaxQueuedNotes: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	view := func(id int, marker string) map[string]any {
		return map[string]any{"id": id, "kind": ViewKindValue, "value": map[string]any{"conversation": map[string]any{"id": id}, "before": marker}}
	}
	// The policy is exercised where it can be counted: enqueue directly, with
	// the reader's one slot already full so nothing drains the queue.
	say := func(method string, params any) {
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		c.enqueue(Notification{Method: method, Params: raw})
	}
	say(NotifyConversationView, view(1, "held"))
	for deadline := time.Now().Add(5 * time.Second); len(c.notes) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the first view never reached the reader's slot")
		}
		time.Sleep(time.Millisecond)
	}
	// Eight more views of conversations of their own: none coalesces with
	// another, so the queue grows past its cap rather than lose the last state
	// of eight conversations — and the approval and the ask that follow are
	// kept too, in the order they arrived.
	for id := 2; id <= 9; id++ {
		say(NotifyConversationView, view(id, "waiting"))
	}
	say(NotifyApprovalRequested, map[string]any{"requestId": "a-1", "id": 2, "tool": "bash", "args": map[string]any{"command": "rm -rf build"}})
	say(NotifyAskRequested, map[string]any{"requestId": "q-1", "id": 2, "questions": []any{}, "context": "why"})

	sawApproval, sawAsk, views := false, false, map[string]bool{}
	deadline := time.Now().Add(5 * time.Second)
	for (!sawApproval || !sawAsk || len(views) < 8) && time.Now().Before(deadline) {
		select {
		case n := <-c.Notifications():
			switch n.Method {
			case NotifyApprovalRequested:
				sawApproval = true
			case NotifyAskRequested:
				sawAsk = true
			case NotifyConversationView:
				var params struct {
					ID    int `json:"id"`
					Value struct {
						Before string `json:"before"`
					} `json:"value"`
				}
				if json.Unmarshal(n.Params, &params) == nil && params.Value.Before == "waiting" {
					views[strconv.Itoa(params.ID)] = true
				}
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawApproval || !sawAsk {
		t.Fatalf("the approval arrived: %v, the ask: %v — neither may be dropped", sawApproval, sawAsk)
	}
	// Eight conversations that were not coalesced away, each with its own final
	// state: a reader behind loses none of them, and the cap only counts.
	if len(views) != 8 {
		t.Fatalf("%d of the eight waiting views arrived: %v", len(views), views)
	}
	c.queuedMu.Lock()
	over := c.over
	c.queuedMu.Unlock()
	if over == 0 {
		t.Fatal("the cap was never reached, so the policy was not exercised")
	}
}

// A whole view is the conversation's last word, so a full queue grows for it
// rather than lose it: an idle conversation whose final state was dropped would
// never be rebuilt, and nothing would trigger the rebuild.
func TestAWholeViewSurvivesAFullQueue(t *testing.T) {
	c := &Client{opts: Options{MaxQueuedNotes: 2}, wake: make(chan struct{}, 1)}
	note := func(kind, value string) Notification {
		return Notification{Method: NotifyConversationView, Params: json.RawMessage(
			`{"id":7,"kind":"` + kind + `","value":` + value + `}`)}
	}
	// A reset and a delta are steps the reader must apply, so neither can be
	// traded for room.
	c.enqueue(note("full", `{"before":"reset"}`))
	c.enqueue(note("value", `null`))
	latest := note("value", `{"before":"latest"}`)
	c.enqueue(latest)
	if got := len(c.queued); got != 3 {
		t.Fatalf("queue holds %d notifications, want the full push, the delta and the latest view", got)
	}
	if last := c.queued[len(c.queued)-1]; string(last.Params) != string(latest.Params) {
		t.Fatalf("the latest view is not last: %+v", c.queued)
	}
	// The next push of the same conversation replaces it in place, so the
	// growth stays one view per conversation.
	again := note("value", `{"before":"again"}`)
	c.enqueue(again)
	if got := len(c.queued); got != 3 {
		t.Fatalf("a second view of the same conversation grew the queue to %d", got)
	}
	if last := c.queued[2]; string(last.Params) != string(again.Params) {
		t.Fatalf("the newest view did not replace the one before it: %+v", c.queued)
	}
}

// A consumer that stops reading notifications must not stop the answers: the
// read loop serves both, and a slow consumer is not Kernel's fault.
func TestSlowNotificationReaderDoesNotBlockAnswers(t *testing.T) {
	f := startFakeKernel(t)
	c, err := Dial(context.Background(), Options{URL: f.url(), Token: "t-test", RequestTimeout: 3 * time.Second, NotesBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Nobody reads Notifications(); Kernel sends five and then the answer.
	done := make(chan error, 1)
	go func() {
		_, err := c.Harness(context.Background())
		done <- err
	}()
	sent := f.next(t)
	for i := 0; i < 5; i++ {
		f.notify(t, NotifyWarning, map[string]any{"kind": "extension_error", "message": "boom"})
	}
	f.reply(t, sent.ID, map[string]any{"version": "kernel-1.5.0-1"})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the answer never arrived with nobody reading notifications: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the answer never arrived: a slow notification reader blocked the read loop")
	}
	// The notifications are still there, in order, for a reader that comes
	// late.
	for i := 0; i < 5; i++ {
		select {
		case n := <-c.Notifications():
			if n.Method != NotifyWarning {
				t.Fatalf("notification %d is %s", i, n.Method)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d notifications were kept", i)
		}
	}
}

// A peer that stops reading must not hold the writer forever: the call fails
// on its own write timeout, and the calls behind it are not stuck behind the
// write mutex.
func TestStalledPeerDoesNotHoldTheWriter(t *testing.T) {
	release := make(chan struct{})
	stalled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		// Read nothing until the test releases it: the client's writes fill
		// the socket buffer and then block inside the write.
		close(stalled)
		<-release
	}))
	defer server.Close()
	defer close(release)
	c, err := Dial(context.Background(), Options{
		URL:            "ws" + strings.TrimPrefix(server.URL, "http") + "/rpc",
		RequestTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-stalled
	// A payload far larger than any socket buffer, twice: the first write
	// blocks in the socket, the second waits on the write mutex.
	big := strings.Repeat("x", 8<<20)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- c.Call(context.Background(), MethodIndexUpdate, map[string]any{"id": 1, "title": big}, nil)
		}()
	}
	deadline := time.After(15 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a call to a peer that never read its frame succeeded")
			}
		case <-deadline:
			t.Fatal("a call to a peer that stopped reading never returned: the writer is held forever")
		}
	}
}

// A slow reader that falls far behind must not lose a step that cannot be
// rebuilt: an approval or an ask lost leaves a session waiting forever, and a
// delta push's ops lost leaves the state wrong. Whole views are what give way.
//
// The reader's slot is filled first, so nothing drains the queue while the
// flood arrives.
func TestAMustArriveFloodKeepsEveryStep(t *testing.T) {
	f := startFakeKernel(t)
	c, err := Dial(context.Background(), Options{
		URL: f.url(), Token: "t-test", RequestTimeout: 5 * time.Second,
		NotesBuffer: 1, MaxQueuedNotes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	say := func(method string, params any) {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		c.enqueue(Notification{Method: method, Params: raw})
	}
	delta := func(op string) map[string]any {
		return map[string]any{"id": 7, "kind": ViewKindValue, "value": nil, "ops": []any{op}}
	}
	say(NotifyWarning, map[string]any{"kind": "extension_error", "message": "hold the slot"})
	for deadline := time.Now().Add(5 * time.Second); len(c.notes) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the first notification never reached the reader's slot")
		}
		time.Sleep(time.Millisecond)
	}
	// More than the cap of steps that must all arrive.
	const ops = 10
	for i := 0; i < ops; i++ {
		say(NotifyConversationView, delta(fmt.Sprintf("op%d", i)))
	}
	say(NotifyApprovalRequested, map[string]any{"requestId": "a-1", "id": 7, "tool": "bash"})
	say(NotifyAskRequested, map[string]any{"requestId": "q-1", "id": 7, "questions": []any{}, "context": "why"})

	deadline := time.Now().Add(10 * time.Second)
	seen := map[string]bool{}
	for !(seen["a-1"] && seen["q-1"]) && time.Now().Before(deadline) {
		select {
		case n := <-c.Notifications():
			var p struct {
				RequestID string `json:"requestId"`
				Ops       []any  `json:"ops"`
			}
			_ = json.Unmarshal(n.Params, &p)
			if p.RequestID != "" {
				seen[p.RequestID] = true
			}
			for _, op := range p.Ops {
				seen[fmt.Sprint(op)] = true
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !seen["a-1"] || !seen["q-1"] {
		t.Fatalf("the approval arrived: %v, the ask: %v; neither may be dropped", seen["a-1"], seen["q-1"])
	}
	for i := 0; i < ops; i++ {
		if !seen[fmt.Sprintf("op%d", i)] {
			t.Fatalf("op%d was dropped; a delta reader cannot rebuild it (%v)", i, seen)
		}
	}
}

// An answer with no id is the contract's parse error: the only call in flight
// must hear it at once, not wait out its timeout.
func TestClientFailsTheOnlyCallOnAnAnswerWithoutAnId(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := c.Harness(context.Background())
		done <- err
	}()
	f.next(t)
	// A frame Kernel could not parse costs this call, and it cannot name it.
	f.write(t, outFrame{JSONRPC: "2.0", Error: &RPCError{Code: CodeParseError, Message: "parse error"}})
	select {
	case err := <-done:
		if !Is(err, CodeParseError) {
			t.Fatalf("the call answered %v, want -32700", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the call is still waiting after a parse error")
	}
}

// An answer without an id while no call is in flight is dropped, and the
// connection stays up for the next call.
func TestClientDropsAnAnswerWithoutAnIdWhenIdle(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	f.write(t, outFrame{JSONRPC: "2.0", Error: &RPCError{Code: CodeParseError, Message: "parse error"}})
	time.Sleep(100 * time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, err := c.Harness(context.Background())
		done <- err
	}()
	sent := f.next(t)
	f.reply(t, sent.ID, map[string]any{"version": "kernel-1.5.0-1"})
	if err := <-done; err != nil {
		t.Fatalf("the call after a dropped frame: %v", err)
	}
}

// kernel.shutdown answers first and closes after, so the call returns while
// the close is still running: the caller waits on Done, not on the call.
func TestShutdownAnswersWhileTheCloseRuns(t *testing.T) {
	f := startFakeKernel(t)
	c := dialFake(t, f)
	type outcome struct {
		result ShutdownResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := c.Shutdown(context.Background())
		done <- outcome{result, err}
	}()
	sent := f.next(t)
	if sent.Method != MethodKernelShutdown {
		t.Fatalf("sent %s", sent.Method)
	}
	if len(sent.Params) > 0 {
		t.Fatalf("kernel.shutdown takes no params, sent %s", sent.Params)
	}
	f.reply(t, sent.ID, map[string]any{"status": ShutdownClosing})
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.result.Status != ShutdownClosing {
			t.Fatalf("status = %q", got.result.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the shutdown call never answered")
	}
	// Kernel then goes away, as it does after answering.
	f.hangUp(t)
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the connection did not end after Kernel closed")
	}
}
