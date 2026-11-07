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
	// NotesBuffer is how many notifications may wait for the reader; 0 is
	// 256. One goroutine reads them, in order, so a reader that stops reading
	// stops Kernel's news.
	NotesBuffer int
	// Log, optional, hears what the Client drops: a frame it cannot read, an
	// answer to a call that is gone.
	Log func(string)
}

// Client is one connection to a Kernel: the contract's 25 methods as Go calls,
// and its notifications in order on Notifications.
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
	opened  map[ID]struct{}
	err     error

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
		opened:  map[ID]struct{}{},
		notes:   make(chan Notification, opts.notesBuffer()),
		done:    make(chan struct{}),
	}
	go c.read()
	return c, nil
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

// Notifications is every notification Kernel sends this connection, in order.
// The channel closes when the connection does; Done and Err say why.
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
	out := make([]ID, 0, len(c.opened))
	for id := range c.opened {
		out = append(out, id)
	}
	return out
}

// Close ends the connection, which unwatches everything it opened.
func (c *Client) Close() error {
	// Shut first, so a waiting call sees ErrClosed whether or not the close
	// handshake reaches Kernel.
	c.shut(nil)
	return c.conn.Close(websocket.StatusNormalClosure, "")
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
	// The reader is the only sender on notes, so closing it here cannot race
	// a send.
	defer close(c.notes)
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
		select {
		case c.notes <- Notification{Method: f.Method, Params: f.Params}:
		case <-c.done:
		}
		return
	}
	id := normalizeID(f.ID)
	if id == "" {
		c.logf("kernel: dropped an answer without an id")
		return
	}
	c.mu.Lock()
	waiting := c.pending[id]
	c.mu.Unlock()
	if waiting == nil {
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

	if err := c.write(ctx, b); err != nil {
		return fmt.Errorf("kernel: %s: %w", method, err)
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
	if err := c.write(ctx, b); err != nil {
		return fmt.Errorf("kernel: %s: %w", method, err)
	}
	return nil
}

func (c *Client) write(ctx context.Context, b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, b)
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
