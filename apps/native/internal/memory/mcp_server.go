// The Memory Server's tools; the protocol is pkg/mcpserver's.

package memory

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

// ServerName is the Memory Server's name in the Runtime Overlay and in MCP.
const ServerName = "droi-memory"

// toolsJSON began as the TOOLS list of apps/desktop/src/memory/mcp-server.ts
// as JSON.stringify writes it; the deprecated Electron app no longer follows
// it.
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
	scratchWrite         = noProjectMemory + `: save only a fact about the user that holds in every Workspace, with scope "global"; leave out what this chat is about.`
)

func describeEntries(entries []Entry, what string) string {
	if len(entries) == 0 {
		return "No entries " + what + "."
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		kind := string(e.Scope) + "/" + string(e.Category)
		if e.Topic != "" {
			kind += " · " + e.Topic
		}
		lines[i] = fmt.Sprintf("- %s [%s, %s] %s", e.ID, kind, e.Day, e.Text)
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
func HandleToolCall(o ServerOptions, name string, args map[string]any, sessionID string) (mcpserver.Result, error) {
	// A Memory Session runs on the same Daemon, so it sees these tools too; a
	// consolidation that wrote to Memory while rewriting it would race itself.
	if sessionID != "" && o.IsMemorySession != nil && o.IsMemorySession(sessionID) {
		return mcpserver.Text(memorySessionRefusal, true), nil
	}
	call := Call{SessionID: sessionID, Tool: name, OK: true}
	result, err := runToolCall(o, name, args, sessionID, &call)
	if err != nil {
		return mcpserver.Result{}, err
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

func runToolCall(o ServerOptions, name string, args map[string]any, sessionID string, call *Call) (mcpserver.Result, error) {
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
	slotFor := func(scope Scope, write bool) (Slot, *mcpserver.Result) {
		if scope == ScopeGlobal {
			return GlobalSlot, nil
		}
		if o.isScratch(sessionID) {
			// A read finds nothing there, so it is an answer; a write is a refusal.
			r := mcpserver.Text(scratchRead, false)
			if write {
				r = mcpserver.Text(scratchWrite, true)
			}
			return Slot{}, &r
		}
		workspace := o.workspaceOf(sessionID)
		if workspace == "" {
			r := mcpserver.Text(`Memory cannot tell which Workspace this Session runs in, so only scope "global" is available.`, true)
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
	// optionalTopic is the topic argument; a topic is only the Global Memory's.
	optionalTopic := func(scope Scope) (topic string, present bool, ok bool) {
		value, present := args["topic"]
		if !present {
			return "", false, true
		}
		topic, isString := value.(string)
		if !isString || (scope != ScopeGlobal && topic != "") {
			return "", true, false
		}
		return topic, true, true
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
	written := func(result WriteResult, verb string) (mcpserver.Result, error) {
		if !result.OK {
			return mcpserver.Text(result.Reason, true), nil
		}
		slot := SlotOf(result.Entry)
		call.Slot = &slot
		call.Written = result.Entry.ID
		if err := wrote(slot); err != nil {
			return mcpserver.Result{}, err
		}
		where := "Project Memory"
		if result.Entry.Scope == ScopeGlobal {
			where = "Global Memory"
		}
		full := ""
		if result.OverSoftLimit {
			full = fmt.Sprintf(" %s is past %s characters; suggest that the user consolidates it in Droi under Settings → Memory.", where, thousands(Limits[result.Entry.Scope].Soft))
		}
		return mcpserver.Text(verb+" "+result.Entry.ID+" in "+where+"."+full, false), nil
	}
	// Droid reaches for Global Memory for anything that sounds generally
	// useful, and a chat without a project has nowhere else to write, so a
	// Global write is answered with what belongs there.
	globalCheck := func(e Entry) string {
		if o.isScratch(sessionID) {
			if e.Topic != "" {
				return ""
			}
			return "\n\nThis chat has no project. Global Memory without a topic keeps only facts about the user that hold in every Workspace, shown in every Session. If this fact is about what this chat is about, give it a topic with memory_replace; if it is a one-off detail, remove " + e.ID + "."
		}
		if sessionProject() != nil {
			return "\n\nGlobal Memory is read in every Workspace. If this fact is about a project rather than the user, add it with scope project instead and remove " + e.ID + "."
		}
		return ""
	}
	// topicCheck answers a Global write with what the topics need: a new topic
	// may repeat one that exists, and the core's corrections share a budget.
	topicCheck := func(e Entry) (string, error) {
		if e.Scope != ScopeGlobal {
			return "", nil
		}
		layers, err := store.GlobalLayers()
		if err != nil {
			return "", err
		}
		note := ""
		if e.Topic != "" {
			var others []string
			isNew := false
			for _, t := range layers.Topics {
				if t.Topic == e.Topic {
					isNew = t.Entries == 1
				} else {
					others = append(others, fmt.Sprintf("%s (%d)", t.Topic, t.Entries))
				}
			}
			if isNew && len(others) > 0 {
				note += "\n\n" + e.Topic + " is a new topic. Global Memory already has: " + strings.Join(others, ", ") + ". If one of them fits, file " + e.ID + " there with memory_replace."
			}
		}
		if e.Topic == "" && e.Category == CategoryCorrection && layers.CoreCorrectionChars > GlobalCorrectionCaps.Chars {
			core, err := store.Corrections([]Slot{GlobalSlot}, CorrectionCaps{Entries: math.MaxInt32, Chars: math.MaxInt32})
			if err != nil {
				return "", err
			}
			note += fmt.Sprintf("\n\nGlobal corrections without a topic now take %s of the %s characters shown at the start of a Session, so the oldest are no longer shown. Shorten or merge them with memory_replace and memory_remove, or give one that matters only for one subject a topic.\n%s",
				thousands(layers.CoreCorrectionChars), thousands(GlobalCorrectionCaps.Chars), describeEntries(core, "are corrections without a topic"))
		}
		return note, nil
	}

	switch name {
	case "memory_search":
		scope, okScope := scopeOf("scope")
		category, okCategory := optionalCategory("category")
		topic, _, okTopic := optionalTopic(scope)
		query, isString := args["query"].(string)
		if !isString || !okScope || !okCategory {
			return mcpserver.Text("memory_search needs a query, and a valid scope and category if given.", true), nil
		}
		if !okTopic {
			return mcpserver.Text(topicOnlyGlobal+` Search with scope "global" to search a topic.`, true), nil
		}
		slot, answer := slotFor(scope, false)
		if answer != nil {
			return *answer, nil
		}
		limit := 10
		if n, ok := args["limit"].(float64); ok {
			limit = int(math.Min(math.Max(1, math.Floor(n)), 50))
		}
		found, err := store.Search(SearchOptions{Query: query, Slot: slot, Category: category, Topic: topic, Limit: limit})
		if err != nil {
			return mcpserver.Result{}, err
		}
		call.Slot = &slot
		call.Query = &query
		call.Found = make([]string, len(found))
		for i, e := range found {
			call.Found[i] = e.ID
		}
		return mcpserver.Text(describeEntries(found, fmt.Sprintf(`in %s Memory match "%s"`, scope, query)), false), nil
	case "memory_list":
		scope, okScope := scopeOf("scope")
		category, okCategory := optionalCategory("category")
		topic, _, okTopic := optionalTopic(scope)
		if !okScope || !okCategory {
			return mcpserver.Text("memory_list needs a valid scope.", true), nil
		}
		if !okTopic {
			return mcpserver.Text(topicOnlyGlobal+` List with scope "global" to list a topic.`, true), nil
		}
		slot, answer := slotFor(scope, false)
		if answer != nil {
			return *answer, nil
		}
		call.Slot = &slot
		entries, err := store.ListTopic(slot, category, topic)
		if err != nil {
			return mcpserver.Result{}, err
		}
		where := "in " + string(scope) + " Memory"
		if topic != "" {
			where += " under " + topic
		}
		return mcpserver.Text(describeEntries(entries, where), false), nil
	case "memory_add":
		scope, okScope := scopeOf("scope")
		text, isString := args["text"].(string)
		if !okScope || !IsCategory(args["category"]) || !isString {
			names := make([]string, len(Categories))
			for i, c := range Categories {
				names[i] = string(c)
			}
			return mcpserver.Text("memory_add needs a scope, a category ("+strings.Join(names, ", ")+") and text.", true), nil
		}
		topic, _, okTopic := optionalTopic(scope)
		if !okTopic {
			return mcpserver.Text(topicOnlyGlobal+" A project's Memory is already about that project.", true), nil
		}
		slot, answer := slotFor(scope, true)
		if answer != nil {
			return *answer, nil
		}
		result, err := store.AddWithTopic(slot, Category(args["category"].(string)), text, topic)
		if err != nil {
			return mcpserver.Result{}, err
		}
		saved, err := written(result, "Saved")
		if err != nil || !result.OK {
			return saved, err
		}
		if slot.Scope == ScopeGlobal {
			note, err := topicCheck(result.Entry)
			if err != nil {
				return mcpserver.Result{}, err
			}
			saved.Content[0].Text += note + globalCheck(result.Entry)
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
				return mcpserver.Result{}, err
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
		return mcpserver.Text(saved.Content[0].Text+"\n\n"+describeEntries(similar, "already in Memory may say the same thing or now be out of date")+
			"\nIf one of them repeats this fact, keep one: replace it with the combined fact and remove "+result.Entry.ID+". If one is now wrong, replace or remove it.", false), nil
	case "memory_replace":
		id, okID := args["id"].(string)
		text, okText := args["text"].(string)
		rawTopic, hasTopic := args["topic"]
		topic, okTopic := rawTopic.(string)
		_, hasText := args["text"]
		if !okID || (hasText && !okText) || (hasTopic && !okTopic) || (!hasText && !hasTopic) {
			return mcpserver.Text("memory_replace needs an id, and the new text, the new topic or both.", true), nil
		}
		// A topic that would be refused must not leave the text changed.
		if hasTopic {
			found, exists, err := store.Get(id)
			if err != nil {
				return mcpserver.Result{}, err
			}
			_, refused := cleanTopic(topic)
			if exists && found.Scope != ScopeGlobal {
				refused = topicOnlyGlobal
			}
			if refused != "" {
				return mcpserver.Text(refused, true), nil
			}
		}
		var result WriteResult
		var err error
		if hasText {
			if result, err = store.Replace(id, text); err != nil {
				return mcpserver.Result{}, err
			}
		}
		if hasTopic && (!hasText || result.OK) {
			if result, err = store.SetTopic(id, topic); err != nil {
				return mcpserver.Result{}, err
			}
		}
		replaced, err := written(result, "Replaced")
		if err != nil || !result.OK {
			return replaced, err
		}
		note, err := topicCheck(result.Entry)
		if err != nil {
			return mcpserver.Result{}, err
		}
		replaced.Content[0].Text += note
		return replaced, nil
	case "memory_remove":
		id, ok := args["id"].(string)
		if !ok {
			return mcpserver.Text("memory_remove needs an id.", true), nil
		}
		found, exists, err := store.Get(id)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if exists {
			if exists, err = store.Remove(id); err != nil {
				return mcpserver.Result{}, err
			}
		}
		if !exists {
			return mcpserver.Text("No Memory entry has the id "+id+".", true), nil
		}
		slot := SlotOf(found)
		call.Slot = &slot
		call.Written = found.ID
		if err := wrote(slot); err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Text("Removed "+found.ID+".", false), nil
	}
	return mcpserver.Text("Unknown tool "+name+".", true), nil
}

// Serve serves the Memory Server's MCP on the two streams until the input ends.
func Serve(in io.Reader, out io.Writer, o ServerOptions) error {
	return mcpserver.Serve(in, out, mcpserver.Server{
		Name:    ServerName,
		Title:   "Droi Memory",
		Version: "1.0.0",
		Tools:   Tools(),
		Call: func(name string, args map[string]any, sessionID string) (mcpserver.Result, error) {
			return HandleToolCall(o, name, args, sessionID)
		},
	})
}
