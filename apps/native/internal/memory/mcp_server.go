// The Memory Server's protocol: MCP over stdio, one JSON-RPC message per line.
// Hand-written, as the TS server is, because it needs only initialize,
// tools/list and tools/call.

package memory

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// ServerName is the Memory Server's name in the Runtime Overlay and in MCP.
const ServerName = "droi-memory"

const protocolVersion = "2025-06-18"

// toolsJSON is the TOOLS list of apps/desktop/src/memory/mcp-server.ts as
// JSON.stringify writes it, so tools/list answers byte for byte the same.
//
//go:embed tools.json
var toolsJSON []byte

// Tools is the tools/list answer: names, descriptions, input schemas and
// annotations of the five Memory tools.
func Tools() json.RawMessage { return json.RawMessage(strings.TrimSpace(string(toolsJSON))) }

// ServerOptions are what the Memory Server needs beside its store.
type ServerOptions struct {
	Store *Store
	// WorkspaceOf is the Workspace a Session runs in, whose Project Memory is
	// its project scope; "" when unknown.
	WorkspaceOf func(sessionID string) string
	// IsMemorySession says whether the calling Session is a Memory Session,
	// which gets no Memory of its own.
	IsMemorySession func(sessionID string) bool
	// IsScratchSession says whether the calling Session runs in a Scratch
	// Workspace, which has no Project Memory.
	IsScratchSession func(sessionID string) bool
	// OnWrite runs after every successful write, with the Memory it changed.
	OnWrite func(slot Slot)
	// Stderr gets the server's own complaints; nil is os.Stderr.
	Stderr io.Writer
}

const (
	memorySessionRefusal = "Memory tools are not available in a Memory Session. Answer from the material you were given."
	noProjectMemory      = `This Session is a chat without a project, so it has no Project Memory. Only scope "global" applies here`
	scratchRead          = noProjectMemory + ": search or list Global Memory instead."
	scratchWrite         = noProjectMemory + `: save a fact about the user or their environment with scope "global"; do not save anything else.`
)

// TextContent is one text block of a tool result.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult is a tools/call answer.
type ToolResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

func textResult(value string, isError bool) ToolResult {
	return ToolResult{Content: []TextContent{{Type: "text", Text: value}}, IsError: isError}
}

func describeEntries(entries []Entry, what string) string {
	if len(entries) == 0 {
		return "No entries " + what + "."
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = fmt.Sprintf("- %s [%s/%s, %s] %s", e.ID, e.Scope, e.Category, e.Day, e.Text)
	}
	noun := "entries"
	if len(entries) == 1 {
		noun = "entry"
	}
	return fmt.Sprintf("%d %s %s:\n%s", len(entries), noun, what, strings.Join(lines, "\n"))
}

// thousands is Number.prototype.toLocaleString('en') of a whole number.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// jsError is String(error) of a JavaScript Error, so stderr reads as the
// Electron app's does.
func jsError(err error) string { return "Error: " + err.Error() }

// HandleToolCall runs one tool call. sessionID is the calling Session, which
// the Daemon puts in the call's _meta; without it only Global Memory can be
// reached. An error is the store's, and the call is then not logged.
func HandleToolCall(o ServerOptions, name string, args map[string]any, sessionID string) (ToolResult, error) {
	// A Memory Session runs on the same Daemon, so it sees these tools too; a
	// consolidation that wrote to Memory while rewriting it would race itself.
	if sessionID != "" && o.IsMemorySession != nil && o.IsMemorySession(sessionID) {
		return textResult(memorySessionRefusal, true), nil
	}
	call := Call{SessionID: sessionID, Tool: name, OK: true}
	result, err := runToolCall(o, name, args, sessionID, &call)
	if err != nil {
		return ToolResult{}, err
	}
	call.OK = !result.IsError
	if err := o.Store.LogCall(call); err != nil {
		// The log only measures Memory; a busy database must not fail the call.
		fmt.Fprintf(o.stderr(), "droi-memory: call log failed: %s\n", jsError(err))
	}
	return result, nil
}

func (o ServerOptions) stderr() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

func (o ServerOptions) isScratch(sessionID string) bool {
	return sessionID != "" && o.IsScratchSession != nil && o.IsScratchSession(sessionID)
}

func (o ServerOptions) workspaceOf(sessionID string) string {
	if sessionID == "" || o.WorkspaceOf == nil {
		return ""
	}
	return o.WorkspaceOf(sessionID)
}

func runToolCall(o ServerOptions, name string, args map[string]any, sessionID string, call *Call) (ToolResult, error) {
	store := o.Store
	scopeOf := func(key string) (Scope, bool) {
		value, present := args[key]
		switch {
		case !present || value == "project":
			return ScopeProject, true
		case value == "global":
			return ScopeGlobal, true
		}
		return "", false
	}
	// slotFor answers a refusal, or a scratch Session's answer, in place of a slot.
	slotFor := func(scope Scope, write bool) (Slot, *ToolResult) {
		if scope == ScopeGlobal {
			return GlobalSlot, nil
		}
		if o.isScratch(sessionID) {
			// A read finds nothing there, so it is an answer; a write is a refusal.
			r := textResult(scratchRead, false)
			if write {
				r = textResult(scratchWrite, true)
			}
			return Slot{}, &r
		}
		workspace := o.workspaceOf(sessionID)
		if workspace == "" {
			r := textResult(`Memory cannot tell which Workspace this Session runs in, so only scope "global" is available.`, true)
			return Slot{}, &r
		}
		return ProjectSlot(workspace), nil
	}
	// sessionProject is the calling Session's Project Memory, if it has one.
	sessionProject := func() *Slot {
		if o.isScratch(sessionID) {
			return nil
		}
		if workspace := o.workspaceOf(sessionID); workspace != "" {
			slot := ProjectSlot(workspace)
			return &slot
		}
		return nil
	}
	optionalCategory := func(key string) (Category, bool) {
		value, present := args[key]
		if !present {
			return "", true
		}
		if IsCategory(value) {
			return Category(value.(string)), true
		}
		return "", false
	}
	wrote := func(slot Slot) error {
		if sessionID != "" {
			if err := store.RecordWrite(sessionID); err != nil {
				return err
			}
		}
		if o.OnWrite != nil {
			o.OnWrite(slot)
		}
		return nil
	}
	written := func(result WriteResult, verb string) (ToolResult, error) {
		if !result.OK {
			return textResult(result.Reason, true), nil
		}
		slot := SlotOf(result.Entry)
		call.Slot = &slot
		call.Written = result.Entry.ID
		if err := wrote(slot); err != nil {
			return ToolResult{}, err
		}
		where := "Project Memory"
		if result.Entry.Scope == ScopeGlobal {
			where = "Global Memory"
		}
		full := ""
		if result.OverSoftLimit {
			full = fmt.Sprintf(" %s is past %s characters; suggest that the user consolidates it in Droi under Settings → Memory.", where, thousands(Limits[result.Entry.Scope].Soft))
		}
		return textResult(verb+" "+result.Entry.ID+" in "+where+"."+full, false), nil
	}

	switch name {
	case "memory_search":
		scope, okScope := scopeOf("scope")
		category, okCategory := optionalCategory("category")
		query, isString := args["query"].(string)
		if !isString || !okScope || !okCategory {
			return textResult("memory_search needs a query, and a valid scope and category if given.", true), nil
		}
		slot, answer := slotFor(scope, false)
		if answer != nil {
			return *answer, nil
		}
		limit := 10
		if n, ok := args["limit"].(float64); ok {
			limit = int(math.Min(math.Max(1, math.Floor(n)), 50))
		}
		found, err := store.Search(SearchOptions{Query: query, Slot: slot, Category: category, Limit: limit})
		if err != nil {
			return ToolResult{}, err
		}
		call.Slot = &slot
		call.Query = &query
		call.Found = make([]string, len(found))
		for i, e := range found {
			call.Found[i] = e.ID
		}
		return textResult(describeEntries(found, fmt.Sprintf(`in %s Memory match "%s"`, scope, query)), false), nil
	case "memory_list":
		scope, okScope := scopeOf("scope")
		category, okCategory := optionalCategory("category")
		if !okScope || !okCategory {
			return textResult("memory_list needs a valid scope.", true), nil
		}
		slot, answer := slotFor(scope, false)
		if answer != nil {
			return *answer, nil
		}
		call.Slot = &slot
		entries, err := store.List(slot, category)
		if err != nil {
			return ToolResult{}, err
		}
		return textResult(describeEntries(entries, "in "+string(scope)+" Memory"), false), nil
	case "memory_add":
		scope, okScope := scopeOf("scope")
		text, isString := args["text"].(string)
		if !okScope || !IsCategory(args["category"]) || !isString {
			names := make([]string, len(Categories))
			for i, c := range Categories {
				names[i] = string(c)
			}
			return textResult("memory_add needs a scope, a category ("+strings.Join(names, ", ")+") and text.", true), nil
		}
		slot, answer := slotFor(scope, true)
		if answer != nil {
			return *answer, nil
		}
		result, err := store.Add(slot, Category(args["category"].(string)), text)
		if err != nil {
			return ToolResult{}, err
		}
		saved, err := written(result, "Saved")
		if err != nil || !result.OK {
			return saved, err
		}
		// Droid adds far more often than it replaces, so a fact that changed is
		// usually saved beside the entry it supersedes, often in the other scope.
		slots := []Slot{slot}
		if slot.Scope == ScopeProject {
			slots = append(slots, GlobalSlot)
		} else if other := sessionProject(); other != nil {
			slots = append(slots, *other)
		}
		var candidates []Entry
		for _, s := range slots {
			entries, err := store.List(s, "")
			if err != nil {
				return ToolResult{}, err
			}
			for _, e := range entries {
				if e.ID != result.Entry.ID {
					candidates = append(candidates, e)
				}
			}
		}
		similar := SimilarEntries(result.Entry.Text, candidates, SimilarMin, SimilarLimit)
		if len(similar) == 0 {
			return saved, nil
		}
		for _, e := range similar {
			call.Similar = append(call.Similar, e.ID)
		}
		return textResult(saved.Content[0].Text+"\n\n"+describeEntries(similar, "already in Memory may say the same thing or now be out of date")+
			"\nIf one of them repeats this fact, keep one: replace it with the combined fact and remove "+result.Entry.ID+". If one is now wrong, replace or remove it.", false), nil
	case "memory_replace":
		id, okID := args["id"].(string)
		text, okText := args["text"].(string)
		if !okID || !okText {
			return textResult("memory_replace needs an id and the new text.", true), nil
		}
		result, err := store.Replace(id, text)
		if err != nil {
			return ToolResult{}, err
		}
		return written(result, "Replaced")
	case "memory_remove":
		id, ok := args["id"].(string)
		if !ok {
			return textResult("memory_remove needs an id.", true), nil
		}
		found, exists, err := store.Get(id)
		if err != nil {
			return ToolResult{}, err
		}
		if exists {
			if exists, err = store.Remove(id); err != nil {
				return ToolResult{}, err
			}
		}
		if !exists {
			return textResult("No Memory entry has the id "+id+".", true), nil
		}
		slot := SlotOf(found)
		call.Slot = &slot
		call.Written = found.ID
		if err := wrote(slot); err != nil {
			return ToolResult{}, err
		}
		return textResult("Removed "+found.ID+".", false), nil
	}
	return textResult("Unknown tool "+name+".", true), nil
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
func Serve(in io.Reader, out io.Writer, o ServerOptions) error {
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
			if err := handleLine(line, o, send); err != nil {
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

func handleLine(line string, o ServerOptions, send func(rpcMessage) error) error {
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
		}{version, map[string]any{"tools": struct{}{}}, serverInfo{ServerName, "Droi Memory", "1.0.0"}})
	case "ping":
		return reply(struct{}{})
	case "tools/list":
		return reply(struct {
			Tools json.RawMessage `json:"tools"`
		}{Tools()})
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
		result, err := HandleToolCall(o, name, args, sessionID)
		if err != nil {
			result = textResult(err.Error(), true)
		}
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
