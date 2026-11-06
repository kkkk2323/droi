// Package hosttools is the Host tool bridge: the MCP servers Droi carries for
// features of the Host's own, such as its Automations (ADR 0035) and
// Workstreams (ADR 0036). The Daemon runs each as `<droi executable>
// host-tools <name>`, a stdio MCP server (pkg/mcpserver) that forwards every
// call to the Host over loopback HTTP, where the feature runs it with the
// Host's Daemon connection and credential: as in ADR 0011, no tool process
// holds the credential.
package hosttools

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

// Command is the subcommand of the app's executable that serves one tool
// server: `<droi> host-tools <name>`.
const Command = "host-tools"

// The environment the Daemon starts a tool server with: where the Bridge
// listens and the token it asks for.
const (
	EnvURL   = "DROI_HOST_TOOLS_URL"
	EnvToken = "DROI_HOST_TOOLS_TOKEN"
)

// Server is one tool server a Host feature registers.
type Server struct {
	// Tools is the "tools" of the server's tools/list answer, as JSON. It is
	// read when the Daemon starts the server.
	Tools func() json.RawMessage
	// Call runs one tool for the Session sessionID ("" when the Daemon named
	// none). An error reaches the Session as an error Result holding its text.
	Call func(ctx context.Context, tool string, args map[string]any, sessionID string) (mcpserver.Result, error)
	// Autonomy and ToolAutonomy are the autonomy levels its tools run at
	// without asking: host.MCPServer's Autonomy and Tools.
	Autonomy     string
	ToolAutonomy map[string]string
}

// Bridge listens on loopback for the tool servers' calls, with a token made
// for this launch.
type Bridge struct {
	exe   string
	token string
	url   string
	srv   *http.Server

	mu      sync.Mutex
	servers map[string]Server
}

// New starts a Bridge. executable is the app's own, which the Daemon runs as
// each tool server; "" (go run) leaves them out of the Overlay, as Memory.
func New(executable string) (*Bridge, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := &Bridge{exe: executable, token: hex.EncodeToString(secret), url: "http://" + ln.Addr().String(), servers: map[string]Server{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /servers/{name}/tools", b.tools)
	mux.HandleFunc("POST /servers/{name}/call", b.call)
	b.srv = &http.Server{Handler: b.authorized(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = b.srv.Serve(ln) }()
	return b, nil
}

// Register adds a tool server under name, its name in the Daemon's MCP list
// and so its tools' prefix (<name>___<tool>). The Daemon reads the Overlay
// when it starts: register before the Host starts it.
func (b *Bridge) Register(name string, s Server) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.servers[name] = s
}

// URL is where the Bridge listens.
func (b *Bridge) URL() string { return b.url }

// Overlay is what the Bridge adds to the Runtime Overlay: each registered
// server, approved, run as the app's executable. Empty without one.
func (b *Bridge) Overlay() host.OverlayOptions {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exe == "" || len(b.servers) == 0 {
		return host.OverlayOptions{}
	}
	o := host.OverlayOptions{MCPServers: map[string]host.MCPServer{}, BlockOnMCPLoad: true}
	for name, s := range b.servers {
		o.MCPServers[name] = host.MCPServer{
			Config: map[string]any{
				"type":    "stdio",
				"command": b.exe,
				"args":    []string{Command, name},
				"env":     map[string]string{EnvURL: b.url, EnvToken: b.token},
			},
			Approved: true,
			Autonomy: s.Autonomy,
			Tools:    maps.Clone(s.ToolAutonomy),
		}
	}
	return o
}

// Close stops listening.
func (b *Bridge) Close() error { return b.srv.Close() }

// Selector is the disabledToolIds entries of a tool server: mcp:<server>
// for all of its tools, or mcp:<server>/<tool> for each tool named.
func Selector(server string, tools ...string) []string {
	if len(tools) == 0 {
		return []string{"mcp:" + server}
	}
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = "mcp:" + server + "/" + t
	}
	return out
}

func (b *Bridge) authorized(next http.Handler) http.Handler {
	want := []byte("Bearer " + b.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) server(w http.ResponseWriter, r *http.Request) (Server, bool) {
	name := r.PathValue("name")
	b.mu.Lock()
	s, ok := b.servers[name]
	b.mu.Unlock()
	if !ok {
		http.Error(w, "no tool server named "+name, http.StatusNotFound)
	}
	return s, ok
}

func (b *Bridge) tools(w http.ResponseWriter, r *http.Request) {
	s, ok := b.server(w, r)
	if !ok {
		return
	}
	tools := json.RawMessage("[]")
	if s.Tools != nil {
		if t := s.Tools(); len(t) > 0 {
			tools = t
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(tools)
}

// callRequest is a forwarded tools/call.
type callRequest struct {
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args"`
	SessionID string         `json:"sessionId"`
}

func (b *Bridge) call(w http.ResponseWriter, r *http.Request) {
	s, ok := b.server(w, r)
	if !ok {
		return
	}
	var req callRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Tool == "" {
		http.Error(w, "a call needs a tool", http.StatusBadRequest)
		return
	}
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	var (
		res mcpserver.Result
		err error
	)
	if s.Call == nil {
		err = errors.New("this server runs no tools")
	} else {
		res, err = s.Call(r.Context(), req.Tool, req.Args, req.SessionID)
	}
	if err != nil {
		res = mcpserver.Text(err.Error(), true)
	}
	body, err := json.Marshal(res)
	if err != nil {
		body, _ = json.Marshal(mcpserver.Text(err.Error(), true))
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
