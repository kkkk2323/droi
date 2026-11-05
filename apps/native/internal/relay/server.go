package relay

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ServerOptions configure a Server; zero values take the defaults.
type ServerOptions struct {
	// Key, when set, is the registration key every Agent must present.
	Key     string
	Version string
	// MaxBody bounds one request body (128 MiB).
	MaxBody int64
	// MaxAgents bounds the Hosts registered at once (10 000).
	MaxAgents int
	// MaxRequests and MaxSockets bound one Host's requests and WebSockets
	// in flight (256 each).
	MaxRequests int
	MaxSockets  int
	// Stall cuts off a phone that takes nothing for this long, or an Agent
	// that takes none of an upload (60 s).
	Stall time.Duration
	// Logf logs registrations; nil is log.Printf. It is never given a Relay
	// Id or a token.
	Logf func(format string, args ...any)
}

// Server is Droi's Relay: Agents register on /agent, phones reach them on
// /c/<Relay Id>/..., and /healthz says it runs. It keeps nothing on disk.
type Server struct {
	o ServerOptions

	mu     sync.Mutex
	agents map[string]*serverAgent
	closed bool
}

// NewServer is a Server with o's limits.
func NewServer(o ServerOptions) *Server {
	if o.MaxBody <= 0 {
		o.MaxBody = 128 << 20
	}
	if o.MaxAgents <= 0 {
		o.MaxAgents = 10000
	}
	if o.MaxRequests <= 0 {
		o.MaxRequests = 256
	}
	if o.MaxSockets <= 0 {
		o.MaxSockets = 256
	}
	if o.Stall <= 0 {
		o.Stall = 60 * time.Second
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	return &Server{o: o, agents: map[string]*serverAgent{}}
}

// Agents is how many Hosts are registered.
func (s *Server) Agents() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.agents)
}

// Close ends every Agent's socket with 1012, so the Hosts dial again.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	agents := s.agents
	s.agents = map[string]*serverAgent{}
	s.mu.Unlock()
	for _, a := range agents {
		a.end(websocket.StatusServiceRestart, "Relay restarting")
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	switch path {
	case "/healthz":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "edition": "droi", "version": s.o.Version, "protocol": Protocol, "agents": s.Agents()})
		return
	case "/":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Droi Relay "+s.o.Version+" is running.\nUse this address in Droi → Settings → Remote Access → Relay.\n")
		return
	case "/agent":
		s.serveAgent(w, r)
		return
	}
	id, rest, ok := clientPath(path)
	if !ok {
		respondJSON(w, http.StatusNotFound, "Not found")
		return
	}
	if r.URL.RawQuery != "" {
		rest += "?" + r.URL.RawQuery
	}
	s.mu.Lock()
	a := s.agents[TokenHash(id)]
	s.mu.Unlock()
	if a == nil {
		respondJSON(w, http.StatusBadGateway, "The computer is not connected to this relay")
		return
	}
	if isUpgrade(r) {
		a.serveSocket(w, r, rest)
		return
	}
	a.serveHTTP(w, r, rest)
}

func respondJSON(w http.ResponseWriter, status int, message string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func isUpgrade(r *http.Request) bool {
	if r.Header.Get("Upgrade") == "" {
		return false
	}
	for _, v := range r.Header.Values("Connection") {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), "upgrade") {
				return true
			}
		}
	}
	return false
}

// serverAgent is one registered Host's socket and what goes through it.
type serverAgent struct {
	s      *Server
	conn   *websocket.Conn
	hash   string
	ctx    context.Context
	cancel context.CancelFunc

	wmu sync.Mutex

	mu    sync.Mutex
	next  uint64
	reqs  map[string]*tunnelRequest
	socks map[string]*tunnelSocket

	inflight atomic.Int32
	// delivering counts frames the reader waits to hand to a slow phone;
	// pongs are not read meanwhile, so a missed one is not a dead Agent.
	delivering atomic.Int32
}

func (s *Server) serveAgent(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(serverMaxFrame)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rctx, rcancel := context.WithTimeout(ctx, registerTimeout)
	typ, data, err := c.Read(rctx)
	rcancel()
	var f frame
	if err != nil || typ != websocket.MessageText || json.Unmarshal(data, &f) != nil || f.T != "register" {
		_ = c.CloseNow()
		return
	}
	refuse := func(code websocket.StatusCode, message string) {
		b, _ := json.Marshal(frame{T: "error", Message: message})
		_ = c.Write(ctx, websocket.MessageText, b)
		_ = c.Close(code, message)
	}
	if s.o.Key != "" && subtle.ConstantTimeCompare([]byte(f.Key), []byte(s.o.Key)) != 1 {
		refuse(CloseBadKey, "Invalid registration key")
		return
	}
	if !validHash.MatchString(f.TokenHash) {
		refuse(CloseBadHash, "Invalid token hash")
		return
	}
	proto := negotiate(f.Proto)
	if proto < 2 {
		refuse(websocket.StatusPolicyViolation, "This relay needs relay protocol 2")
		return
	}

	a := &serverAgent{s: s, conn: c, hash: f.TokenHash, ctx: ctx, cancel: cancel, reqs: map[string]*tunnelRequest{}, socks: map[string]*tunnelSocket{}}
	s.mu.Lock()
	old := s.agents[a.hash]
	if s.closed || (old == nil && len(s.agents) >= s.o.MaxAgents) {
		s.mu.Unlock()
		refuse(websocket.StatusTryAgainLater, "This relay is full")
		return
	}
	s.agents[a.hash] = a
	n := len(s.agents)
	s.mu.Unlock()
	if old != nil {
		old.end(CloseReplaced, "Replaced by a newer connection")
	}
	if a.send(frame{T: "registered", Proto: proto}) != nil {
		s.drop(a)
		return
	}
	s.o.Logf("relay: a computer registered; computers=%d", n)
	go a.ping()
	a.read()
	s.drop(a)
}

// drop forgets a, unless another Agent took its place, and ends what went
// through it.
func (s *Server) drop(a *serverAgent) {
	s.mu.Lock()
	if s.agents[a.hash] == a {
		delete(s.agents, a.hash)
	}
	n := len(s.agents)
	s.mu.Unlock()
	a.cancel()
	a.end(websocket.StatusServiceRestart, "Computer disconnected")
	s.o.Logf("relay: a computer disconnected; computers=%d", n)
}

func (a *serverAgent) end(code websocket.StatusCode, reason string) {
	a.mu.Lock()
	socks := a.socks
	a.socks = map[string]*tunnelSocket{}
	a.mu.Unlock()
	for _, t := range socks {
		go func() { _ = t.conn.Close(websocket.StatusServiceRestart, "The computer disconnected from the relay") }()
	}
	go func() {
		_ = a.conn.Close(code, reason)
		a.cancel()
	}()
}

func (a *serverAgent) write(typ websocket.MessageType, b []byte) error {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	return a.conn.Write(a.ctx, typ, b)
}

func (a *serverAgent) send(f frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return a.write(websocket.MessageText, b)
}

func (a *serverAgent) sendBinary(kind byte, id string, payload []byte) error {
	return a.write(websocket.MessageBinary, encodeBinary(kind, id, payload))
}

func (a *serverAgent) newID(prefix string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.next++
	return prefix + strconv.FormatUint(a.next, 10)
}

func (a *serverAgent) ping() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		err := a.conn.Ping(ctx)
		cancel()
		if err != nil && a.ctx.Err() == nil && a.delivering.Load() == 0 {
			_ = a.conn.CloseNow()
			return
		}
	}
}

func (a *serverAgent) read() {
	for {
		typ, data, err := a.conn.Read(a.ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			kind, id, payload, err := decodeBinary(data)
			if err != nil {
				continue
			}
			switch kind {
			case binHChunk:
				a.toRequest(id, event{kind: evChunk, data: payload})
			case binWText:
				a.toSocket(id, websocket.MessageText, payload)
			case binWBin:
				a.toSocket(id, websocket.MessageBinary, payload)
			}
			continue
		}
		var f frame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.T {
		case "hres":
			a.toRequest(f.ID, event{kind: evHead, status: f.Status, headers: f.Headers})
		case "hchunk":
			if f.B != nil {
				if b, err := base64.StdEncoding.DecodeString(*f.B); err == nil {
					a.toRequest(f.ID, event{kind: evChunk, data: b})
				}
			}
		case "hend":
			a.toRequest(f.ID, event{kind: evEnd})
		case "herr":
			a.toRequest(f.ID, event{kind: evErr, message: f.Message})
		case "hack":
			if r := a.request(f.ID); r != nil {
				r.refund(f.N)
			}
		case "wopened":
			if t := a.socket(f.ID); t != nil {
				t.once.Do(func() { close(t.opened) })
			} else {
				_ = a.send(frame{T: "wclose", ID: f.ID, Code: 1000})
			}
		case "werr":
			if t := a.removeSocket(f.ID); t != nil {
				go func() { _ = t.conn.Close(websocket.StatusInternalError, "The computer could not open the socket") }()
			}
		case "wmsg":
			switch {
			case f.S != nil:
				a.toSocket(f.ID, websocket.MessageText, []byte(*f.S))
			case f.B != nil:
				if b, err := base64.StdEncoding.DecodeString(*f.B); err == nil {
					a.toSocket(f.ID, websocket.MessageBinary, b)
				}
			}
		case "wclose":
			if t := a.removeSocket(f.ID); t != nil {
				code, reason := sanitizeCloseCode(f.Code), f.Reason
				if len(reason) > 120 {
					reason = reason[:120]
				}
				go func() { _ = t.conn.Close(websocket.StatusCode(code), reason) }()
			}
		case "push":
			_ = a.send(frame{T: "pushed", ID: f.ID, Unsupported: true})
		}
	}
}

// deliver hands v to ch unless gone closes first or nothing takes it for
// Stall, which is reported as false.
func deliver[T any](a *serverAgent, ch chan<- T, gone <-chan struct{}, v T) bool {
	select {
	case ch <- v:
		return true
	default:
	}
	a.delivering.Add(1)
	defer a.delivering.Add(-1)
	t := time.NewTimer(a.s.o.Stall)
	defer t.Stop()
	select {
	case ch <- v:
		return true
	case <-gone:
	case <-t.C:
	case <-a.ctx.Done():
	}
	return false
}

// --- HTTP ---

const (
	evHead = iota
	evChunk
	evEnd
	evErr
)

type event struct {
	kind    int
	status  int
	headers headers
	data    []byte
	message string
}

type tunnelRequest struct {
	events   chan event
	gone     chan struct{}
	stalled  chan struct{}
	stallOne sync.Once
	credit   atomic.Int64
	credited chan struct{}
}

func (r *tunnelRequest) refund(n int64) {
	if n <= 0 {
		return
	}
	if r.credit.Add(n) > bodyWindow {
		r.credit.Store(bodyWindow)
	}
	select {
	case r.credited <- struct{}{}:
	default:
	}
}

func (a *serverAgent) request(id string) *tunnelRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reqs[id]
}

func (a *serverAgent) toRequest(id string, ev event) {
	r := a.request(id)
	if r == nil {
		if ev.kind == evHead || ev.kind == evChunk {
			_ = a.send(frame{T: "hcancel", ID: id})
		}
		return
	}
	if !deliver(a, r.events, r.gone, ev) {
		r.stallOne.Do(func() { close(r.stalled) })
	}
}

func (a *serverAgent) serveHTTP(w http.ResponseWriter, r *http.Request, rest string) {
	o := a.s.o
	if r.ContentLength > o.MaxBody {
		respondJSON(w, http.StatusRequestEntityTooLarge, "Request body too large")
		return
	}
	if a.inflight.Add(1) > int32(o.MaxRequests) {
		a.inflight.Add(-1)
		respondJSON(w, http.StatusServiceUnavailable, "Too many requests to this computer at once")
		return
	}
	defer a.inflight.Add(-1)

	id := a.newID("h")
	req := &tunnelRequest{events: make(chan event, 64), gone: make(chan struct{}), stalled: make(chan struct{}), credited: make(chan struct{}, 1)}
	req.credit.Store(bodyWindow)
	a.mu.Lock()
	a.reqs[id] = req
	a.mu.Unlock()
	finished := false
	defer func() {
		close(req.gone)
		a.mu.Lock()
		_, live := a.reqs[id]
		delete(a.reqs, id)
		a.mu.Unlock()
		if live && !finished && a.ctx.Err() == nil {
			_ = a.send(frame{T: "hcancel", ID: id})
		}
	}()

	hasBody := r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0
	if err := a.send(frame{T: "hreq", ID: id, Method: r.Method, Path: rest, Headers: fromHTTP(r.Header, requestDropped), Stream: hasBody}); err != nil {
		respondJSON(w, http.StatusBadGateway, "The computer disconnected from the relay")
		return
	}
	rc := http.NewResponseController(w)
	uploaded := make(chan struct{})
	failed := make(chan int, 1)
	if hasBody {
		_ = rc.EnableFullDuplex()
		go a.upload(r, id, req, uploaded, failed)
	} else {
		close(uploaded)
	}

	var headTimer <-chan time.Time
	waiting := (<-chan struct{})(uploaded)
	headSent := false
	abort := func() { panic(http.ErrAbortHandler) }
	for {
		select {
		case <-waiting:
			waiting = nil
			if !headSent {
				t := time.NewTimer(headTimeout)
				defer t.Stop()
				headTimer = t.C
			}
		case <-headTimer:
			if !headSent {
				respondJSON(w, http.StatusGatewayTimeout, "The computer did not answer in time")
				return
			}
		case status := <-failed:
			if headSent {
				abort()
			}
			if status == http.StatusRequestEntityTooLarge {
				w.Header().Set("Connection", "close")
				respondJSON(w, status, "Request body too large")
			} else {
				respondJSON(w, status, "The computer stopped reading the request body")
			}
			return
		case <-req.stalled:
			if headSent {
				abort()
			}
			respondJSON(w, http.StatusGatewayTimeout, "The relay could not pass the answer on")
			return
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			if headSent {
				abort()
			}
			respondJSON(w, http.StatusBadGateway, "The computer disconnected from the relay")
			return
		case ev := <-req.events:
			switch ev.kind {
			case evHead:
				if headSent {
					continue
				}
				headSent = true
				ev.headers.apply(w.Header(), responseDropped)
				status := ev.status
				if status < 100 || status > 999 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				_ = rc.Flush()
			case evChunk:
				headSent = true
				_ = rc.SetWriteDeadline(time.Now().Add(o.Stall))
				if _, err := w.Write(ev.data); err != nil {
					return
				}
				if err := rc.Flush(); err != nil {
					return
				}
			case evEnd:
				finished = true
				return
			case evErr:
				finished = true
				if headSent {
					abort()
				}
				msg := ev.message
				if msg == "" {
					msg = "The computer could not answer"
				}
				respondJSON(w, http.StatusBadGateway, msg)
				return
			}
		}
	}
}

// upload streams r's body to the Agent, never more than bodyWindow ahead
// of what it acknowledged, then sends hreqend and closes uploaded.
func (a *serverAgent) upload(r *http.Request, id string, req *tunnelRequest, uploaded chan<- struct{}, failed chan<- int) {
	buf := make([]byte, bodySlice)
	var total int64
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > a.s.o.MaxBody {
				failed <- http.StatusRequestEntityTooLarge
				return
			}
			for req.credit.Load() <= 0 {
				t := time.NewTimer(a.s.o.Stall)
				select {
				case <-req.credited:
					t.Stop()
				case <-req.gone:
					t.Stop()
					return
				case <-t.C:
					failed <- http.StatusGatewayTimeout
					return
				}
			}
			if a.sendBinary(binHBody, id, buf[:n]) != nil {
				return
			}
			req.credit.Add(-int64(n))
		}
		if err == io.EOF {
			if a.send(frame{T: "hreqend", ID: id}) == nil {
				close(uploaded)
			}
			return
		}
		if err != nil {
			return
		}
		select {
		case <-req.gone:
			return
		default:
		}
	}
}

// --- WebSockets ---

type wsMessage struct {
	typ  websocket.MessageType
	data []byte
}

type tunnelSocket struct {
	conn   *websocket.Conn
	out    chan wsMessage
	opened chan struct{}
	once   sync.Once
	gone   chan struct{}
}

func (a *serverAgent) socket(id string) *tunnelSocket {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.socks[id]
}

func (a *serverAgent) removeSocket(id string) *tunnelSocket {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.socks[id]
	delete(a.socks, id)
	return t
}

func (a *serverAgent) toSocket(id string, typ websocket.MessageType, data []byte) {
	t := a.socket(id)
	if t == nil {
		return
	}
	if !deliver(a, t.out, t.gone, wsMessage{typ, data}) {
		_ = t.conn.CloseNow()
	}
}

func (a *serverAgent) serveSocket(w http.ResponseWriter, r *http.Request, rest string) {
	a.mu.Lock()
	full := len(a.socks) >= a.s.o.MaxSockets
	a.mu.Unlock()
	if full {
		respondJSON(w, http.StatusServiceUnavailable, "Too many connections to this computer")
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(clientMaxMessage)
	id := a.newID("w")
	t := &tunnelSocket{conn: c, out: make(chan wsMessage, 64), opened: make(chan struct{}), gone: make(chan struct{})}
	defer close(t.gone)
	a.mu.Lock()
	a.socks[id] = t
	a.mu.Unlock()
	if a.send(frame{T: "wopen", ID: id, Path: rest}) != nil {
		a.removeSocket(id)
		_ = c.Close(websocket.StatusServiceRestart, "The computer disconnected from the relay")
		return
	}
	// The phone's first messages wait in its socket until the Host opened
	// its side.
	wait := time.NewTimer(headTimeout)
	select {
	case <-t.opened:
		wait.Stop()
	case <-wait.C:
		if a.removeSocket(id) != nil {
			_ = a.send(frame{T: "wclose", ID: id, Code: 1000})
		}
		_ = c.Close(websocket.StatusTryAgainLater, "The computer did not answer in time")
		return
	case <-a.ctx.Done():
		return
	}

	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	go func() {
		for {
			select {
			case m := <-t.out:
				wctx, wcancel := context.WithTimeout(ctx, a.s.o.Stall)
				err := c.Write(wctx, m.typ, m.data)
				wcancel()
				if err != nil {
					_ = c.CloseNow()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			if a.removeSocket(id) != nil && a.ctx.Err() == nil {
				code := int(websocket.CloseStatus(err))
				_ = a.send(frame{T: "wclose", ID: id, Code: sanitizeCloseCode(code)})
			}
			return
		}
		kind := binWText
		if typ == websocket.MessageBinary {
			kind = binWBin
		}
		if a.sendBinary(kind, id, data) != nil {
			return
		}
	}
}
