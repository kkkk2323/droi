package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Status is where an Agent stands with its Relay.
type Status int

const (
	// Off: not running.
	Off Status = iota
	// Connecting: dialing or registering.
	Connecting
	// Connected: registered; phones reach the Host through the Relay.
	Connected
	// Retrying: the last attempt failed; it dials again at Next.
	Retrying
	// Refused: the Relay refused the Host (a wrong key, or another
	// computer with the same Relay Id); it tries again at Next, later than
	// after a failure.
	Refused
)

// State is an Agent's Status, why the last attempt failed and when it
// tries again.
type State struct {
	Status Status
	Err    string
	Next   time.Time
}

// AgentOptions configure an Agent.
type AgentOptions struct {
	// URL is the Relay's address: http(s)://host[:port][/prefix].
	URL string
	// Key is the Relay's registration key, "" for an open Relay.
	Key string
	// ID is the Host's Relay Id; phones address it as URL/c/<ID>.
	ID string
	// Target is the Gateway's loopback origin, which the Agent sends every
	// request and WebSocket on to.
	Target string
	// Name and Version describe the Host to the Relay.
	Name, Version string
	// OnState, optional, hears every change of State.
	OnState func(State)
}

// Agent is a Host's side of a Relay: it keeps one socket open to the Relay
// and answers what comes through it from the Gateway.
type Agent struct {
	o    AgentOptions
	kick chan struct{}

	mu    sync.Mutex
	state State
	conn  *websocket.Conn
}

// NewAgent is an Agent for o; Run starts it.
func NewAgent(o AgentOptions) *Agent {
	return &Agent{o: o, kick: make(chan struct{}, 1)}
}

// ClientBase is where a phone reaches the Host of Relay Id id on the
// Relay at relayURL.
func ClientBase(relayURL, id string) string {
	return strings.TrimRight(relayURL, "/") + "/c/" + id
}

// AgentURL is the socket an Agent dials for Relay Id id.
func AgentURL(relayURL, id string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(relayURL))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a relay address", relayURL)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("%q is not an http or https address", relayURL)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/agent"
	u.RawPath = ""
	u.RawQuery = "th=" + TokenHash(id)
	u.Fragment = ""
	return u.String(), nil
}

// State is the Agent's current State.
func (a *Agent) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *Agent) set(s State) {
	a.mu.Lock()
	changed := a.state != s
	a.state = s
	a.mu.Unlock()
	if changed && a.o.OnState != nil {
		a.o.OnState(s)
	}
}

// Kick says the network changed: an Agent waiting to dial again dials at
// once, and a connected one checks that its socket still answers.
func (a *Agent) Kick() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
	a.mu.Lock()
	c := a.conn
	a.mu.Unlock()
	if c != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if c.Ping(ctx) != nil {
				_ = c.CloseNow()
			}
		}()
	}
}

// RefusedError is a Relay refusing the Host, with its close code.
type RefusedError struct {
	Code    int
	Message string
}

func (e *RefusedError) Error() string { return e.Message }

// Run keeps the Agent connected until ctx ends, then sets Off.
func (a *Agent) Run(ctx context.Context) {
	// Drain a Kick from before Run, which would cut the first wait short.
	select {
	case <-a.kick:
	default:
	}
	var delay time.Duration
	for ctx.Err() == nil {
		start := time.Now()
		err := a.session(ctx)
		if ctx.Err() != nil {
			break
		}
		var refused *RefusedError
		var wait time.Duration
		status := Retrying
		if errors.As(err, &refused) {
			status, wait = Refused, 5*time.Minute
			if refused.Code == CloseReplaced {
				wait = time.Minute
			}
		} else {
			if time.Since(start) > 30*time.Second {
				delay = 0
			}
			delay = nextDelay(delay)
			wait = delay
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		a.set(State{Status: status, Err: msg, Next: time.Now().Add(wait)})
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
		case <-t.C:
		case <-a.kick:
			delay = 0
		}
		t.Stop()
	}
	a.set(State{Status: Off})
}

// nextDelay is the wait after a failure that followed a wait of prev: a
// second, doubling to half a minute, with a little jitter.
func nextDelay(prev time.Duration) time.Duration {
	d := time.Second
	if prev > 0 {
		d = min(2*prev, 30*time.Second)
	}
	return d + time.Duration(rand.Int64N(int64(d/5)+1))
}

func (a *Agent) session(ctx context.Context) error {
	prev := a.State()
	a.set(State{Status: Connecting, Err: prev.Err})
	target, err := url.Parse(a.o.Target)
	if err != nil || target.Host == "" {
		return fmt.Errorf("%q is not the Gateway's address", a.o.Target)
	}
	if !ValidID(a.o.ID) {
		return &RefusedError{Code: CloseBadHash, Message: "the Relay Id is not valid"}
	}
	wsURL, err := AgentURL(a.o.URL, a.o.ID)
	if err != nil {
		return &RefusedError{Message: err.Error()}
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	c, _, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"User-Agent": {"Droi/" + a.o.Version}},
	})
	cancel()
	if err != nil {
		return err
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(serverMaxFrame)

	reg, _ := json.Marshal(frame{T: "register", TokenHash: TokenHash(a.o.ID), Name: a.o.Name, Ver: a.o.Version, Key: a.o.Key, Proto: Protocol})
	rctx, rcancel := context.WithTimeout(ctx, registerTimeout)
	defer rcancel()
	if err := c.Write(rctx, websocket.MessageText, reg); err != nil {
		return err
	}
	var f frame
	for {
		typ, data, err := c.Read(rctx)
		if err != nil {
			return refusal(err, f.Message)
		}
		if typ != websocket.MessageText || json.Unmarshal(data, &f) != nil {
			continue
		}
		if f.T == "registered" {
			break
		}
		if f.T != "error" {
			return fmt.Errorf("the relay answered %q", f.T)
		}
	}
	rcancel()
	proto := f.Proto
	if proto < 1 {
		proto = 1
	}

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	t := &tunnel{
		conn: c, proto: proto, target: strings.TrimRight(target.Scheme+"://"+target.Host, "/"), ctx: sctx,
		client: &http.Client{
			Transport: &http.Transport{Proxy: nil, DisableCompression: true, MaxIdleConnsPerHost: 32, IdleConnTimeout: 90 * time.Second},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		reqs:  map[string]*localRequest{},
		socks: map[string]*localSocket{},
	}
	defer t.client.CloseIdleConnections()
	a.mu.Lock()
	a.conn = c
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.conn = nil
		a.mu.Unlock()
	}()
	a.set(State{Status: Connected})

	go func() {
		tick := time.NewTicker(25 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-tick.C:
			}
			pctx, pcancel := context.WithTimeout(sctx, 15*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil && sctx.Err() == nil {
				_ = c.CloseNow()
				return
			}
		}
	}()
	return refusal(t.read(sctx), "")
}

// refusal is err, or a RefusedError when the Relay closed with a code that
// says it will not take this Host.
func refusal(err error, message string) error {
	code := int(websocket.CloseStatus(err))
	switch code {
	case CloseBadKey, CloseBadToken:
		if message == "" {
			message = "the relay refused the registration key"
		}
		return &RefusedError{Code: code, Message: message}
	case CloseBadHash:
		return &RefusedError{Code: code, Message: "the relay refused the Relay Id"}
	case CloseReplaced:
		return &RefusedError{Code: code, Message: "another computer connected to the relay with this computer's Relay Id"}
	}
	if message != "" && code > 0 {
		return &RefusedError{Code: code, Message: message}
	}
	return err
}

// tunnel is one connected socket to the Relay and the requests and
// WebSockets going through it.
type tunnel struct {
	conn   *websocket.Conn
	proto  int
	target string
	ctx    context.Context
	client *http.Client

	wmu sync.Mutex

	mu    sync.Mutex
	reqs  map[string]*localRequest
	socks map[string]*localSocket
}

type localRequest struct {
	cancel context.CancelFunc
	body   *bodyQueue
}

type localSocket struct {
	cancel context.CancelFunc
	out    chan wsMessage

	mu   sync.Mutex
	conn *websocket.Conn
}

func (t *tunnel) write(typ websocket.MessageType, b []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.conn.Write(t.ctx, typ, b)
}

func (t *tunnel) send(f frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return t.write(websocket.MessageText, b)
}

func (t *tunnel) read(ctx context.Context) error {
	for {
		typ, data, err := t.conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageBinary {
			kind, id, payload, err := decodeBinary(data)
			if err != nil {
				continue
			}
			switch kind {
			case binHBody:
				if r := t.request(id); r != nil && r.body != nil {
					r.body.push(payload)
				}
			case binWText:
				t.toLocal(id, websocket.MessageText, payload)
			case binWBin:
				t.toLocal(id, websocket.MessageBinary, payload)
			}
			continue
		}
		var f frame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.T {
		case "hreq":
			t.startRequest(ctx, f)
		case "hreqend":
			if r := t.request(f.ID); r != nil && r.body != nil {
				r.body.finish()
			}
		case "hcancel":
			t.mu.Lock()
			r := t.reqs[f.ID]
			delete(t.reqs, f.ID)
			t.mu.Unlock()
			if r != nil {
				r.cancel()
			}
		case "wopen":
			t.openSocket(ctx, f)
		case "wmsg":
			switch {
			case f.S != nil:
				t.toLocal(f.ID, websocket.MessageText, []byte(*f.S))
			case f.B != nil:
				if b, err := base64.StdEncoding.DecodeString(*f.B); err == nil {
					t.toLocal(f.ID, websocket.MessageBinary, b)
				}
			}
		case "wclose":
			t.closeLocal(f.ID, f.Code, f.Reason)
		}
	}
}

func (t *tunnel) request(id string) *localRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reqs[id]
}

func (t *tunnel) startRequest(ctx context.Context, f frame) {
	rctx, cancel := context.WithCancel(ctx)
	r := &localRequest{cancel: cancel}
	var body io.Reader
	switch {
	case f.Stream:
		r.body = newBodyQueue(rctx, func(n int) { _ = t.send(frame{T: "hack", ID: f.ID, N: int64(n)}) })
		body = r.body
	case f.B != nil:
		b, err := base64.StdEncoding.DecodeString(*f.B)
		if err != nil {
			cancel()
			_ = t.send(frame{T: "herr", ID: f.ID, Message: "malformed request body"})
			return
		}
		body = bytes.NewReader(b)
	}
	t.mu.Lock()
	t.reqs[f.ID] = r
	t.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			t.mu.Lock()
			if t.reqs[f.ID] == r {
				delete(t.reqs, f.ID)
			}
			t.mu.Unlock()
		}()
		path := f.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		req, err := http.NewRequestWithContext(rctx, f.Method, t.target+path, body)
		if err != nil {
			_ = t.send(frame{T: "herr", ID: f.ID, Message: err.Error()})
			return
		}
		f.Headers.apply(req.Header, requestDropped)
		resp, err := t.client.Do(req)
		if err != nil {
			if rctx.Err() == nil {
				_ = t.send(frame{T: "herr", ID: f.ID, Message: "the Gateway did not answer"})
			}
			return
		}
		defer resp.Body.Close()
		if t.send(frame{T: "hres", ID: f.ID, Status: resp.StatusCode, Headers: fromHTTP(resp.Header, responseDropped)}) != nil {
			return
		}
		buf := make([]byte, chunkSize)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if t.sendChunk(f.ID, buf[:n]) != nil {
					return
				}
			}
			if err == io.EOF {
				_ = t.send(frame{T: "hend", ID: f.ID})
				return
			}
			if err != nil {
				if rctx.Err() == nil {
					_ = t.send(frame{T: "herr", ID: f.ID, Message: "the Gateway's answer broke off"})
				}
				return
			}
		}
	}()
}

func (t *tunnel) sendChunk(id string, p []byte) error {
	if t.proto >= 2 {
		return t.write(websocket.MessageBinary, encodeBinary(binHChunk, id, p))
	}
	b := base64.StdEncoding.EncodeToString(p)
	return t.send(frame{T: "hchunk", ID: id, B: &b})
}

func (t *tunnel) sendMessage(id string, typ websocket.MessageType, p []byte) error {
	if t.proto >= 2 {
		kind := binWText
		if typ == websocket.MessageBinary {
			kind = binWBin
		}
		return t.write(websocket.MessageBinary, encodeBinary(kind, id, p))
	}
	if typ == websocket.MessageText {
		s := string(p)
		return t.send(frame{T: "wmsg", ID: id, S: &s})
	}
	b := base64.StdEncoding.EncodeToString(p)
	return t.send(frame{T: "wmsg", ID: id, B: &b})
}

func (t *tunnel) openSocket(ctx context.Context, f frame) {
	rctx, cancel := context.WithCancel(ctx)
	s := &localSocket{cancel: cancel, out: make(chan wsMessage, 256)}
	t.mu.Lock()
	t.socks[f.ID] = s
	t.mu.Unlock()
	go func() {
		defer cancel()
		path := f.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		u := "ws" + strings.TrimPrefix(t.target, "http") + path
		c, _, err := websocket.Dial(rctx, u, nil)
		if err != nil {
			if t.removeSocket(f.ID) != nil && rctx.Err() == nil {
				_ = t.send(frame{T: "werr", ID: f.ID, Message: "the Gateway refused the socket"})
			}
			return
		}
		c.SetReadLimit(serverMaxFrame)
		s.mu.Lock()
		s.conn = c
		s.mu.Unlock()
		if rctx.Err() != nil {
			_ = c.CloseNow()
			return
		}
		if t.send(frame{T: "wopened", ID: f.ID}) != nil {
			_ = c.CloseNow()
			return
		}
		go func() {
			for {
				select {
				case m := <-s.out:
					if c.Write(rctx, m.typ, m.data) != nil {
						_ = c.CloseNow()
						return
					}
				case <-rctx.Done():
					return
				}
			}
		}()
		for {
			typ, data, err := c.Read(rctx)
			if err != nil {
				if t.removeSocket(f.ID) != nil && t.ctx.Err() == nil {
					_ = t.send(frame{T: "wclose", ID: f.ID, Code: sanitizeCloseCode(int(websocket.CloseStatus(err)))})
				}
				_ = c.CloseNow()
				return
			}
			if t.sendMessage(f.ID, typ, data) != nil {
				_ = c.CloseNow()
				return
			}
		}
	}()
}

func (t *tunnel) removeSocket(id string) *localSocket {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.socks[id]
	delete(t.socks, id)
	return s
}

func (t *tunnel) toLocal(id string, typ websocket.MessageType, data []byte) {
	t.mu.Lock()
	s := t.socks[id]
	t.mu.Unlock()
	if s == nil {
		return
	}
	select {
	case s.out <- wsMessage{typ, data}:
	case <-t.ctx.Done():
	}
}

func (t *tunnel) closeLocal(id string, code int, reason string) {
	s := t.removeSocket(id)
	if s == nil {
		return
	}
	s.mu.Lock()
	c := s.conn
	s.mu.Unlock()
	if c == nil {
		s.cancel()
		return
	}
	go func() {
		_ = c.Close(websocket.StatusCode(sanitizeCloseCode(code)), reason)
		s.cancel()
	}()
}

// bodyQueue is a request body arriving in frames: the Relay sends no more
// than bodyWindow ahead of what ack has acknowledged, so it never blocks the
// tunnel's reader.
type bodyQueue struct {
	ctx    context.Context
	ack    func(n int)
	notify chan struct{}

	mu      sync.Mutex
	chunks  [][]byte
	cur     []byte
	done    bool
	pending int
}

func newBodyQueue(ctx context.Context, ack func(n int)) *bodyQueue {
	return &bodyQueue{ctx: ctx, ack: ack, notify: make(chan struct{}, 1)}
}

func (q *bodyQueue) push(b []byte) {
	q.mu.Lock()
	q.chunks = append(q.chunks, b)
	q.mu.Unlock()
	q.wake()
}

func (q *bodyQueue) finish() {
	q.mu.Lock()
	q.done = true
	q.mu.Unlock()
	q.wake()
}

func (q *bodyQueue) wake() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *bodyQueue) Read(p []byte) (int, error) {
	for {
		q.mu.Lock()
		if len(q.cur) == 0 && len(q.chunks) > 0 {
			q.cur, q.chunks = q.chunks[0], q.chunks[1:]
		}
		if len(q.cur) > 0 {
			n := copy(p, q.cur)
			q.cur = q.cur[n:]
			q.pending += n
			// Acknowledged in batches, and whenever the queue runs dry, so
			// the Relay never waits on bytes already read.
			var ack int
			if q.pending >= bodySlice || (len(q.cur) == 0 && len(q.chunks) == 0) {
				ack, q.pending = q.pending, 0
			}
			q.mu.Unlock()
			if ack > 0 {
				q.ack(ack)
			}
			return n, nil
		}
		done := q.done
		q.mu.Unlock()
		if done {
			return 0, io.EOF
		}
		select {
		case <-q.notify:
		case <-q.ctx.Done():
			return 0, q.ctx.Err()
		}
	}
}
