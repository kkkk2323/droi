// Package kernel is Droi's client of the Kernel sidecar: the JSON-RPC 2.0
// methods and notifications of apps/kernel/protocol over one WebSocket, the
// handshake the sidecar prints when it starts, and its health check.
//
// The contract is apps/kernel/protocol/README.md and its fixtures: this
// package's tests decode the same JSON files Kernel's tests parse, so a field
// that moves fails here rather than in the window.
package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
)

// ID is one of the contract's identifiers: a conversation, an entry, a task, a
// submission or a document. It is a JSON number (Pi's int64), exact below
// 2^53.
type ID int64

// String is the ID as text, for logs and map keys.
func (id ID) String() string { return strconv.FormatInt(int64(id), 10) }

// The protocol's 27 methods.
const (
	MethodHarnessInfo         = "harness.info"
	MethodConversationCreate  = "conversation.create"
	MethodConversationList    = "conversation.list"
	MethodConversationOpen    = "conversation.open"
	MethodConversationUnwatch = "conversation.unwatch"
	MethodConversationSubmit  = "conversation.submit"
	MethodConversationAbort   = "conversation.abort"
	MethodSubmissionAbort     = "submission.abort"
	MethodConversationConfig  = "conversation.configure"
	MethodConversationCompact = "conversation.compact"
	MethodConversationFork    = "conversation.fork"
	MethodConversationEntries = "conversation.entries"
	MethodConversationUsage   = "conversation.usage"
	MethodTaskAbort           = "task.abort"
	MethodIndexUpdate         = "index.update"
	MethodProcessList         = "process.list"
	MethodProcessStop         = "process.stop"
	MethodApprovalRespond     = "approval.respond"
	MethodAskRespond          = "ask.respond"
	MethodMCPList             = "mcp.list"
	MethodMCPSet              = "mcp.set"
	MethodMCPAuthorize        = "mcp.authorize"
	MethodMCPAuthorizeCancel  = "mcp.authorize.cancel"
	MethodSkillsList          = "skills.list"
	MethodSkillsSetDisabled   = "skills.setDisabled"
	// MethodModelComplete is one model call with no conversation and no
	// tools, for the work a Host does with a model of its own: Memory's
	// summaries (ADR 0042). M1 answers -32009 until capabilities.models is
	// true.
	MethodModelComplete = "model.complete"
	// MethodKernelShutdown asks Kernel to close in order: it answers
	// {"status":"closing"}, then refuses new work, closes the harness and the
	// storage, and exits 0. It is the only way to stop a Kernel in order on
	// Windows, which has no SIGTERM a parent can send.
	MethodKernelShutdown = "kernel.shutdown"
)

// Methods is every method, in the contract's order.
var Methods = []string{
	MethodHarnessInfo, MethodConversationCreate, MethodConversationList,
	MethodConversationOpen, MethodConversationUnwatch, MethodConversationSubmit,
	MethodConversationAbort, MethodSubmissionAbort, MethodConversationConfig,
	MethodConversationCompact, MethodConversationFork, MethodConversationEntries,
	MethodConversationUsage, MethodTaskAbort, MethodIndexUpdate, MethodProcessList,
	MethodProcessStop, MethodApprovalRespond, MethodAskRespond, MethodMCPList,
	MethodMCPSet, MethodMCPAuthorize, MethodMCPAuthorizeCancel, MethodSkillsList,
	MethodSkillsSetDisabled, MethodModelComplete, MethodKernelShutdown,
}

// The protocol's notifications.
const (
	NotifyConversationView  = "conversation.view"
	NotifyConversationBusy  = "conversation.busy"
	NotifyApprovalRequested = "approval.requested"
	NotifyApprovalResolved  = "approval.resolved"
	NotifyAskRequested      = "ask.requested"
	NotifyAskResolved       = "ask.resolved"
	NotifyTaskChanged       = "task.changed"
	NotifyMCPChanged        = "mcp.changed"
	NotifyProcessChanged    = "process.changed"
	NotifyIndexChanged      = "index.changed"
	NotifyWarning           = "warning"
)

// The error codes of the contract's table. Clients branch on Code, never on
// Message.
const (
	CodeParseError        = -32700
	CodeInvalidRequest    = -32600
	CodeMethodNotFound    = -32601
	CodeInvalidParams     = -32602
	CodeInternal          = -32603
	CodeConversationGone  = -32000
	CodeConversationBusy  = -32001
	CodeConversationIdle  = -32002
	CodeSubmissionGone    = -32003
	CodeSubmissionSettled = -32004
	CodeSubmissionPlaced  = -32005
	CodeTaskGone          = -32006
	CodeRequestGone       = -32007
	CodeReadOnly          = -32008
	CodeUnsupported       = -32009
	CodeKernelFailed      = -32010
	CodeProcessGone       = -32011
	CodeTimeout           = -32012
	CodeConflict          = -32013
	CodeModelInvalid      = -32014
	CodeModelFailed       = -32015
	CodeModelNotFound     = -32016
	CodeModelOverBudget   = -32017
	CodeModelRefused      = -32018
)

// The capabilities harness.info reports; a false one means the method answers
// CodeUnsupported.
const (
	CapabilityConversations = "conversations"
	CapabilityCompaction    = "compaction"
	CapabilityFork          = "fork"
	CapabilityUsage         = "usage"
	CapabilityTasks         = "tasks"
	CapabilityIndex         = "index"
	CapabilityImages        = "images"
	CapabilityProcesses     = "processes"
	CapabilityApprovals     = "approvals"
	CapabilityAsks          = "asks"
	CapabilityMCP           = "mcp"
	CapabilitySkills        = "skills"
	CapabilityCodemode      = "codemode"
	CapabilityModels        = "models"
)

// Handshake is the first line Kernel prints on stdout: where it listens, and
// what it is. The token is not here: the Host gives it to Kernel in
// DROI_KERNEL_TOKEN and to the Client in Options.Token.
type Handshake struct {
	Port    int    `json:"port"`
	PID     int    `json:"pid,omitempty"`
	Version string `json:"version,omitempty"`
}

// TokenEnv is the environment variable the Host gives Kernel its token in.
// Kernel writes a line to stderr and exits with code 2 when it is missing.
const TokenEnv = "DROI_KERNEL_TOKEN"

// NewToken is the token of one launch: 32 hex characters of crypto
// randomness. The Host makes a fresh one per launch and gives it to Kernel in
// TokenEnv and to the Client in Options.Token; it never goes in argv, where
// another process could read it.
func NewToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// The statuses Kernel exits with, as the contract lists them. A Supervisor
// acts on the last two differently from a crash: a refusal may be retried or
// reported, and a storage failure is a restart with the same data directory.
const (
	// ExitOK is a kernel that was asked to stop and closed cleanly.
	ExitOK = 0
	// ExitFailed is a kernel that could not start or could not close: a bug, or
	// an environment problem.
	ExitFailed = 1
	// ExitRefused is a kernel that refused to start — bad arguments, no token,
	// or a data directory another kernel already holds — which is not a crash.
	ExitRefused = 2
	// ExitStorage is a kernel whose storage failed; it is restarted, and its
	// data directory reused.
	ExitStorage = 3
)

// ExitNoToken is how Kernel exits when the token is missing: one refusal among
// the ones ExitRefused covers.
const ExitNoToken = ExitRefused

// Health is what GET /health answers.
type Health struct {
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Pi      string `json:"pi,omitempty"`
}

// URL is Kernel's WebSocket address on the loopback interface.
func URL(port int) string { return "ws://127.0.0.1:" + strconv.Itoa(port) + "/rpc" }

// HealthURL is Kernel's health endpoint.
func HealthURL(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) + "/health" }

// The entry kinds: Pi's six, then the five Droi adds.
const (
	EntryUser       = "pi.user"
	EntryAssistant  = "pi.assistant"
	EntrySystem     = "pi.system"
	EntryToolResult = "pi.tool-result"
	EntryReset      = "pi.reset"
	EntryCompaction = "pi.compaction"

	EntryApproval = "droi.approval"
	EntryAsk      = "droi.ask"
	EntrySubagent = "droi.subagent"
	EntryProcess  = "droi.process"
	EntryNote     = "droi.note"
)

// Entry is one entry of a transcript, as conversation.open, conversation.entries
// and conversation.view send it. Model, Data and Edits stay JSON here: Pi's
// entry shape is the window's to decode (pkg/transcript, M2).
type Entry struct {
	ID             ID              `json:"id"`
	ConversationID ID              `json:"conversationId"`
	Kind           string          `json:"kind"`
	Model          json.RawMessage `json:"model,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	Head           *bool           `json:"head,omitempty"`
	Edits          json.RawMessage `json:"edits,omitempty"`
	ByTaskID       *ID             `json:"byTaskId,omitempty"`
}

// Conversation is what a view says about the conversation itself.
type Conversation struct {
	ID ID `json:"id"`
	// Parent is set on a forked conversation: {conversationId, at}.
	Parent json.RawMessage `json:"parent,omitempty"`
}

// View is Pi's ConversationView: the conversation, a window of its entries,
// and the built-in documents by kind. conversation.view sends the same value,
// so a client parses one shape.
type View struct {
	Conversation Conversation               `json:"conversation"`
	Entries      []Entry                    `json:"entries"`
	Before       json.RawMessage            `json:"before,omitempty"`
	Docs         map[string]json.RawMessage `json:"docs,omitempty"`
}

// Doc is one built-in document of a view, by kind (pi.agent, pi.live,
// pi.usage, pi.inbox, ...).
func (v View) Doc(kind string) json.RawMessage { return v.Docs[kind] }
