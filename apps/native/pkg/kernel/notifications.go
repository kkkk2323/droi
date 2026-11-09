package kernel

import "encoding/json"

// Notification is one notification Kernel sent: its method, and its params
// undecoded, in the order they arrived.
type Notification struct {
	Method string
	Params json.RawMessage

	// Folded is how many earlier notifications this one stands for: states of
	// the same conversation, or news of the same task or process, that arrived
	// while the reader had not taken the previous one. The newest state is the
	// one that matters, so the earlier ones were folded rather than queued, and
	// this is the count of what that saved. It is local to this Client and
	// never part of the wire.
	Folded int `json:"-"`
	// Dropped is how many notifications of this kind were lost before this one:
	// warnings past the newest 256, or unknown methods past the newest 16.
	// Local to this Client, never part of the wire.
	Dropped int `json:"-"`
}

// Decode reads the params into v, which is one of the types below.
func (n Notification) Decode(v any) error {
	if len(n.Params) == 0 {
		return nil
	}
	return json.Unmarshal(n.Params, v)
}

// ViewNotification is conversation.view: a conversation's view after a commit,
// in the shape the connection's subscription asked for.
//
// A "view" subscriber reads Value and ignores Ops. A "delta" subscriber
// applies Ops to the state it keeps, and replaces that state whenever Kind is
// ViewKindFull — Kernel sends one as soon as it cannot prove the ops are
// continuous with what the subscriber holds, so the subscriber never has to
// guess. With ViewKindFailed, Kernel could not read the conversation at all.
type ViewNotification struct {
	ID   ID     `json:"id"`
	Kind string `json:"kind"`
	// Value is the whole view for a "view" subscriber and for ViewKindFull;
	// null for a "delta" subscriber's ViewKindValue push.
	Value *View           `json:"value,omitempty"`
	Ops   json.RawMessage `json:"ops,omitempty"`
	// Retired says the conversation's watch is over.
	Retired bool `json:"retired,omitempty"`
	// Error is why a failed view failed: {message, code}.
	Error json.RawMessage `json:"error,omitempty"`
}

// Failed reports whether Kernel could not read the conversation.
func (n ViewNotification) Failed() bool { return n.Kind == ViewKindFailed }

// Whole reports whether the push carries a whole view, which replaces whatever
// the subscriber kept: every push to a "view" subscriber, and ViewKindFull.
func (n ViewNotification) Whole() bool {
	return n.Value != nil && (n.Kind == ViewKindValue || n.Kind == ViewKindFull)
}

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
	// ScopeHint is the autonomy level that made this call ask ("ask",
	// "edits" or "safe"), so a card can say why it is asking. What an "always"
	// answer remembers is each option's Scope.
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
	// By is the X-Droi-Client of the connection that answered ("window",
	// "phone", "host"), "unknown" for one that sent none, and "kernel" for a
	// request the kernel settled itself (a timeout, a restart).
	By string `json:"by,omitempty"`
}

// AskRequested is ask.requested: the ask_user tool's questions. It goes to
// every connection, and the first answer wins.
type AskRequested struct {
	RequestID string     `json:"requestId"`
	ID        ID         `json:"id"`
	Questions []Question `json:"questions"`
	// Context is the sentence that says what the answer is for. The contract
	// makes it required and non-empty, because a question without one cannot
	// be answered well.
	Context string `json:"context"`
	// Blocking says the run waits for the answer, so the card takes the
	// composer's place; a non-blocking ask sits above it and its answer arrives
	// as the next turn.
	Blocking bool `json:"blocking,omitempty"`
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
	// By is who answered, as ApprovalResolved's.
	By string `json:"by,omitempty"`
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

// ProcessChanged is process.changed: a background process started or ended.
// The end carries the newest output as Tail.
type ProcessChanged struct {
	ID        ID     `json:"id"`
	ProcessID string `json:"processId"`
	Status    string `json:"status"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	// Tail is the process's newest output.
	Tail string `json:"tail,omitempty"`
}

// MemoryRead is memory.read: the model read a Session Summary, File as its path in the Memory folder, at At (an ISO
// time).
type MemoryRead struct {
	File string `json:"file"`
	At   string `json:"at"`
}

// IndexChanged is index.changed: Droi's metadata of a conversation moved, so a
// list redraws its row.
type IndexChanged struct {
	ID    ID      `json:"id"`
	Title *string `json:"title,omitempty"`
	// TitleSource is where Title came from, TitleSourceAuto or TitleSourceChosen:
	// a title generator may only replace an automatic one.
	TitleSource    string `json:"titleSource,omitempty"`
	Archived       *bool  `json:"archived,omitempty"`
	LastActivityAt int64  `json:"lastActivityAt,omitempty"`
	// ViewedAt is when a person last saw the conversation, nil while nobody has: unread while LastActivityAt is
	// later.
	ViewedAt *int64 `json:"viewedAt"`
}

// Warning is Kernel's own report: Pi onReport's kinds, such as storage_failed,
// task_error or extension_error.
type Warning struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// The kinds a conversation.view push takes.
const (
	// ViewKindValue carries the value for a "view" subscriber, or only ops
	// for a "delta" one.
	ViewKindValue = "value"
	// ViewKindFull carries the whole value after ops a "delta" subscriber
	// could not merge. A subscriber replaces its state with it.
	ViewKindFull = "full"
	// ViewKindFailed says Kernel could not read the conversation.
	ViewKindFailed = "failed"
)
