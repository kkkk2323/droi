package chatbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
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
	th := b.chatThreadOf(bearer(r))
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
			"files attaches files of the Workspace (at most 8 files of up to 20 MB and 8 PNG, JPEG or GIF images of up to 10 MB): " +
			"in a Channel they come with the reply, in a direct message each follows the text as a message of its own. " +
			"mentions @s WeCom users by user id at the start of a Channel reply (at most 8; not in a direct message). " +
			"A WeCom error, such as its rate limit of 30 messages a minute and 1000 an hour, comes back as a tool error: " +
			"the message was not sent. In a WeCom Channel the result says when the post has replies you have not read.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "The message, in Markdown."},
				"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 2 * maxPerReply,
					"description": "Paths of files in the Workspace to send, relative to it or absolute."},
				"mentions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": maxPerReply,
					"description": "WeCom user ids to @ in a WeCom Channel."},
			},
			"required":             []string{"text"},
			"additionalProperties": false,
		},
	},
	{
		Name: "read_thread",
		Description: "Read this Chat Thread in WeCom. In a WeCom Channel: the post (on the first page) and a page of its replies, oldest first, " +
			"each with its id, author and time (UTC); @ mentions show as @<user id> (@you is you) and files as [file: name], " +
			"and the files and images of each are listed under Attachments with the id download_attachment takes. " +
			"A page ending with a cursor has more: call again with that cursor. " +
			"In a direct message there is no history: new messages arrive in this Session.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cursor": map[string]any{"type": "string", "description": "The cursor the previous page ended with; omit for the first page."},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxReplyPage, "description": "Replies per page, 20 when omitted."},
				"post_id": map[string]any{"type": "string",
					"description": "Another post of this WeCom Channel to read, such as one search_channel found; omit for this Chat Thread's post."},
			},
			"additionalProperties": false,
		},
	},
	{
		Name: "react",
		Description: "Put an emoji on this Chat Thread's post or on one of its replies in a WeCom Channel, or remove one you put; " +
			"it does not work in a direct message. WeCom's emoji are numbered 1 to 115; common ones: " +
			"69 收到 (got it), 3 DONE, 1 +1, 6 OK, 41 好, 48 强, 46 庆祝, 62 抱拳, 85 疑问, 115 鼓掌.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"emoji":    map[string]any{"type": "integer", "minimum": 1, "maximum": maxEmoji, "description": "The emoji's number."},
				"reply_id": map[string]any{"type": "string", "description": "The reply to react to, as read_thread shows it; omit for the post."},
				"remove":   map[string]any{"type": "boolean", "description": "Remove the emoji instead of putting it."},
			},
			"required":             []string{"emoji"},
			"additionalProperties": false,
		},
	},
	{
		Name: "search_channel",
		Description: "Search this Chat Thread's WeCom Channel for posts and replies whose text holds every keyword. " +
			"Each match comes with its post id (and reply ids) and a brief; read_thread with that post_id shows it in full. " +
			"A page ending with a cursor has more: call again with the same keywords and that cursor. Not in a direct message.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keywords": map[string]any{"type": "array", "items": map[string]any{"type": "string", "maxLength": maxKeywordBytes},
					"minItems": 1, "maxItems": maxSearchKeywords, "description": "Words the text holds, all of them; at most 32 bytes each."},
				"cursor": map[string]any{"type": "string", "description": "The cursor the previous page ended with; omit for the first page."},
			},
			"required":             []string{"keywords"},
			"additionalProperties": false,
		},
	},
	{
		Name: "download_attachment",
		Description: "Download a file or image of this Chat Thread into its folder under the Workspace (in " + attachmentsDir + ", which Git ignores) and answer the path, " +
			"to open with your other tools. The id is one listed under Attachments in a message from WeCom or in read_thread. " +
			"In a WeCom Channel the link is read again at each download; a direct message's link lasts 5 minutes after the message arrived.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "The attachment's id."},
			},
			"required":             []string{"id"},
			"additionalProperties": false,
		},
	},
}

func (b *Bridge) callTool(ctx context.Context, th *chatThread, name string, args json.RawMessage) (string, error) {
	switch name {
	case "reply":
		var p struct {
			Text     string   `json:"text"`
			Files    []string `json:"files"`
			Mentions []string `json:"mentions"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return b.reply(ctx, th, p.Text, p.Files, p.Mentions)
	case "read_thread":
		var p struct {
			Cursor string `json:"cursor"`
			Limit  int    `json:"limit"`
			PostID string `json:"post_id"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return b.readThread(ctx, th, p.PostID, p.Cursor, p.Limit)
	case "react":
		var p struct {
			Emoji   int    `json:"emoji"`
			ReplyID string `json:"reply_id"`
			Remove  bool   `json:"remove"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return b.react(ctx, th, p.Emoji, p.ReplyID, p.Remove)
	case "search_channel":
		var p struct {
			Keywords []string `json:"keywords"`
			Cursor   string   `json:"cursor"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return b.searchChannel(ctx, th, p.Keywords, p.Cursor)
	case "download_attachment":
		var p struct {
			ID string `json:"id"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
		}
		return b.downloadAttachment(ctx, th, p.ID)
	}
	return "", fmt.Errorf("no tool %q", name)
}

// reply sends Markdown, and files, to the Chat Thread. In a direct message
// it goes under the latest message's req_id while WeCom still takes
// replies to it, else as a message of its own.
func (b *Bridge) reply(ctx context.Context, th *chatThread, text string, files, mentions []string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text is empty")
	}
	if len(text) > maxMarkdownBytes {
		return "", fmt.Errorf("text is %d bytes; WeCom takes at most %d", len(text), maxMarkdownBytes)
	}
	if len(mentions) > 0 && th.kind != kindChannelPost {
		return "", errors.New("mentions work only in a WeCom Channel; this is a direct message")
	}
	if len(mentions) > maxPerReply || slices.Contains(mentions, "") {
		return "", fmt.Errorf("give at most %d user ids to mention", maxPerReply)
	}
	out, err := b.outgoingFiles(files)
	if err != nil {
		return "", err
	}
	if th.kind == kindChannelPost {
		return b.replyInChannel(ctx, th, text, out, mentions)
	}
	b.mu.Lock()
	wc, user := b.wecom, th.userID
	b.mu.Unlock()
	if wc == nil {
		return "", errNotConnected
	}
	ids, err := uploadAll(ctx, wc.uploadStep, out)
	if err != nil {
		return "", err
	}
	if err := b.sendDirect(ctx, th, wc, map[string]any{"msgtype": "markdown", "markdown": map[string]any{"content": text}}); err != nil {
		return "", fmt.Errorf("not sent: %w", err)
	}
	if err := b.sendDirectFiles(ctx, th, wc, out, ids); err != nil {
		return "", err
	}
	return "Sent to " + user + " in WeCom" + withFiles(out) + ".", nil
}

// sendDirect sends one message of a direct message.
func (b *Bridge) sendDirect(ctx context.Context, th *chatThread, wc *longConnection, body map[string]any) error {
	b.mu.Lock()
	reqID, at, user := th.reqID, th.reqAt, th.userID
	b.mu.Unlock()
	if reqID != "" && b.o.Now().Sub(at) < replyWindow {
		_, err := wc.call(ctx, "aibot_respond_msg", reqID, body)
		return err
	}
	body = maps.Clone(body)
	body["chatid"], body["chat_type"] = user, 1
	_, err := wc.call(ctx, "aibot_send_msg", "", body)
	return err
}
