package chatbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// toolServer serves the droi-chat tools as an HTTP MCP endpoint on loopback,
// stateless, as the TypeScript SDK serves in-process tools. Every Chat
// Session gets a bearer token of its own, which binds a call to its Chat
// Thread.
type toolServer struct {
	url string
	srv *http.Server
}

const (
	mcpPath            = "/mcp"
	mcpProtocolVersion = "2025-06-18"
	// maxMarkdownBytes is WeCom's limit for a Markdown message.
	maxMarkdownBytes = 20480
)

func startToolServer(b *Bridge) (*toolServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc(mcpPath, func(w http.ResponseWriter, r *http.Request) { serveMCP(b, w, r) })
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return &toolServer{url: "http://" + ln.Addr().String() + mcpPath, srv: srv}, nil
}

func (t *toolServer) close() { _ = t.srv.Close() }

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func rpcError(id json.RawMessage, code int, msg string) map[string]any {
	if id == nil {
		id = json.RawMessage("null")
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

func serveMCP(b *Bridge, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, rpcError(nil, -32000, "Method not allowed"))
		return
	}
	th := b.threadOf(bearer(r))
	if th == nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, rpcError(nil, -32603, "Unauthorized"))
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "Parse error"))
		return
	}
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := p.ProtocolVersion
		if version == "" {
			version = mcpProtocolVersion
		}
		result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "droi-chat", "version": "1"},
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": toolList}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeJSON(w, http.StatusOK, rpcError(req.ID, -32602, "Invalid params"))
			return
		}
		text, err := b.callTool(r.Context(), th, p.Name, p.Arguments)
		if err != nil {
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}
		} else {
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
		}
	default:
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32601, "Method not found: "+req.Method))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func bearer(r *http.Request) string {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return ""
	}
	return tok
}

// tool is one droi-chat tool as tools/list describes it.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

var toolList = []tool{
	{
		Name: "reply",
		Description: "Send a message to this Chat Thread in WeCom: in a WeCom Channel, a reply under the post; in a direct message, a message to the user. " +
			"It is the only way anything you write reaches the people there; " +
			"call it for progress on a long task and for your answer. Markdown, at most 20480 bytes. " +
			"A WeCom error, such as its rate limit of 30 messages a minute and 1000 an hour, comes back as a tool error: " +
			"the message was not sent.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "The message, in Markdown."},
			},
			"required":             []string{"text"},
			"additionalProperties": false,
		},
	},
	{
		Name: "read_thread",
		Description: "Read this Chat Thread in WeCom. In a WeCom Channel: the post (on the first page) and a page of its replies, oldest first, " +
			"each with its id, author and time (UTC); @ mentions show as @<user id> (@you is you) and files as [file: name]. " +
			"A page ending with a cursor has more: call again with that cursor. " +
			"In a direct message there is no history: new messages arrive in this Session.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cursor": map[string]any{"type": "string", "description": "The cursor the previous page ended with; omit for the first page."},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxReplyPage, "description": "Replies per page, 20 when omitted."},
			},
			"additionalProperties": false,
		},
	},
}

func (b *Bridge) callTool(ctx context.Context, th *thread, name string, args json.RawMessage) (string, error) {
	switch name {
	case "reply":
		var p struct {
			Text string `json:"text"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return b.reply(ctx, th, p.Text)
	case "read_thread":
		var p struct {
			Cursor string `json:"cursor"`
			Limit  int    `json:"limit"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return b.readThread(ctx, th, p.Cursor, p.Limit)
	}
	return "", fmt.Errorf("no tool %q", name)
}

// reply sends Markdown to the Chat Thread. In a direct message it goes
// under the latest message's req_id while WeCom still takes replies to it,
// else as a message of its own.
func (b *Bridge) reply(ctx context.Context, th *thread, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text is empty")
	}
	if len(text) > maxMarkdownBytes {
		return "", fmt.Errorf("text is %d bytes; WeCom takes at most %d", len(text), maxMarkdownBytes)
	}
	if th.kind == threadKindChannel {
		return b.replyInChannel(ctx, th, text)
	}
	b.mu.Lock()
	wc, reqID, at, user := b.wecom, th.reqID, th.reqAt, th.userID
	b.mu.Unlock()
	if wc == nil {
		return "", errNotConnected
	}
	markdown := map[string]any{"content": text}
	var err error
	if reqID != "" && b.o.Now().Sub(at) < replyWindow {
		_, err = wc.call(ctx, "aibot_respond_msg", reqID, map[string]any{"msgtype": "markdown", "markdown": markdown})
	} else {
		_, err = wc.call(ctx, "aibot_send_msg", "", map[string]any{"chatid": user, "chat_type": 1, "msgtype": "markdown", "markdown": markdown})
	}
	if err != nil {
		return "", fmt.Errorf("not sent: %w", err)
	}
	return "Sent to " + user + " in WeCom.", nil
}
