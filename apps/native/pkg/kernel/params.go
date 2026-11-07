package kernel

import (
	"encoding/json"
	"strconv"
)

// The methods' params and results, in the contract's shapes.

// ModelRef names a model of one provider, as an agent stores it.
type ModelRef struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// NameSet adds and removes names: extensions, tools.
type NameSet struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// AgentChange sets part of an agent. The contract gives every field three
// states: absent leaves it as it is, null clears it, and a value replaces it.
// So every field is a *Field — nil for absent, Some(v) to set it, None to
// clear it — and a change that only sets the model sends nothing else.
type AgentChange struct {
	Model         *Field[ModelRef] `json:"model,omitempty"`
	ThinkingLevel *Field[string]   `json:"thinkingLevel,omitempty"`
	Extensions    *Field[NameSet]  `json:"extensions,omitempty"`
	ModelTools    *Field[NameSet]  `json:"modelTools,omitempty"`
	Instructions  *Field[string]   `json:"instructions,omitempty"`
	Cwd           *Field[string]   `json:"cwd,omitempty"`
	MaxTurns      *Field[int]      `json:"maxTurns,omitempty"`
}

// Agent is an agent as conversation.configure resolves it: names, not objects.
type Agent struct {
	Model         *ModelRef `json:"model,omitempty"`
	ThinkingLevel string    `json:"thinkingLevel,omitempty"`
	Extensions    []string  `json:"extensions,omitempty"`
	Tools         []string  `json:"tools,omitempty"`
	ModelTools    *NameSet  `json:"modelTools,omitempty"`
	Instructions  *string   `json:"instructions,omitempty"`
	Cwd           string    `json:"cwd,omitempty"`
	MaxTurns      *int      `json:"maxTurns,omitempty"`
}

// CreateParams is conversation.create's params.
type CreateParams struct {
	Agent        *AgentChange `json:"agent,omitempty"`
	Cwd          string       `json:"cwd,omitempty"`
	Instructions string       `json:"instructions,omitempty"`
	// Kind is Droi's own classification: "chat", "automation",
	// "workstream", "memory", "scratch".
	Kind      string `json:"kind,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// CreateResult is the new conversation and its first view.
type CreateResult struct {
	ConversationID ID   `json:"conversationId"`
	View           View `json:"view"`
}

// ListParams is conversation.list's params: one SQL over Droi's metadata
// table. Cursor is opaque and goes back verbatim.
type ListParams struct {
	Kind     string `json:"kind,omitempty"`
	Archived *bool  `json:"archived,omitempty"`
	// Workspace is the conversation's cwd, exactly as it was resolved.
	Workspace string `json:"workspace,omitempty"`
	// RepoRoot is the working tree a conversation's cwd sits in, so a
	// repository's worktrees list as one group.
	RepoRoot string `json:"repoRoot,omitempty"`
	// Parent lists that conversation's children instead of the top-level rows.
	// Without it the page holds only conversations nobody owns (ADR 0043).
	Parent *ID             `json:"parent,omitempty"`
	Query  string          `json:"query,omitempty"`
	Cursor json.RawMessage `json:"cursor,omitempty"`
	Limit  int             `json:"limit,omitempty"`
}

// ListResult is one page of the conversation list.
type ListResult struct {
	Items  []Summary       `json:"items"`
	Cursor json.RawMessage `json:"cursor,omitempty"`
}

// Summary is one row of Droi's metadata table. The times are milliseconds
// since the Unix epoch.
type Summary struct {
	ID    ID     `json:"id"`
	Title string `json:"title,omitempty"`
	Kind  string `json:"kind,omitempty"`
	// Workspace is the conversation's cwd: where its tools run.
	Workspace string `json:"workspace,omitempty"`
	// RepoRoot is the working tree Workspace sits in, or Workspace itself when it
	// is not inside one. It is what groups a repository's worktrees together.
	RepoRoot string `json:"repoRoot,omitempty"`
	// ParentID is lineage: the conversation this one was forked from. A fork is a
	// list row of its own, so ParentID says nothing about whether a row belongs in
	// the main list; OwnerID does.
	ParentID *ID `json:"parentId,omitempty"`
	// OwnerID is the conversation that spawned this one (a subagent, a Workstream
	// agent). conversation.list leaves owned rows out unless asked with Parent, so a
	// row with an OwnerID is never a main-list row.
	OwnerID        *ID   `json:"ownerId,omitempty"`
	Archived       bool  `json:"archived"`
	CreatedAt      int64 `json:"createdAt,omitempty"`
	LastActivityAt int64 `json:"lastActivityAt,omitempty"`
	// TitleSource says where Title came from: TitleSourceAuto is the kernel's own
	// guess (the first line of the first user message), TitleSourceChosen is a title a
	// person set. Absent while the conversation has no title.
	TitleSource string `json:"titleSource,omitempty"`
	// ViewedAt is when a person last saw the conversation; nil is "not yet", which
	// is what makes the unread dot show.
	ViewedAt *int64 `json:"viewedAt,omitempty"`
	// Preview is the first line of the newest message — the answer once there is
	// one, the question while it is still being written — for the list's second line.
	Preview string `json:"preview,omitempty"`
}

// Window is how many of the newest entries an open asks for: at least Min,
// and the conversation's view carries up to Max.
type Window struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// How a connection wants its conversation.view pushes: the whole window, or
// only the ops that changed it.
const (
	// SubscribeView sends the whole view on every push. It is the default: the
	// window sits on the machine that runs Kernel and redraws cheaply.
	SubscribeView = "view"
	// SubscribeDelta sends only Pi's ops, with value null, for a client that
	// reaches Kernel over the Relay and keeps its own state (the phone, M5).
	// A push whose ops it cannot merge with what it holds arrives as a
	// kind "full" view instead, so it never has to guess.
	SubscribeDelta = "delta"
)

// OpenParams is conversation.open's params: it subscribes this connection and
// answers its first view.
type OpenParams struct {
	ID     ID      `json:"id"`
	Window *Window `json:"window,omitempty"`
	// Subscribe is "view" (the default, and "" for it) or "delta". The answer
	// is always a whole view, so a delta subscriber starts from one.
	Subscribe string `json:"subscribe,omitempty"`
}

// OpenResult is the first view of an opened conversation.
type OpenResult struct {
	View View `json:"view"`
}

// IDParams is the params of the methods that take one conversation:
// conversation.unwatch and conversation.abort.
type IDParams struct {
	ID ID `json:"id"`
}

// ContentBlock is one block of a submission: a text, or an image as base64.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// TextBlock is a text block of a submission.
func TextBlock(text string) ContentBlock { return ContentBlock{Type: "text", Text: text} }

// ImageBlock is an image block of a submission.
func ImageBlock(data []byte, mimeType string) ContentBlock {
	return ContentBlock{Type: "image", Data: encodeBase64(data), MimeType: mimeType}
}

// SubmitParams is conversation.submit's params. Input is a string or a list of
// content blocks; Continue asks for a run on the context as it is, with no new
// user message. WhenBusy is "steer", "followUp" or "reject" and WhenIdle is
// "reject" or ""; anything else is Kernel's -32602, so these constants are the
// only values this client sends.
type SubmitParams struct {
	ID        ID     `json:"id"`
	Input     any    `json:"input,omitempty"`
	Continue  bool   `json:"continue,omitempty"`
	WhenBusy  string `json:"whenBusy,omitempty"`
	WhenIdle  string `json:"whenIdle,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// What a submission does when the conversation's run is in progress. Kernel
// accepts these three and nothing else: a fourth value is -32602 invalid
// params, as the integration run found with a "queue" this client had invented.
const (
	// WhenBusySteer joins the running turn.
	WhenBusySteer = "steer"
	// WhenBusyFollowUp adds a turn after this one, which is what a queued
	// message is.
	WhenBusyFollowUp = "followUp"
	// WhenBusyReject answers -32001 conversation busy.
	WhenBusyReject = "reject"
)

// What a submission does when the conversation is idle. Kernel accepts this one
// and "" (the run as asked); the other values this client once invented were
// its own, and Kernel refused them.
const (
	// WhenIdleReject answers -32002 conversation idle.
	WhenIdleReject = "reject"
)

// SubmitResult names the submission Kernel queued.
type SubmitResult struct {
	SubmissionID ID `json:"submissionId"`
}

// AbortParams is conversation.abort's params: stop what runs, or with
// Background also the tasks that run on after the turn.
type AbortParams struct {
	ID         ID   `json:"id"`
	Background bool `json:"background,omitempty"`
}

// AbortSubmissionParams is submission.abort's params.
type AbortSubmissionParams struct {
	ID           ID `json:"id"`
	SubmissionID ID `json:"submissionId"`
}

// ImportParams is conversation.import's params. Without ID it creates a conversation; with
// ID it appends one batch to an existing one.
//
// Entries are Pi entry drafts, sent as JSON: the client does not model them, because
// what an imported history holds is the caller's own data (Entries, Model, Data).
type ImportParams struct {
	// ID is the conversation to append to; absent means create.
	ID ID `json:"id,omitempty"`
	// Key makes the batch idempotent. A replay answers the first call's result.
	Key string `json:"key,omitempty"`
	// Entries is the batch: at most Limits.MaxImportEntriesPerCall.
	Entries []ImportEntry `json:"entries"`
	// The fields below create the conversation and are refused on an append.
	Agent        *AgentChange `json:"agent,omitempty"`
	Cwd          string       `json:"cwd,omitempty"`
	Kind         string       `json:"kind,omitempty"`
	Title        string       `json:"title,omitempty"`
	Instructions *string      `json:"instructions,omitempty"`
	Archived     *bool        `json:"archived,omitempty"`
	// CreatedAt is the conversation's own beginning, in epoch milliseconds. Absent: its
	// first message's time.
	CreatedAt *int64 `json:"createdAt,omitempty"`
}

// ImportEntry is one entry an import writes: Pi's EntryDraft, with its fields kept as
// JSON so a caller can write shapes this package does not name.
type ImportEntry struct {
	Kind  string          `json:"kind"`
	Model json.RawMessage `json:"model,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Edits json.RawMessage `json:"edits,omitempty"`
	Head  json.RawMessage `json:"head,omitempty"`
}

// ImportResult is what an import wrote.
type ImportResult struct {
	ConversationID ID `json:"conversationId"`
	// EntryIDs are the IDs the batch's entries got, in order. On a replay they are the
	// first call's.
	EntryIDs []ID `json:"entryIds"`
	// Replayed says the key had already been used: nothing was written.
	Replayed bool `json:"replayed,omitempty"`
}

// SubmissionGet is where one submission got to.
//
// Status is Pi's own: queued, placed, done or unanswered. Entry is the user
// entry the submission produced and Answer the assistant entry that answered
// it; both are absent while the submission is queued. Reason explains an
// unanswered one.
type SubmissionGet struct {
	SubmissionID ID     `json:"submissionId"`
	Status       string `json:"status"`
	Entry        *ID    `json:"entry,omitempty"`
	Answer       *ID    `json:"answer,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// ConfigureParams is conversation.configure's params.
type ConfigureParams struct {
	ID     ID           `json:"id"`
	Change *AgentChange `json:"change"`
}

// ConfigureResult is the agent the conversation now runs with.
type ConfigureResult struct {
	Agent Agent `json:"agent"`
}

// CompactParams is conversation.compact's params: Kernel starts a compaction
// task and answers its id.
type CompactParams struct {
	ID           ID     `json:"id"`
	Instructions string `json:"instructions,omitempty"`
}

// CompactResult names the compaction task.
type CompactResult struct {
	TaskID ID `json:"taskId"`
}

// ForkParams is conversation.fork's params: At is the entry the new
// conversation copies up to.
type ForkParams struct {
	ID ID `json:"id"`
	At ID `json:"at"`
}

// ForkResult is the forked conversation and its view.
type ForkResult struct {
	ConversationID ID   `json:"conversationId"`
	View           View `json:"view"`
}

// EntriesParams is conversation.entries' params: a page of entries. Query and
// Cursor are Pi's, and go back verbatim.
type EntriesParams struct {
	ID     ID              `json:"id"`
	Query  json.RawMessage `json:"query,omitempty"`
	Limit  int             `json:"limit,omitempty"`
	Cursor json.RawMessage `json:"cursor,omitempty"`
}

// EntriesResult is one page of entries.
type EntriesResult struct {
	Items  []Entry         `json:"items"`
	Cursor json.RawMessage `json:"cursor,omitempty"`
}

// UsageParams is conversation.usage's params.
type UsageParams struct {
	ID ID `json:"id"`
}

// Usage is what a conversation spent, by model and by tool, and how full its
// context is. Models, Tools and Context are Pi's shapes; opaque until the
// usage page reads them.
type Usage struct {
	Models  json.RawMessage `json:"models,omitempty"`
	Tools   json.RawMessage `json:"tools,omitempty"`
	Context json.RawMessage `json:"context,omitempty"`
}

// AbortTaskParams is task.abort's params.
type AbortTaskParams struct {
	TaskID ID `json:"taskId"`
}

// Status is the result of the methods that answer how something ended.
type Status struct {
	Status string `json:"status"`
}

// IndexUpdateParams is index.update's params: Droi's own metadata of a
// conversation. A nil field stays as it was.
type IndexUpdateParams struct {
	ID ID `json:"id"`
	// Title is the new title. Leave it nil to keep the stored one, use TitleText
	// for a name, or TitleNull to give the conversation back its automatic name
	// (the first line of its first message).
	Title    json.RawMessage `json:"title,omitempty"`
	Archived *bool           `json:"archived,omitempty"`
	Kind     *string         `json:"kind,omitempty"`
	// TitleSource is TitleSourceAuto or TitleSourceChosen, and only means anything
	// with Title: absent, a title a caller names is a choice. TitleSourceAuto says a
	// generator wrote it, so the kernel may replace it again.
	TitleSource string `json:"titleSource,omitempty"`
	// ViewedAt is the unread marker. Leave it nil to keep the stored time, use
	// ViewedAtNull to ask for the dot back, or ViewedAtTime for a time.
	ViewedAt json.RawMessage `json:"viewedAt,omitempty"`
	// OwnerID makes a conversation the owner of this one (OwnerOf), and OwnerNull
	// makes this a list row again; leave it nil to keep what is stored. An owner that
	// does not exist is CodeConversationNotFound, and one that would own itself, this
	// row, or make the two own each other is CodeInvalidParams.
	OwnerID json.RawMessage `json:"ownerId,omitempty"`
}

// TitleText is index.update's title.
func TitleText(title string) json.RawMessage {
	b, _ := json.Marshal(title)
	return b
}

// TitleNull is index.update's "forget the name": the automatic one comes back.
func TitleNull() json.RawMessage { return json.RawMessage("null") }

// OwnerOf is index.update's "owner is this conversation".
func OwnerOf(id ID) json.RawMessage { return json.RawMessage(id.String()) }

// OwnerNull is index.update's "this is a list row again".
func OwnerNull() json.RawMessage { return json.RawMessage("null") }

// The two sources a title can have.
const (
	// TitleSourceAuto is the kernel's own guess: the first line of the first user message.
	TitleSourceAuto = "auto"
	// TitleSourceChosen is a title a person set, which the kernel's guess must not overwrite.
	TitleSourceChosen = "chosen"
)

// ViewedAtTime is index.update's "a person has seen everything up to here".
func ViewedAtTime(ms int64) json.RawMessage { return json.RawMessage(strconv.FormatInt(ms, 10)) }

// ViewedAtNull is index.update's "this conversation is unread again".
func ViewedAtNull() json.RawMessage { return json.RawMessage("null") }

// Process is one background process of a conversation (design §3.4): the
// bash_background tool's droi.process entry.
type Process struct {
	ID        string `json:"id"`
	PID       int    `json:"pid,omitempty"`
	Command   string `json:"command,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"`
	LogPath   string `json:"logPath,omitempty"`
	// Status is "running" or "exited".
	Status   string `json:"status"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

// ProcessesParams is process.list's params.
type ProcessesParams struct {
	ID ID `json:"id"`
}

// ProcessesResult is a conversation's background processes.
type ProcessesResult struct {
	Processes []Process `json:"processes"`
}

// StopProcessParams is process.stop's params: it kills the process's whole
// group.
type StopProcessParams struct {
	ID        ID     `json:"id"`
	ProcessID string `json:"processId"`
}

// The four answers an approval takes (design §3.1, Q1).
const (
	// ApprovalProceedOnce allows this one call.
	ApprovalProceedOnce = "proceed_once"
	// ApprovalProceedAlwaysTool allows every later call of the tool.
	ApprovalProceedAlwaysTool = "proceed_always_tool"
	// ApprovalProceedAlwaysFile allows every later call of the tool on this
	// file.
	ApprovalProceedAlwaysFile = "proceed_always_file"
	// ApprovalCancel refuses the call.
	ApprovalCancel = "cancel"
)

// ApprovalScope names what an approval answer's scope covers.
type ApprovalScope struct {
	Tool string `json:"tool,omitempty"`
	Path string `json:"path,omitempty"`
}

// ApprovalRespondParams is approval.respond's params.
type ApprovalRespondParams struct {
	RequestID string         `json:"requestId"`
	Outcome   string         `json:"outcome"`
	Scope     *ApprovalScope `json:"scope,omitempty"`
}

// AskAnswer answers one question: the options the user chose, or the text they
// typed.
type AskAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected,omitempty"`
	Text       *string  `json:"text,omitempty"`
}

// AskRespondParams is ask.respond's params.
type AskRespondParams struct {
	RequestID string      `json:"requestId"`
	Answers   []AskAnswer `json:"answers,omitempty"`
	Cancelled *bool       `json:"cancelled,omitempty"`
}

// MCPServer is one MCP server as mcp.list, mcp.set and mcp.changed report it.
type MCPServer struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport,omitempty"`
	Status    string   `json:"status,omitempty"`
	Enabled   bool     `json:"enabled"`
	Tools     []string `json:"tools,omitempty"`
	Error     *string  `json:"error,omitempty"`
}

// MCPServers is the MCP server list.
type MCPServers struct {
	Servers []MCPServer `json:"servers"`
}

// MCPServerConfig is a server to add or change: a command for stdio, a URL
// for HTTP.
type MCPServerConfig struct {
	Transport string            `json:"transport,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// MCPSetParams is mcp.set's params: add or change one server, or remove it.
type MCPSetParams struct {
	Name    string           `json:"name"`
	Server  *MCPServerConfig `json:"server,omitempty"`
	Enabled *bool            `json:"enabled,omitempty"`
	Remove  bool             `json:"remove,omitempty"`
}

// MCPAuthorizeParams is mcp.authorize's and mcp.authorize.cancel's params.
type MCPAuthorizeParams struct {
	Name string `json:"name"`
}

// MCPAuthorizeResult is the page the user opens to authorize a server.
type MCPAuthorizeResult struct {
	AuthorizationURL string `json:"authorizationUrl"`
}

// Skill is one skill as skills.list, skills.setDisabled and skills.changed
// report it.
type Skill struct {
	Name    string `json:"name"`
	Scope   string `json:"scope,omitempty"`
	Path    string `json:"path,omitempty"`
	Enabled bool   `json:"enabled"`
}

// Skills is the skill list.
type Skills struct {
	Skills []Skill `json:"skills"`
}

// SkillsSetDisabledParams is skills.setDisabled's params.
type SkillsSetDisabledParams struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
}

// HarnessInfo is harness.info's result.
type HarnessInfo struct {
	Version   string   `json:"version"`
	Kernel    KernelID `json:"kernel"`
	Providers []struct {
		ID     string   `json:"id"`
		Name   string   `json:"name,omitempty"`
		Models []string `json:"models,omitempty"`
	} `json:"providers"`
	Models []ModelInfo `json:"models"`
	// Capabilities says which methods this build implements.
	Capabilities Capabilities    `json:"capabilities"`
	Extensions   []ExtensionInfo `json:"extensions,omitempty"`
	Limits       Limits          `json:"limits,omitempty"`
}

// Limits are Kernel's own bounds, which a client reads rather than assumes.
// Each is optional on the wire and has a stated default, so a build that does
// not publish one is read with the contract's value rather than with zero.
type Limits struct {
	// ViewPushesPerSecond is how often one connection hears one conversation
	// per second. Commits inside one interval are coalesced into one push
	// and the last state always arrives: it is a frequency cap, not a
	// throttle of the data.
	ViewPushesPerSecond int `json:"viewPushesPerSecond,omitempty"`
	// ViewWindowDefault is the window a view holds when conversation.open
	// names none.
	ViewWindowDefault *Window `json:"viewWindowDefault,omitempty"`
	// MaxEntriesPerPage caps conversation.entries. A larger limit is not an
	// error: the page comes back at the cap with a cursor, and a client that
	// ignores the cursor has seen only a prefix of the history.
	MaxEntriesPerPage int `json:"maxEntriesPerPage,omitempty"`
	// MaxConversationsPerPage caps conversation.list, the same way.
	MaxConversationsPerPage int `json:"maxConversationsPerPage,omitempty"`
	// MaxImportEntriesPerCall caps one conversation.import batch. A larger one is
	// refused rather than truncated: splitting is the caller's job.
	MaxImportEntriesPerCall int `json:"maxImportEntriesPerCall,omitempty"`
}

// The contract's own limits, for a build that does not publish one.
const (
	defaultViewPushesPerSecond = 5
	defaultMaxEntriesPerPage   = 200
	defaultMaxConversations    = 200
)

// PushRate is how often one connection hears one conversation per second.
func (l Limits) PushRate() int {
	if l.ViewPushesPerSecond > 0 {
		return l.ViewPushesPerSecond
	}
	return defaultViewPushesPerSecond
}

// EntriesPerPage is the page conversation.entries may answer with; ask again
// with the cursor for the rest.
func (l Limits) EntriesPerPage() int {
	if l.MaxEntriesPerPage > 0 {
		return l.MaxEntriesPerPage
	}
	return defaultMaxEntriesPerPage
}

// ConversationsPerPage is the page conversation.list may answer with.
func (l Limits) ConversationsPerPage() int {
	if l.MaxConversationsPerPage > 0 {
		return l.MaxConversationsPerPage
	}
	return defaultMaxConversations
}

// Window is how a view's window is sized when a request names none; the
// contract's is {min: 100, max: 200}.
func (l Limits) Window() Window {
	if l.ViewWindowDefault != nil {
		return *l.ViewWindowDefault
	}
	return Window{Min: 100, Max: defaultMaxEntriesPerPage}
}

// KernelID says what the sidecar is built from.
type KernelID struct {
	Name    string            `json:"name,omitempty"`
	Version string            `json:"version,omitempty"`
	Pi      map[string]string `json:"pi,omitempty"`
}

// ModelInfo is one model Kernel offers.
type ModelInfo struct {
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	ContextWindow int      `json:"contextWindow,omitempty"`
	MaxOutput     int      `json:"maxOutput,omitempty"`
	Input         []string `json:"input,omitempty"`
	Thinking      []string `json:"thinking,omitempty"`
}

// Capabilities says which methods this build implements.
type Capabilities struct {
	Conversations bool `json:"conversations"`
	Compaction    bool `json:"compaction"`
	Fork          bool `json:"fork"`
	Usage         bool `json:"usage"`
	Tasks         bool `json:"tasks"`
	Index         bool `json:"index"`
	// Import says conversation.import answers.
	Import    bool `json:"import"`
	Images    bool `json:"images"`
	Processes bool `json:"processes"`
	Approvals bool `json:"approvals"`
	Asks      bool `json:"asks"`
	MCP       bool `json:"mcp"`
	Skills    bool `json:"skills"`
	Codemode  bool `json:"codemode"`
	// Models says model.complete answers; false in M1.
	Models bool `json:"models"`
}

// ExtensionInfo is one extension this build ships.
type ExtensionInfo struct {
	Name     string   `json:"name"`
	Tools    []string `json:"tools,omitempty"`
	Sections []string `json:"sections,omitempty"`
}

// ModelCompleteParams is model.complete's params: one model call with no
// conversation, no tools and nothing stored.
type ModelCompleteParams struct {
	// Model is the model to ask, in agent.model's shape. A model the build
	// does not know is CodeModelNotFound, never a silent substitute.
	Model ModelRef `json:"model"`
	// System is the system text; "" leaves the kernel's own minimal one.
	System string `json:"system,omitempty"`
	// Input is a string, or a list of content blocks.
	Input any `json:"input"`
	// Schema, when given, is a JSON Schema the answer must satisfy.
	Schema json.RawMessage `json:"schema,omitempty"`
	// ThinkingLevel is "off", "low", "medium" or "high"; "" is the model's
	// default, which may think for minutes.
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
	// TimeoutMs bounds the whole call; 0 is 120000, and at most 600000.
	TimeoutMs int `json:"timeoutMs,omitempty"`
}

// ModelCompleteResult is the answer: the model that gave it, its text, the
// parsed answer when a schema was asked for, and what the call cost.
type ModelCompleteResult struct {
	Model ModelRef `json:"model"`
	Text  string   `json:"text"`
	// JSON is the answer parsed and checked against the schema; absent
	// without one.
	JSON  json.RawMessage    `json:"json,omitempty"`
	Usage ModelCompleteUsage `json:"usage"`
}

// ModelCompleteUsage is what one model.complete call cost.
type ModelCompleteUsage struct {
	Input       int      `json:"input"`
	Output      int      `json:"output"`
	TotalTokens int      `json:"totalTokens"`
	Cost        *float64 `json:"cost,omitempty"`
}

// ShutdownResult is kernel.shutdown's result: Kernel has begun closing.
type ShutdownResult struct {
	Status string `json:"status"`
}

// ShutdownClosing is the status kernel.shutdown answers.
const ShutdownClosing = "closing"
