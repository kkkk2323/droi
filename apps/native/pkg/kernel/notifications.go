package kernel

import "encoding/json"

// Notification is one notification Kernel sent: its method, and its params
// undecoded, in the order they arrived.
type Notification struct {
	Method string
	Params json.RawMessage
}

// Decode reads the params into v, which is one of the types below.
func (n Notification) Decode(v any) error {
	if len(n.Params) == 0 {
		return nil
	}
	return json.Unmarshal(n.Params, v)
}

// ViewNotification is conversation.view: a conversation's whole view after a
// commit, or, with Kind "failed", that Kernel could not read it. Consume Value
// and ignore Ops (design §2.3).
type ViewNotification struct {
	ID    ID              `json:"id"`
	Kind  string          `json:"kind"`
	Value *View           `json:"value,omitempty"`
	Ops   json.RawMessage `json:"ops,omitempty"`
	// Retired says the conversation's watch is over.
	Retired bool `json:"retired,omitempty"`
	// Error is why a failed view failed: {message, code}.
	Error json.RawMessage `json:"error,omitempty"`
}

// Failed reports whether Kernel could not read the conversation.
func (n ViewNotification) Failed() bool { return n.Kind == "failed" }

// BusyNotification is conversation.busy: the run that started, or null when
// the conversation went idle. RunFailure says why the run that just ended
// failed.
type BusyNotification struct {
	ID         ID              `json:"id"`
	Run        json.RawMessage `json:"run"`
	RunFailure json.RawMessage `json:"runFailure,omitempty"`
}

// Busy reports whether a run is in progress.
func (n BusyNotification) Busy() bool {
	return len(n.Run) > 0 && string(n.Run) != "null"
}

// ApprovalRequested is approval.requested: a tool waits for an answer. It goes
// to every connection, and the first answer wins.
type ApprovalRequested struct {
	RequestID string          `json:"requestId"`
	ID        ID              `json:"id"`
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args,omitempty"`
	// Options are the answers Kernel offers, in ApprovalRespondParams'
	// spelling.
	Options []ApprovalOption `json:"options,omitempty"`
	// ScopeHint is what kind of memory an "always" answer takes: "ask",
	// "file" or "tool".
	ScopeHint string `json:"scopeHint,omitempty"`
}

// ApprovalOption is one answer an approval offers.
type ApprovalOption struct {
	ID    string         `json:"id"`
	Label string         `json:"label"`
	Scope *ApprovalScope `json:"scope,omitempty"`
}

// ApprovalResolved is approval.resolved: the request is answered, here or on
// another connection.
type ApprovalResolved struct {
	RequestID string `json:"requestId"`
	ID        ID     `json:"id"`
	Outcome   string `json:"outcome"`
	// By says which Client answered, when Kernel tells.
	By string `json:"by,omitempty"`
}

// AskRequested is ask.requested: the ask_user tool's questions. It goes to
// every connection, and the first answer wins.
type AskRequested struct {
	RequestID string     `json:"requestId"`
	ID        ID         `json:"id"`
	Questions []Question `json:"questions"`
	// Context is what the model said about why it asks.
	Context json.RawMessage `json:"context,omitempty"`
}

// Question is one question of an ask.requested.
type Question struct {
	QuestionID  string           `json:"questionId"`
	Question    string           `json:"question"`
	Options     []QuestionChoice `json:"options,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
}

// QuestionChoice is one option of a question.
type QuestionChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// AskResolved is ask.resolved: the questions are answered, or were dismissed.
type AskResolved struct {
	RequestID string      `json:"requestId"`
	ID        ID          `json:"id"`
	Answers   []AskAnswer `json:"answers,omitempty"`
	Cancelled bool        `json:"cancelled,omitempty"`
}

// TaskChanged is task.changed: a task of the graph (a compaction, a subagent,
// an afterRun) changed status.
type TaskChanged struct {
	TaskID         ID     `json:"taskId"`
	Kind           string `json:"kind,omitempty"`
	Status         string `json:"status"`
	ConversationID ID     `json:"conversationId,omitempty"`
}

// MCPChanged is mcp.changed: the MCP server list, after a connection or an
// authorization.
type MCPChanged = MCPServers

// ProcessChanged is process.changed: a background process wrote output, or
// exited.
type ProcessChanged struct {
	ID        ID     `json:"id"`
	ProcessID string `json:"processId"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	// Tail is the process's newest output.
	Tail string `json:"tail,omitempty"`
}

// IndexChanged is index.changed: Droi's metadata of a conversation moved, so a
// list redraws its row.
type IndexChanged struct {
	ID             ID      `json:"id"`
	Title          *string `json:"title,omitempty"`
	Archived       *bool   `json:"archived,omitempty"`
	LastActivityAt int64   `json:"lastActivityAt,omitempty"`
}

// Warning is Kernel's own report: Pi onReport's kinds, such as storage_failed,
// task_error or extension_error.
type Warning struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
