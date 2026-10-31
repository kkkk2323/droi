// Package mcpserver is a stdio MCP Server: MCP over standard input and output,
// one JSON-RPC message per line, answering initialize, ping, tools/list and
// tools/call. It is hand-written because that is all a Daemon asks of a tool
// server. The Memory Server (ADR 0010) is built on it.
//
// An app that ships its own tools runs a Server as a subcommand of its own
// executable, the way Droi runs `Droi memory-server`: an entry of an
// extension.Extension's Commands,
//
//	"my-tools": func([]string) int {
//		err := mcpserver.Serve(os.Stdin, os.Stdout, mcpserver.Server{
//			Name:    "my-tools",
//			Title:   "My Tools",
//			Version: "1.0.0",
//			Tools:   toolsJSON, // the "tools" of a tools/list answer, as JSON
//			Call: func(name string, args map[string]any, sessionID string) (mcpserver.Result, error) {
//				return mcpserver.Text("done", false), nil
//			},
//		})
//		if err != nil {
//			fmt.Fprintln(os.Stderr, err)
//			return 1
//		}
//		return 0
//	},
//
// which the Daemon starts once its Overlay lists the executable as a stdio
// server (host.MCPServer):
//
//	{"type": "stdio", "command": h.Executable(), "args": ["my-tools"]}
package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// protocolVersion answers an initialize that names none.
const protocolVersion = "2025-06-18"

// Server is a tool server's identity, its tools and the function that runs
// them.
type Server struct {
	// Name, Title and Version are the serverInfo of the initialize answer.
	Name, Title, Version string
	// Tools is the "tools" of the tools/list answer: a JSON array of names,
	// descriptions, input schemas and, for a tool whose Result carries
	// StructuredContent, output schemas.
	Tools json.RawMessage
	// Call runs one tool. sessionID is the calling Session, which the Daemon
	// puts in the call's _meta; it is "" when the call carries none. An error
	// reaches the caller as an error Result holding the error's text.
	Call func(name string, args map[string]any, sessionID string) (Result, error)
}

// Content is one text block of a Result.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Result is a tools/call answer.
type Result struct {
	Content []Content `json:"content"`
	// StructuredContent is the result as a JSON object, for a tool whose
	// tools/list entry declares an outputSchema; a value that is no object
	// once marshaled (nil, a nil map, null) is left out. A Daemon fails a
	// call of such a tool that answers without it, unless the call failed
	// (IsError). Content should still carry the same result as text, for
	// clients that read only that.
	StructuredContent any  `json:"structuredContent,omitempty"`
	IsError           bool `json:"isError,omitempty"`
}

// Text is a Result of one text block; isError marks it as the tool's failure.
func Text(text string, isError bool) Result {
	return Result{Content: []Content{{Type: "text", Text: text}}, IsError: isError}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcMessage keeps the TS key order: jsonrpc, id, then result or error. An id
// the request did not carry is left out, as JSON.stringify drops undefined.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

var nullID = json.RawMessage("null")

// Serve serves MCP on the two streams until the input ends.
func Serve(in io.Reader, out io.Writer, s Server) error {
	send := func(m rpcMessage) error {
		m.JSONRPC = "2.0"
		line, err := jsonText(m)
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, line+"\n")
		return err
	}
	reader := bufio.NewReader(in)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if jsTrim(line) != "" {
			if err := handleLine(line, s, send); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func handleLine(line string, s Server, send func(rpcMessage) error) error {
	var request map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		var other any
		if json.Unmarshal([]byte(line), &other) == nil {
			// Valid JSON that is no object carries no method and no id.
			return nil
		}
		return send(rpcMessage{ID: nullID, Error: &rpcError{-32700, "Parse error"}})
	}
	id := request["id"]
	isNotification := id == nil || string(id) == "null"
	var method any
	_ = json.Unmarshal(request["method"], &method)
	var params map[string]any
	_ = json.Unmarshal(request["params"], &params)
	reply := func(result any) error { return send(rpcMessage{ID: id, Result: result}) }
	fail := func(code int, message string) error {
		return send(rpcMessage{ID: id, Error: &rpcError{code, message}})
	}
	switch method {
	case "initialize":
		version, ok := params["protocolVersion"].(string)
		if !ok {
			version = protocolVersion
		}
		type serverInfo struct {
			Name    string `json:"name"`
			Title   string `json:"title"`
			Version string `json:"version"`
		}
		return reply(struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Capabilities    map[string]any `json:"capabilities"`
			ServerInfo      serverInfo     `json:"serverInfo"`
		}{version, map[string]any{"tools": struct{}{}}, serverInfo{s.Name, s.Title, s.Version}})
	case "ping":
		return reply(struct{}{})
	case "tools/list":
		return reply(struct {
			Tools json.RawMessage `json:"tools"`
		}{s.Tools})
	case "tools/call":
		name, ok := params["name"].(string)
		if !ok {
			return fail(-32602, "tools/call needs a name")
		}
		args, _ := params["arguments"].(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		meta, _ := params["_meta"].(map[string]any)
		sessionID, _ := meta["assemblySessionId"].(string)
		result, err := s.Call(name, args, sessionID)
		if err != nil {
			result = Text(err.Error(), true)
		}
		result.StructuredContent = jsonObject(result.StructuredContent)
		return reply(result)
	}
	if isNotification {
		return nil
	}
	name := "undefined"
	if raw, present := request["method"]; present {
		name = fmt.Sprint(method)
		if method == nil {
			name = string(raw)
		}
	}
	return fail(-32601, "Method not found: "+name)
}

// jsonObject is v as JSON when it marshals to an object, else nil: a typed
// nil would go out as null, which a Daemon refuses, and a value that does not
// marshal would end Serve.
func jsonObject(v any) any {
	if v == nil {
		return nil
	}
	text, err := jsonText(v)
	if err != nil || !strings.HasPrefix(text, "{") {
		return nil
	}
	return json.RawMessage(text)
}

// jsonText is JSON.stringify, which leaves <, > and & alone.
func jsonText(v any) (string, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// jsTrim is String.prototype.trim: JavaScript's whitespace includes U+3000 and
// U+FEFF, which unicode.IsSpace does not agree on.
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}
