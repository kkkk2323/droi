// Package gateway is the Host's Gateway (CONTEXT.md, ADR 0001): it lets a
// Remote Client such as the Phone App reach the Daemon. It checks the Pairing
// Token at the WebSocket upgrade, supplies the Factory credential where the
// Client left its placeholder, adds the System Prompt Addition to
// `daemon.initialize_session`, and otherwise forwards frames verbatim. Beside
// the Daemon socket it answers Droi's own Scratch Workspace requests (ADR
// 0008), tells a Client where a Session's transcript is, and describes the
// computer on /meta. It serves no web Client.
package gateway

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
)

// The Gateway contract the Clients share (packages/daemon-layer/src/gateway.ts).
const (
	// DaemonPath is the WebSocket path whose frames go to the Daemon. A plain
	// request on it answers 204 or 401, so a Client can tell a bad Pairing
	// Token from an unreachable Daemon.
	DaemonPath = "/daemon"
	// TokenQuery is the query parameter carrying the token.
	TokenQuery = "token"
	// MetaPath describes the computer; never anything secret.
	MetaPath = "/meta"
	// APIKeyPlaceholder is what a Client sends instead of a Factory credential.
	APIKeyPlaceholder = "droi-gateway"

	ScratchPath        = "/scratch-workspaces"
	ScratchTrashPath   = "/scratch-workspaces/trash"
	ScratchRestorePath = "/scratch-workspaces/restore"
	ScratchPathQuery   = "path"

	SessionFilePath = "/session-file"
	SessionIDQuery  = "sessionId"
)

// PreferredPort is the Gateway's usual port. A paired phone remembers the
// address with its port, so the port should not change between launches.
const PreferredPort = 41417

const (
	loopback          = "127.0.0.1"
	initializeSession = "daemon.initialize_session"
	// The ws library's default maxPayload, which the Electron Gateway ran with.
	maxMessage = 100 << 20
)

// TokenKind says which token a bridge authenticated with.
type TokenKind string

const (
	// TokenPairing is the Pairing Token Remote Clients present.
	TokenPairing TokenKind = "pairing"
	// TokenLocal is a per-launch token only a Local Client knows.
	TokenLocal TokenKind = "local"
)

// Scratch makes, trashes and restores Scratch Workspaces. A refused path is
// reported as a *NotAScratchWorkspace, which answers 400; any other error 500.
type Scratch interface {
	Create() (string, error)
	Trash(path string) error
	Restore(path string) error
}

// Options is what the Gateway needs from the Host.
type Options struct {
	// Port is the loopback port to try first; when it is taken an ephemeral
	// port is used instead. 0 picks an ephemeral port.
	Port int
	// RemoteAccess binds the LAN listeners from the start.
	RemoteAccess bool
	// DaemonURL is the Daemon's WebSocket URL, "" while it is not running.
	DaemonURL func() string
	// PairingToken is the token Remote Clients present.
	PairingToken func() string
	// LocalToken, optional, is a second token that Revoke(TokenPairing)
	// leaves alone.
	LocalToken func() string
	// Credential replaces the Client's placeholder: a Factory login token or
	// an API key. Nil, an error or an empty credential leaves the frame
	// untouched, and the Daemon then rejects it.
	Credential func(ctx context.Context) (*droid.Credential, error)
	// AppendSystemPrompt, optional, is the System Prompt Addition, read at
	// every Session start; blank leaves Droid's prompt alone.
	AppendSystemPrompt func() string
	// Version, ComputerName and ComputerID go into /meta.
	Version      string
	ComputerName string
	ComputerID   string
	// Scratch, optional, makes Scratch Workspaces; nil answers 404.
	Scratch Scratch
	// FindSessionFile, optional, is where the Daemon keeps a Session's
	// transcript, "" when it has none; nil answers 404.
	FindSessionFile func(sessionID string) string
}

// Meta is the /meta answer.
type Meta struct {
	App          string `json:"app"`
	Version      string `json:"version"`
	RemoteAccess bool   `json:"remoteAccess"`
	// Name is the computer's display name.
	Name string `json:"name"`
	// ComputerID survives restarts, Pairing Token resets and address changes.
	ComputerID string `json:"computerId"`
}

// Gateway keeps one listener on loopback and, only while Remote Access is
// on, one per LAN interface address. Binding interface addresses rather than
// 0.0.0.0 leaves the loopback listener untouched when Remote Access toggles
// and keeps the port closed to the network otherwise.
type Gateway struct {
	opts     Options
	port     int
	loopback *http.Server

	mu      sync.Mutex
	closed  bool
	lan     []lanListener
	bridges map[*bridge]struct{}
}

type lanListener struct {
	addr string
	srv  *http.Server
}

// Start binds the loopback listener, and the LAN listeners when
// opts.RemoteAccess is set.
func Start(opts Options) (*Gateway, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(loopback, strconv.Itoa(opts.Port)))
	if err != nil && opts.Port != 0 {
		ln, err = net.Listen("tcp", net.JoinHostPort(loopback, "0"))
	}
	if err != nil {
		return nil, err
	}
	g := &Gateway{opts: opts, port: ln.Addr().(*net.TCPAddr).Port, bridges: map[*bridge]struct{}{}}
	g.loopback = g.serve(ln)
	if opts.RemoteAccess {
		g.bindLAN()
	}
	return g, nil
}

// URL is the loopback address. A Remote Client is told a LAN address
// instead, by the pairing link.
func (g *Gateway) URL() string { return "http://" + net.JoinHostPort(loopback, strconv.Itoa(g.port)) }

// Port is the port every listener uses.
func (g *Gateway) Port() int { return g.port }

// RemoteAccess reports that LAN listeners are up.
func (g *Gateway) RemoteAccess() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.lan) > 0
}

// LANAddresses are the LAN addresses listening, in the order they were
// bound; empty while Remote Access is off.
func (g *Gateway) LANAddresses() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, len(g.lan))
	for i, l := range g.lan {
		out[i] = l.addr
	}
	return out
}

// SetRemoteAccess binds or unbinds the LAN listeners. Turning it off
// revokes every Pairing Token bridge at once; the loopback listener never
// moves.
func (g *Gateway) SetRemoteAccess(enabled bool) {
	if enabled {
		g.bindLAN()
		return
	}
	g.Revoke(TokenPairing)
	g.unbindLAN()
}

// Revoke ends every bridge that authenticated with this kind of token, as
// after a Pairing Token reset.
func (g *Gateway) Revoke(kind TokenKind) {
	g.mu.Lock()
	var ending []*bridge
	for b := range g.bridges {
		if b.kind == kind {
			ending = append(ending, b)
			delete(g.bridges, b)
		}
	}
	g.mu.Unlock()
	for _, b := range ending {
		b.terminate()
	}
}

// Close ends every bridge and listener.
func (g *Gateway) Close() error {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil
	}
	g.closed = true
	ending := g.bridges
	g.bridges = map[*bridge]struct{}{}
	g.mu.Unlock()
	for b := range ending {
		b.terminate()
	}
	g.unbindLAN()
	return shutdown(g.loopback)
}

func (g *Gateway) serve(ln net.Listener) *http.Server {
	srv := &http.Server{Handler: http.HandlerFunc(g.handle), ReadHeaderTimeout: time.Minute}
	go func() { _ = srv.Serve(ln) }()
	return srv
}

func shutdown(srv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return srv.Close()
	}
	return nil
}

func (g *Gateway) bindLAN() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	port := strconv.Itoa(g.port)
next:
	for _, addr := range LANInterfaceAddresses() {
		for _, l := range g.lan {
			if l.addr == addr {
				continue next
			}
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(addr, port))
		if err != nil {
			// An interface that refuses to bind is skipped; the others still serve.
			continue
		}
		g.lan = append(g.lan, lanListener{addr: addr, srv: g.serve(ln)})
	}
}

func (g *Gateway) unbindLAN() {
	g.mu.Lock()
	closing := g.lan
	g.lan = nil
	g.mu.Unlock()
	var wg sync.WaitGroup
	for _, l := range closing {
		wg.Go(func() { _ = shutdown(l.srv) })
	}
	wg.Wait()
}

// LANInterfaceAddresses are the IPv4 addresses of interfaces that are up
// and not loopback: the ones a phone could reach.
func LANInterfaceAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagRunning == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
					out = append(out, ip4.String())
				}
			}
		}
	}
	return out
}

func (g *Gateway) classify(presented string) TokenKind {
	if g.opts.PairingToken != nil && tokenMatches(presented, g.opts.PairingToken()) {
		return TokenPairing
	}
	if g.opts.LocalToken != nil && tokenMatches(presented, g.opts.LocalToken()) {
		return TokenLocal
	}
	return ""
}

func tokenMatches(presented, expected string) bool {
	if presented == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}

func (g *Gateway) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	if isUpgrade(r) {
		g.upgrade(w, r, path)
		return
	}
	q := r.URL.Query()
	switch path {
	case DaemonPath:
		status := http.StatusUnauthorized
		if g.classify(q.Get(TokenQuery)) != "" {
			status = http.StatusNoContent
		}
		reply(w, status, nil)
	case ScratchPath, ScratchTrashPath, ScratchRestorePath:
		status, body := g.answerScratch(r, path, q)
		reply(w, status, body)
	case SessionFilePath:
		status, body := g.answerSessionFile(r, q)
		reply(w, status, body)
	case MetaPath:
		reply(w, http.StatusOK, Meta{
			App:          "Droi",
			Version:      g.opts.Version,
			RemoteAccess: g.RemoteAccess(),
			Name:         g.opts.ComputerName,
			ComputerID:   g.opts.ComputerID,
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// reply answers cross-origin, so a Client served elsewhere can ask; no
// answer carries a secret.
func reply(w http.ResponseWriter, status int, body any) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Access-Control-Allow-Origin", "*")
	if body == nil {
		w.WriteHeader(status)
		return
	}
	h.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encodeJSON(body))
}

type errorBody struct {
	Error string `json:"error"`
}

type pathBody struct {
	Path string `json:"path"`
}

func (g *Gateway) answerScratch(r *http.Request, endpoint string, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, nil
	}
	scratch := g.opts.Scratch
	if scratch == nil {
		return http.StatusNotFound, nil
	}
	var err error
	if endpoint == ScratchPath {
		var path string
		if path, err = scratch.Create(); err == nil {
			return http.StatusCreated, pathBody{Path: path}
		}
	} else {
		path := q.Get(ScratchPathQuery)
		if path == "" {
			return http.StatusBadRequest, errorBody{Error: "No folder given"}
		}
		if endpoint == ScratchTrashPath {
			err = scratch.Trash(path)
		} else {
			err = scratch.Restore(path)
		}
		if err == nil {
			return http.StatusNoContent, nil
		}
	}
	var refused *NotAScratchWorkspace
	if errors.As(err, &refused) {
		return http.StatusBadRequest, errorBody{Error: err.Error()}
	}
	return http.StatusInternalServerError, errorBody{Error: err.Error()}
}

func (g *Gateway) answerSessionFile(r *http.Request, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	if r.Method != http.MethodGet {
		return http.StatusMethodNotAllowed, nil
	}
	id := q.Get(SessionIDQuery)
	if id == "" {
		return http.StatusBadRequest, nil
	}
	if g.opts.FindSessionFile != nil {
		if path := g.opts.FindSessionFile(id); path != "" {
			return http.StatusOK, pathBody{Path: path}
		}
	}
	return http.StatusNotFound, nil
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

func (g *Gateway) upgrade(w http.ResponseWriter, r *http.Request, path string) {
	if path != DaemonPath {
		rejectUpgrade(w, http.StatusNotFound, "Not Found")
		return
	}
	kind := g.classify(r.URL.Query().Get(TokenQuery))
	if kind == "" {
		rejectUpgrade(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	daemonURL := ""
	if g.opts.DaemonURL != nil {
		daemonURL = g.opts.DaemonURL()
	}
	if daemonURL == "" {
		rejectUpgrade(w, http.StatusServiceUnavailable, "Daemon Unavailable")
		return
	}
	// No origin check: the token is the lock, and the Phone App and a Client
	// served from another origin connect from elsewhere.
	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	g.runBridge(client, daemonURL, kind)
}

func rejectUpgrade(w http.ResponseWriter, status int, reason string) {
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nConnection: close\r\n\r\n", status, reason)
		_ = conn.Close()
		return
	}
	w.Header().Set("Connection", "close")
	w.WriteHeader(status)
}

// bridge pipes frames between a Client socket and its own Daemon socket.
type bridge struct {
	kind      TokenKind
	client    *websocket.Conn
	cancel    context.CancelFunc
	closeOnce sync.Once

	mu       sync.Mutex
	upstream *websocket.Conn
	done     bool
}

func (g *Gateway) runBridge(client *websocket.Conn, daemonURL string, kind TokenKind) {
	client.SetReadLimit(maxMessage)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &bridge{kind: kind, client: client, cancel: cancel}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		_ = client.CloseNow()
		return
	}
	g.bridges[b] = struct{}{}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.bridges, b)
		g.mu.Unlock()
	}()

	// Frames the Client sends meanwhile wait in the socket, in order.
	upstream, _, err := websocket.Dial(ctx, daemonURL, nil)
	if err != nil {
		b.closeBoth()
		return
	}
	upstream.SetReadLimit(maxMessage)
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		_ = upstream.CloseNow()
		return
	}
	b.upstream = upstream
	b.mu.Unlock()

	var wg sync.WaitGroup
	wg.Go(func() { b.toClient(ctx, upstream) })
	b.toDaemon(ctx, upstream, g.opts)
	wg.Wait()
}

// toDaemon forwards the Client's frames one at a time, so a frame waiting
// for its credential is never overtaken by the next.
func (b *bridge) toDaemon(ctx context.Context, upstream *websocket.Conn, opts Options) {
	for {
		typ, data, err := b.client.Read(ctx)
		if err != nil {
			b.closeBoth()
			return
		}
		if typ == websocket.MessageText {
			needsCredential := strings.Contains(string(data), APIKeyPlaceholder)
			startsSession := strings.Contains(string(data), initializeSession)
			if needsCredential && opts.Credential != nil {
				if cred, err := opts.Credential(ctx); err == nil {
					data = InjectCredential(data, cred)
				}
			}
			if startsSession && opts.AppendSystemPrompt != nil {
				data = InjectSystemPrompt(data, opts.AppendSystemPrompt())
			}
		}
		if err := upstream.Write(ctx, typ, data); err != nil {
			b.closeBoth()
			return
		}
	}
}

func (b *bridge) toClient(ctx context.Context, upstream *websocket.Conn) {
	for {
		typ, data, err := upstream.Read(ctx)
		if err != nil {
			b.closeBoth()
			return
		}
		if err := b.client.Write(ctx, typ, data); err != nil {
			b.closeBoth()
			return
		}
	}
}

// closeBoth closes both sockets with a close frame and no status, as one
// side closing or failing did in the Electron Gateway.
func (b *bridge) closeBoth() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.done = true
		upstream := b.upstream
		b.mu.Unlock()
		go func() { _ = b.client.Close(websocket.StatusNoStatusRcvd, "") }()
		if upstream != nil {
			go func() { _ = upstream.Close(websocket.StatusNoStatusRcvd, "") }()
		}
	})
}

// terminate drops both sockets at once, without a close handshake.
func (b *bridge) terminate() {
	b.cancel()
	b.mu.Lock()
	b.done = true
	upstream := b.upstream
	b.mu.Unlock()
	_ = b.client.CloseNow()
	if upstream != nil {
		_ = upstream.CloseNow()
	}
}
