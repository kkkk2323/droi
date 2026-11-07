package kernel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ErrClosed is why a call fails once the Client is closed.
var ErrClosed = errors.New("kernel: the client is closed")

const (
	defaultRequestTimeout = 30 * time.Second
	// defaultMaxMessage bounds one frame: a view with several pictures is the
	// largest thing Kernel sends.
	defaultMaxMessage  = 64 << 20
	defaultNotesBuffer = 256
	// closeUnauthorized is the WebSocket close code the contract gives a
	// wrong or missing token that got as far as the upgrade.
	closeUnauthorized = websocket.StatusCode(4401)
	// defaultWriteTimeout bounds one write to the socket.
	defaultWriteTimeout = 10 * time.Second
)

// Options say how to reach a Kernel.
type Options struct {
	// URL is Kernel's WebSocket address, kernel.URL(port).
	URL string
	// Token is the token Kernel was launched with, in its DROI_KERNEL_TOKEN;
	// Dial sends it as a Bearer token.
	Token string
	// RequestTimeout bounds one call; 0 is 30 s. Every method answers at once
	// — none waits on a task — so a call that outlasts this is a Kernel that
	// stopped answering.
	RequestTimeout time.Duration
	// MaxMessage bounds one incoming frame; 0 is 64 MiB.
	MaxMessage int64
	// NotesBuffer is how many notifications are handed to the reader at once;
	// 0 is 256. A reader that stops reading never stops the answers: the read
	// loop queues notifications without waiting, and the queue is bounded per
	// kind, so a slow consumer costs a known amount of memory, not calls. One
	// goroutine reads the queue in order, so a reader sees Kernel's news as
	// Kernel sent it, with the states it superseded folded away.
	NotesBuffer int
	// WriteTimeout bounds one write to the socket; 0 is 10 s. Kernel not
	// reading its frames is a broken connection, and without this the caller
	// waiting on the write mutex would wait forever.
	WriteTimeout time.Duration
	// Log, optional, hears what the Client drops: a frame it cannot read, an
	// answer to a call that is gone.
	Log func(string)
}

// Client is one connection to a Kernel: the contract's methods as Go calls, and
// its notifications in order on Notifications.
//
// A Client is one connection's view of Kernel: conversation.view arrives only
// for the conversations it opened, and Close (or losing the connection)
// unwatches them. Approval and ask notifications reach every connection, so a
// Client that answers first wins and the others get CodeRequestGone.
//
// A Client does not reconnect: Kernel is a child process, so when it exits the
// Client is done (Done) and the Supervisor starts a new Kernel.
type Client struct {
	conn *websocket.Conn
	opts Options

	writeMu sync.Mutex
	seq     atomic.Int64

	mu      sync.Mutex
	pending map[string]chan *rpcFrame
	opened  []ID
	err     error

	// queue holds notifications the read loop took but the reader has not been
	// given yet, folded by kind and bounded per kind, so a reader that stops
	// reading costs a known amount of memory and never stops the read loop.
	queue *notifQueue
	wake  chan struct{}

	notes chan Notification
	done  chan struct{}
	stop  sync.Once
}

// Dial connects to Kernel. The context bounds the handshake only; the
// connection lives until Close.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	if opts.URL == "" {
		return nil, errors.New("kernel: no URL")
	}
	header := http.Header{}
	if opts.Token != "" {
		// The contract names one place for the token, the Authorization
		// header. It does not go in the URL either: a query string reaches
		// Kernel's own request log and anything that logs a URL on the way.
		header.Set("Authorization", "Bearer "+opts.Token)
	}
	conn, res, err := websocket.Dial(ctx, opts.URL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if res != nil {
			return nil, fmt.Errorf("kernel: dial: %w (HTTP %d)", err, res.StatusCode)
		}
		return nil, fmt.Errorf("kernel: dial: %w", err)
	}
	conn.SetReadLimit(opts.maxMessage())
	c := &Client{
		conn:    conn,
		opts:    opts,
		pending: map[string]chan *rpcFrame{},
		queue:   newNotifQueue(),
		wake:    make(chan struct{}, 1),
		notes:   make(chan Notification, opts.notesBuffer()),
		done:    make(chan struct{}),
	}
	go c.deliver()
	go c.read()
	return c, nil
}

func (o Options) writeTimeout() time.Duration {
	if o.WriteTimeout > 0 {
		return o.WriteTimeout
	}
	return defaultWriteTimeout
}

func (o Options) maxMessage() int64 {
	if o.MaxMessage > 0 {
		return o.MaxMessage
	}
	return defaultMaxMessage
}

func (o Options) notesBuffer() int {
	if o.NotesBuffer > 0 {
		return o.NotesBuffer
	}
	return defaultNotesBuffer
}

func (o Options) timeout() time.Duration {
	if o.RequestTimeout != 0 {
		return o.RequestTimeout
	}
	return defaultRequestTimeout
}

// Notifications is every notification Kernel sends this connection, in order,
// folded and bounded as the queue describes: a conversation's states arrive as
// the newest one, and a flood the reader cannot keep up with is folded (states)
// or counted (warnings) rather than queued.
//
// The channel closes when the connection does; Done and Err say why. Two kinds
// of flood end the connection instead of dropping what cannot be rebuilt: more
// approvals and asks than the queue holds, and more delta steps than a reader
// could apply — reconnecting opens the conversations and starts from a whole
// view.

func (c *Client) Notifications() <-chan Notification { return c.notes }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is why the connection ended: nil when Close asked for it.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Opened is the conversations this connection watches, in the order it opened
// them. A Kernel that restarted has forgotten them, so a Supervisor restarts
// its Clients and they open these again.
func (c *Client) Opened() []ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ID(nil), c.opened...)
}

// watch notes that this connection watches id.
func (c *Client) watch(id ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, have := range c.opened {
		if have == id {
			return
		}
	}
	c.opened = append(c.opened, id)
}

// forget stops counting id as watched, "" only when Kernel took the unwatch.
func (c *Client) forget(id ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, have := range c.opened {
		if have == id {
			c.opened = append(c.opened[:i], c.opened[i+1:]...)
			return
		}
	}
}

// Close ends the connection, which unwatches everything it opened. Closing
// twice is safe: the second answer is nil.
func (c *Client) Close() error {
	// Shut first, so a waiting call sees ErrClosed whether or not the close
	// handshake reaches Kernel.
	c.shut(nil)
	if err := c.conn.Close(websocket.StatusNormalClosure, ""); err != nil && c.Err() == nil {
		return err
	}
	return nil
}

func (c *Client) shut(err error) {
	c.stop.Do(func() {
		c.mu.Lock()
		if c.err == nil {
			c.err = err
		}
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *Client) logf(format string, args ...any) {
	if c.opts.Log != nil {
		c.opts.Log(fmt.Sprintf(format, args...))
	}
}

func (c *Client) read() {
	for {
		typ, data, err := c.conn.Read(context.Background())
		if err != nil {
			if websocket.CloseStatus(err) == closeUnauthorized {
				c.shut(errTokenRefused)
			} else {
				c.shut(err)
			}
			return
		}
		if typ != websocket.MessageText {
			c.logf("kernel: dropped a %v frame", typ)
			continue
		}
		c.handle(data)
	}
}

// errTokenRefused is why Kernel closed a connection: the token did not match.
var errTokenRefused = errors.New("kernel: the token was refused")

// handle reads one frame, or a JSON-RPC batch of them.
func (c *Client) handle(data []byte) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return
	}
	if trimmed[0] == '[' {
		var frames []rpcFrame
		if err := json.Unmarshal(trimmed, &frames); err != nil {
			c.logf("kernel: dropped a frame: %v", err)
			return
		}
		for _, f := range frames {
			c.dispatch(f)
		}
		return
	}
	var f rpcFrame
	if err := json.Unmarshal(trimmed, &f); err != nil {
		c.logf("kernel: dropped a frame: %v", err)
		return
	}
	c.dispatch(f)
}

func (c *Client) dispatch(f rpcFrame) {
	if f.Method != "" {
		// The read loop never waits for the reader: a consumer that is busy
		// handling a notification and calls back in must not deadlock, and a
		// consumer that stops reading must not stall the answers.
		c.enqueue(Notification{Method: f.Method, Params: f.Params})
		return
	}
	id := normalizeID(f.ID)
	c.mu.Lock()
	waiting := c.pending[id]
	// An answer with no id is JSON-RPC's parse error (-32700), which the
	// contract sends when it cannot read a frame at all. One call in flight
	// must hear it rather than wait out its timeout; with several, nothing
	// says which one it belonged to, so it is logged and each call answers
	// for itself.
	if id == "" && f.Error != nil && len(c.pending) == 1 {
		for _, only := range c.pending {
			waiting = only
		}
	}
	c.mu.Unlock()
	if waiting == nil {
		if id == "" {
			c.logf("kernel: dropped an answer without an id: %v", f.Error)
			return
		}
		c.logf("kernel: dropped the answer to call %s, which is gone", id)
		return
	}
	// The slot holds one answer: the call is already gone if it does not.
	select {
	case waiting <- &f:
	default:
	}
}

// rpcFrame is one JSON-RPC 2.0 frame, either direction.
type rpcFrame struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// normalizeID is a frame's id as the map key: JSON-RPC allows a number or a
// string, so both spellings of 7 name the same call.
func normalizeID(raw json.RawMessage) string {
	s := string(bytes.TrimSpace(raw))
	if s == "" || s == "null" {
		return ""
	}
	if s[0] == '"' {
		var unquoted string
		if json.Unmarshal([]byte(s), &unquoted) != nil {
			return ""
		}
		return unquoted
	}
	return s
}

// RPCError is Kernel's answer to a call that failed. Branch on Code, never on
// Message; Data carries the machine-readable fields the contract's table names.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 && string(e.Data) != "null" {
		return fmt.Sprintf("kernel: %s (%d): %s", e.Message, e.Code, e.Data)
	}
	return fmt.Sprintf("kernel: %s (%d)", e.Message, e.Code)
}

// Is reports whether err is Kernel's answer with code, following the contract.
func Is(err error, code int) bool {
	var rpc *RPCError
	return errors.As(err, &rpc) && rpc.Code == code
}

// Call runs one method. params may be nil; a JSON-RPC error comes back as
// *RPCError.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	select {
	case <-c.done:
		return c.terminalErr()
	default:
	}
	if err := ctx.Err(); err != nil {
		// The caller gave up before the frame left: Kernel never hears of it,
		// so it never answers a call nobody waits for.
		return fmt.Errorf("kernel: %s: %w", method, err)
	}
	id := strconv.FormatInt(c.seq.Add(1), 10)
	b, err := json.Marshal(rpcFrame{JSONRPC: "2.0", ID: json.RawMessage(strconv.Quote(id)), Method: method, Params: mustJSON(params)})
	if err != nil {
		return fmt.Errorf("kernel: %s: %w", method, err)
	}
	waiting := make(chan *rpcFrame, 1)
	c.mu.Lock()
	c.pending[id] = waiting
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	// The write has a deadline of its own: the caller's context bounds the
	// wait for the answer, and a caller that gives up must not end the
	// connection everyone else is using.
	if err := c.write(ctx, method, b); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.timeout())
	defer cancel()
	select {
	case res := <-waiting:
		if res.Error != nil {
			return res.Error
		}
		if result == nil || len(res.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(res.Result, result); err != nil {
			return fmt.Errorf("kernel: %s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("kernel: %s: %w", method, ctx.Err())
	case <-c.done:
		return fmt.Errorf("kernel: %s: %w", method, c.terminalErr())
	}
}

// Send writes a notification to Kernel. The contract has no notification the
// Host sends today; the approval policy arrives with M3.
func (c *Client) Send(ctx context.Context, method string, params any) error {
	select {
	case <-c.done:
		return c.terminalErr()
	default:
	}
	b, err := json.Marshal(rpcFrame{JSONRPC: "2.0", Method: method, Params: mustJSON(params)})
	if err != nil {
		return fmt.Errorf("kernel: %s: %w", method, err)
	}
	return c.write(ctx, method, b)
}

// write sends one frame with a deadline of its own, so a Kernel that stopped
// reading makes this call fail instead of holding the write mutex forever.
//
// caller bounds only whether the frame goes at all: a caller that gives up
// while the frame waits for the writer must not have its request sent, and a
// caller that gives up during the write must not end the connection everyone
// else is using.
func (c *Client) write(caller context.Context, method string, b []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.opts.writeTimeout())
	defer cancel()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return fmt.Errorf("kernel: %s: %w", method, c.terminalErr())
	default:
	}
	// The caller may have given up while this frame waited for the writer. A
	// request nobody waits for must not reach Kernel: it would run a write
	// with no one to read the answer.
	if err := caller.Err(); err != nil {
		return fmt.Errorf("kernel: %s: %w", method, err)
	}
	if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
		return fmt.Errorf("kernel: %s: %w", method, err)
	}
	return nil
}

// enqueue puts a notification in the queue for the reader and wakes it. The
// read loop never waits here, so a reader that is busy — or that stops reading
// — costs the queue, not the answers.
//
// What happens to a notification depends on whether it can be rebuilt:
//
//   - A conversation's whole view is its state, and one task's or one process's
//     news is that entry's state: the newest one supersedes the earlier one the
//     reader has not taken, and how many it replaced travels in Folded.
//   - An approval or an ask is a question a session is waiting for. It is queued
//     as it arrives, and when its bucket is full the connection ends rather than
//     dropping it: a reconnect re-opens the conversations, and the session asks
//     again. A resolution keeps one entry per request.
//   - Warnings are advice: the newest are kept and the count of the ones lost
//     travels in Dropped.
//
// The read loop is one goroutine for both answers and notifications, which is
// why this never blocks: blocking here would make every call wait behind a
// reader that stopped reading.
func (c *Client) enqueue(n Notification) {
	if c.queue.put(n) {
		c.overflow(n.Method)
		return
	}
	c.signal()
}

// overflow ends the connection because the queue could not hold a notification
// that must not be dropped.
//
// Ending it is the point: the alternative is a session waiting for an approval
// that never comes, and a Client that reconnects opens the conversations it had
// and is told about them again.
func (c *Client) overflow(method string) {
	err := fmt.Errorf("kernel: the notification queue cannot hold another %s without dropping one a reader cannot rebuild (approvals and asks: %d, view steps: %d); the connection is closed so a new one can open its conversations again", method, queueMaxRequests, queueMaxViewSteps)
	c.logf("%v", err)
	c.shut(err)
	_ = c.conn.Close(websocket.StatusPolicyViolation, "notification queue full")
}

// notificationID is the conversation a notification is about, for any
// notification that names one: a view push of any kind, a busy change, an
// approval, an ask.
func notificationID(n Notification) (ID, bool) {
	var p struct {
		ID ID `json:"id"`
	}
	if json.Unmarshal(n.Params, &p) != nil || p.ID == 0 {
		return 0, false
	}
	return p.ID, true
}

// wholeView is a conversation.view push that carries a whole view: kind
// "value" with a value, which is what a "view" subscriber receives. A delta
// subscriber's push has no value, and a "full" push is a reset that a reader
// must see, so neither is one.
func wholeView(n Notification) (ID, bool) {
	if n.Method != NotifyConversationView {
		return 0, false
	}
	var p struct {
		ID    ID              `json:"id"`
		Kind  string          `json:"kind"`
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(n.Params, &p) != nil || p.ID == 0 || p.Kind != ViewKindValue {
		return 0, false
	}
	if len(p.Value) == 0 || string(bytes.TrimSpace(p.Value)) == "null" {
		return 0, false
	}
	return p.ID, true
}

func (c *Client) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// deliver hands queued notifications to the reader, one at a time, on a
// goroutine of its own: it is the only sender on notes and the only one that
// closes it.
//
// The order is the queue's: a question that needs an answer first, then the
// conversation state, then the rest. A reader that was stuck for a while
// therefore wakes to the approvals, not to the frame that happened to be first.
func (c *Client) deliver() {
	defer close(c.notes)
	for {
		if n, ok := c.queue.take(); ok {
			select {
			case c.notes <- n:
				continue
			case <-c.done:
				return
			}
		}
		select {
		case <-c.wake:
		case <-c.done:
			return
		}
	}
}

// mustJSON is params as JSON: nil is omitted, and an already-encoded
// json.RawMessage is sent as it is.
func mustJSON(params any) json.RawMessage {
	if params == nil {
		return nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw
	}
	b, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	return b
}

func (c *Client) terminalErr() error {
	if err := c.Err(); err != nil {
		return err
	}
	return ErrClosed
}

func encodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// call runs one method whose result decodes into T.
func call[T any](c *Client, ctx context.Context, method string, params any) (T, error) {
	var out T
	err := c.Call(ctx, method, params, &out)
	return out, err
}
