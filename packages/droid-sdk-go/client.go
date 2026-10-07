// Package droid is a Go client for the Droid Daemon (`droid daemon`): the
// JSON-RPC protocol over a WebSocket that Factory's TypeScript SDK
// (@factory/droid-sdk) speaks. Client is one authenticated connection; the
// session package builds Sessions, their state and reconnection on top.
package droid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

const (
	jsonRPCVersion = "2.0"
	// The Daemon still checks the legacy API version field.
	legacyAPIVersion = "1.0.0"

	defaultRequestTimeout = 30 * time.Second
	defaultConnectTimeout = 5 * time.Second
	// Daemon messages carry whole transcripts; the TS client sets no limit.
	readLimit = 256 << 20
)

// Options configure a connection.
type Options struct {
	// URL of the Daemon's WebSocket, as ws://127.0.0.1:<port>, or of a
	// Gateway that forwards to it.
	URL string
	// Credential authenticates the connection (daemon.authenticate). Leave
	// it nil to connect without authenticating, as a trusted transport does.
	Credential *Credential
	// Caller and Surface say which kind of client this is; they default to
	// "go-sdk" and "sdk". The Daemon refuses caller "sdk" without SDK
	// metadata, whose language can only be typescript or python.
	Caller  string
	Surface string
	// RequestTimeout bounds a request that its context does not; 30s by
	// default. Some methods take longer (protocol.Methods).
	RequestTimeout time.Duration
	// ConnectTimeout bounds opening the socket; 5s by default.
	ConnectTimeout time.Duration
	// Header is sent with the WebSocket handshake.
	Header http.Header
	// Record, when set, sees every frame as it is sent or received, for
	// tests and diagnosis. It must not keep the slice.
	Record func(dir Direction, frame []byte)
	// BeforeCall, when set, runs before every request with its method and
	// the sessionId of its params ("" when it has none); an error fails the
	// request.
	BeforeCall func(ctx context.Context, method, sessionID string) error
	Logger     *slog.Logger
}

// Credential is what daemon.authenticate accepts: a Factory API key or an
// access token.
type Credential struct {
	APIKey string
	Token  string
}

// Direction tells a recorded frame's way.
type Direction int

const (
	Sent Direction = iota
	Received
)

func (d Direction) String() string {
	if d == Sent {
		return "sent"
	}
	return "received"
}

// Notification is a message the Daemon sends unasked.
type Notification struct {
	Method string
	Params json.RawMessage
}

// RequestHandler answers a request the Daemon sends the Client (a permission
// or AskUser prompt). The result is sent back as the response.
type RequestHandler func(ctx context.Context, params json.RawMessage) (any, error)

// Request is a request the Daemon sent the Client.
type Request struct {
	ID     string
	Method string
	Params json.RawMessage
}

// Client is one connection to the Daemon. Its methods are safe from any
// goroutine.
type Client struct {
	opts   Options
	conn   *websocket.Conn
	log    *slog.Logger
	ctx    context.Context
	cancel context.CancelCauseFunc

	writeMu sync.Mutex

	mu       sync.Mutex
	pending  map[string]pendingCall
	acks     map[string]ackWait
	handlers map[string]RequestHandler
	later    map[string]func(Request)
	subs     map[int]func(Notification)
	nextSub  int
	done     chan struct{}
	err      error
}

// ackWait is an acknowledged request waiting for the session notification
// of type completedBy that carries its id.
type ackWait struct {
	ch          chan struct{}
	completedBy string
}

type pendingCall struct {
	ch   chan response
	hook func(json.RawMessage)
}

type response struct {
	result json.RawMessage
	err    error
}

// Dial opens a connection and, when opts.Credential is set, authenticates
// it.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = defaultRequestTimeout
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = defaultConnectTimeout
	}
	if opts.Caller == "" {
		opts.Caller = "go-sdk"
	}
	if opts.Surface == "" {
		opts.Surface = "sdk"
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	dialCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()
	conn, resp, err := websocket.Dial(dialCtx, opts.URL, &websocket.DialOptions{HTTPHeader: opts.Header})
	if err != nil {
		if resp != nil {
			return nil, &DialError{StatusCode: resp.StatusCode, Err: err}
		}
		return nil, &DialError{Err: err}
	}
	conn.SetReadLimit(readLimit)
	cctx, ccancel := context.WithCancelCause(context.Background())
	c := &Client{
		opts:     opts,
		conn:     conn,
		log:      log,
		ctx:      cctx,
		cancel:   ccancel,
		pending:  map[string]pendingCall{},
		acks:     map[string]ackWait{},
		handlers: map[string]RequestHandler{},
		later:    map[string]func(Request){},
		subs:     map[int]func(Notification){},
		done:     make(chan struct{}),
	}
	go c.readLoop()
	if opts.Credential != nil {
		if err := c.authenticate(ctx); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) authenticate(ctx context.Context) error {
	p := protocol.AuthenticateParams{
		Caller: c.opts.Caller,
		// No SDK metadata: the protocol only knows TypeScript and Python SDKs.
		Metadata: &protocol.DaemonConnectionMetadata{
			Tracing: &protocol.DaemonConnectionMetadataTracing{App: c.opts.Surface, MachineType: "local"},
		},
	}
	if c.opts.Credential.APIKey != "" {
		p.APIKey = c.opts.Credential.APIKey
	} else {
		p.Token = c.opts.Credential.Token
	}
	if _, err := c.Authenticate(ctx, p); err != nil {
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == CodeAuthenticationError {
			return &AuthError{Err: err}
		}
		return fmt.Errorf("authenticate: %w", err)
	}
	return nil
}

// Version is this package's release, sent to the Daemon as the SDK version.
const Version = "0.1.0"

// Call sends a request and decodes its result into result (when not nil).
// For a method the Daemon acknowledges first (protocol.Methods), Call waits
// for the notification that completes it.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	info := protocol.Methods[method]
	timeout := c.opts.RequestTimeout
	if info.Timeout > 0 {
		timeout = info.Timeout
	}
	parent := ctx
	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	if id == "" {
		id = uuid.NewString()
	}
	req := map[string]any{
		"type":                   "request",
		"jsonrpc":                jsonRPCVersion,
		"factoryApiVersion":      legacyAPIVersion,
		"factoryProtocolVersion": protocol.FactoryProtocolVersion,
		"id":                     id,
		"method":                 method,
	}
	if params != nil {
		req["params"] = params
	}
	if c.opts.BeforeCall != nil {
		if err := c.opts.BeforeCall(ctx, method, sessionIDOf(params)); err != nil {
			return err
		}
	}
	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("%s: encode: %w", method, err)
	}
	ch := make(chan response, 1)
	var ack chan struct{}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	hook, _ := ctx.Value(responseHookKey{}).(func(json.RawMessage))
	c.pending[id] = pendingCall{ch: ch, hook: hook}
	if info.Ack {
		ack = make(chan struct{}, 1)
		c.acks[id] = ackWait{ch: ack, completedBy: info.CompletedBy}
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		delete(c.acks, id)
		c.mu.Unlock()
	}()
	if err := c.write(ctx, b); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var res response
	select {
	case res = <-ch:
	case <-ctx.Done():
		return &TimeoutError{Method: method, Err: ctx.Err()}
	case <-c.done:
		return c.Err()
	}
	if res.err != nil {
		return res.err
	}
	if ack != nil && isAccepted(res.result) {
		// As the TS client does, the wait for completion gets its own
		// timeout, counted from the acknowledgement.
		if !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(parent, timeout)
			defer cancel()
		}
		select {
		case <-ack:
			res.result = json.RawMessage("{}")
		case <-ctx.Done():
			return &TimeoutError{Method: method, Err: ctx.Err()}
		case <-c.done:
			return c.Err()
		}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(res.result, result); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	return nil
}

type responseHookKey struct{}

// WithResponseHook makes Call run fn with the result of the request it sends
// with ctx, on the read goroutine, as the response arrives and before any
// message that follows it is handled. fn must not block.
func WithResponseHook(ctx context.Context, fn func(result json.RawMessage)) context.Context {
	return context.WithValue(ctx, responseHookKey{}, fn)
}

type requestIDKey struct{}

// WithRequestID makes the request Call sends with ctx carry id, which the
// notifications it causes refer to (as create_message's requestId).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDOf is the id WithRequestID put on ctx, or "".
func RequestIDOf(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func sessionIDOf(params any) string {
	if params == nil {
		return ""
	}
	b, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	var v struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(b, &v)
	return v.SessionID
}

func isAccepted(raw json.RawMessage) bool {
	var v struct {
		Accepted bool `json:"accepted"`
	}
	return json.Unmarshal(raw, &v) == nil && v.Accepted
}

// Handle sets the handler of a request the Daemon sends (see
// protocol.ServerRequest*). A request without a handler is answered with
// an error.
func (c *Client) Handle(method string, h RequestHandler) {
	c.mu.Lock()
	c.handlers[method] = h
	c.mu.Unlock()
}

// HandleLater sets fn to receive the requests of method without answering
// them: answer each with Respond. fn runs on the read goroutine and must not
// block.
func (c *Client) HandleLater(method string, fn func(Request)) {
	c.mu.Lock()
	c.later[method] = fn
	c.mu.Unlock()
}

// Respond sends the result of a request the Daemon sent. The Daemon also
// takes the answer to a request it sent another connection, as one restored
// by daemon.load_session.
func (c *Client) Respond(ctx context.Context, id string, result any) error {
	b, err := json.Marshal(map[string]any{
		"type":                   "response",
		"jsonrpc":                jsonRPCVersion,
		"factoryApiVersion":      legacyAPIVersion,
		"factoryProtocolVersion": protocol.FactoryProtocolVersion,
		"id":                     id,
		"result":                 result,
	})
	if err != nil {
		return err
	}
	if err := c.Err(); err != nil {
		return err
	}
	return c.write(ctx, b)
}

// Subscribe calls fn with every notification, on the connection's read
// goroutine, in order; fn must not block. The returned function
// unsubscribes.
func (c *Client) Subscribe(fn func(Notification)) (unsubscribe func()) {
	c.mu.Lock()
	id := c.nextSub
	c.nextSub++
	c.subs[id] = fn
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.subs, id)
		c.mu.Unlock()
	}
}

// Done is closed when the connection ends; Err then says why.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended, or nil while it is open.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the connection.
func (c *Client) Close() error {
	c.fail(ErrClosed)
	return c.conn.Close(websocket.StatusNormalClosure, "client disconnect")
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	close(c.done)
	c.mu.Unlock()
	c.cancel(err)
}

func (c *Client) write(ctx context.Context, b []byte) error {
	if c.opts.Record != nil {
		c.opts.Record(Sent, b)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, b)
}

// envelope is any message the Daemon sends.
type envelope struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

func (c *Client) readLoop() {
	for {
		_, b, err := c.conn.Read(c.ctx)
		if err != nil {
			if cause := context.Cause(c.ctx); cause != nil {
				err = cause
			} else {
				err = &ClosedError{Status: websocket.CloseStatus(err), Err: err}
			}
			c.fail(err)
			return
		}
		if c.opts.Record != nil {
			c.opts.Record(Received, b)
		}
		c.dispatch(b)
	}
}

func (c *Client) dispatch(b []byte) {
	var m envelope
	if err := json.Unmarshal(b, &m); err != nil {
		c.log.Warn("droid: undecodable message", "err", err)
		return
	}
	switch {
	case m.Type == "notification" || (m.ID == "" && m.Method != ""):
		c.completeAck(m)
		c.mu.Lock()
		subs := make([]func(Notification), 0, len(c.subs))
		for _, fn := range c.subs {
			subs = append(subs, fn)
		}
		c.mu.Unlock()
		n := Notification{Method: m.Method, Params: m.Params}
		for _, fn := range subs {
			fn(n)
		}
	case m.Type == "request" || (m.ID != "" && m.Method != ""):
		c.mu.Lock()
		fn := c.later[m.Method]
		c.mu.Unlock()
		if fn != nil {
			fn(Request{ID: m.ID, Method: m.Method, Params: m.Params})
			return
		}
		go c.answer(m)
	case m.ID != "":
		c.mu.Lock()
		p, ok := c.pending[m.ID]
		c.mu.Unlock()
		if !ok {
			return
		}
		switch {
		case m.Error != nil:
			p.ch <- response{err: m.Error}
		case m.Result == nil:
			p.ch <- response{err: &RPCError{Code: -32600, Message: "Response missing result field"}}
		default:
			if p.hook != nil {
				p.hook(m.Result)
			}
			p.ch <- response{result: m.Result}
		}
	}
}

// completeAck ends the wait of an acknowledged request whose completing
// session notification arrived.
func (c *Client) completeAck(m envelope) {
	if m.Method != protocol.NotificationSessionNotification {
		return
	}
	c.mu.Lock()
	n := len(c.acks)
	c.mu.Unlock()
	if n == 0 {
		return
	}
	var p struct {
		Notification struct {
			Type      string `json:"type"`
			RequestID string `json:"requestId"`
		} `json:"notification"`
	}
	if json.Unmarshal(m.Params, &p) != nil || p.Notification.RequestID == "" {
		return
	}
	c.mu.Lock()
	w, ok := c.acks[p.Notification.RequestID]
	c.mu.Unlock()
	if !ok || w.completedBy != p.Notification.Type {
		return
	}
	select {
	case w.ch <- struct{}{}:
	default:
	}
}

func (c *Client) answer(m envelope) {
	c.mu.Lock()
	h := c.handlers[m.Method]
	c.mu.Unlock()
	out := map[string]any{
		"type":                   "response",
		"jsonrpc":                jsonRPCVersion,
		"factoryApiVersion":      legacyAPIVersion,
		"factoryProtocolVersion": protocol.FactoryProtocolVersion,
		"id":                     m.ID,
	}
	if h == nil {
		out["error"] = RPCError{Code: -32601, Message: "no handler for " + m.Method}
	} else if result, err := h(c.ctx, m.Params); err != nil {
		out["error"] = RPCError{Code: -32603, Message: err.Error()}
	} else {
		out["result"] = result
	}
	b, err := json.Marshal(out)
	if err != nil {
		c.log.Warn("droid: encode response", "method", m.Method, "err", err)
		return
	}
	if err := c.write(c.ctx, b); err != nil {
		c.log.Warn("droid: send response", "method", m.Method, "err", err)
	}
}
