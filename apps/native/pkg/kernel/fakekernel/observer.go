package fakekernel

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// Observe wraps a connection so a test can assert what the window sent: the method and the params, in
// order, for every call of the protocol.
//
// It exists because the two halves of a view test need different seams. The transcript says what the
// Kernel answered; the Observer says what the window asked for. The Fake Daemon recorded the requests it
// served, and a test that asserts a Gateway did not start a Session, or that a prompt addition went with
// exactly one create, needs the same evidence here.
//
// The wrappers are one method each, not reflection, so a method the client has and the Observer does not is
// a compile error: clientAPI below names every call of the protocol, and both *kernel.Client and *Observer
// are asserted against it. The one direction a compile-time assertion cannot see — a method added to the
// contract and to the client but never to the Observer's list — is what TestObserverForwardsEveryMethodOfTheContract
// checks, against kernel.Methods.
type Observer struct {
	client *kernel.Client

	mu    sync.Mutex
	calls []RPC
}

// RPC is one observed call: the method, its params as the wire carries them, and when it was sent.
type RPC struct {
	Method string
	// Params is the request's params JSON, absent for a method that takes none. It is the caller's own
	// serialization, so a test asserting on it asserts on the wire.
	Params json.RawMessage
	At     time.Time
}

// clientAPI is the protocol's calls, as pkg/kernel spells them.
//
// It is written out rather than derived (Go cannot reflect a method set at compile time), and it earns its
// keep at the two assertions below: a client method the Observer does not forward stops the package from
// building. Its other direction — a method in the contract that neither type has — is the runtime check in
// the package's own test.
type clientAPI interface {
	Harness(ctx context.Context) (kernel.HarnessInfo, error)
	Create(ctx context.Context, p kernel.CreateParams) (kernel.CreateResult, error)
	List(ctx context.Context, p kernel.ListParams) (kernel.ListResult, error)
	Open(ctx context.Context, p kernel.OpenParams) (kernel.OpenResult, error)
	Unwatch(ctx context.Context, id kernel.ID) error
	Submit(ctx context.Context, p kernel.SubmitParams) (kernel.SubmitResult, error)
	Abort(ctx context.Context, p kernel.AbortParams) error
	AbortSubmission(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.Status, error)
	SubmissionGet(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.SubmissionGet, error)
	Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error)
	Compact(ctx context.Context, p kernel.CompactParams) (kernel.CompactResult, error)
	Fork(ctx context.Context, p kernel.ForkParams) (kernel.ForkResult, error)
	Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error)
	Usage(ctx context.Context, id kernel.ID) (kernel.Usage, error)
	AbortTask(ctx context.Context, p kernel.AbortTaskParams) (kernel.Status, error)
	UpdateIndex(ctx context.Context, p kernel.IndexUpdateParams) error
	Processes(ctx context.Context, id kernel.ID) (kernel.ProcessesResult, error)
	StopProcess(ctx context.Context, p kernel.StopProcessParams) (kernel.Status, error)
	RespondApproval(ctx context.Context, p kernel.ApprovalRespondParams) (kernel.Status, error)
	RespondAsk(ctx context.Context, p kernel.AskRespondParams) (kernel.Status, error)
	MCP(ctx context.Context) (kernel.MCPServers, error)
	SetMCP(ctx context.Context, p kernel.MCPSetParams) (kernel.MCPServers, error)
	AuthorizeMCP(ctx context.Context, p kernel.MCPAuthorizeParams) (kernel.MCPAuthorizeResult, error)
	CancelMCPAuthorize(ctx context.Context, p kernel.MCPAuthorizeParams) error
	Skills(ctx context.Context) (kernel.Skills, error)
	SetSkillDisabled(ctx context.Context, p kernel.SkillsSetDisabledParams) (kernel.Skills, error)
	ModelComplete(ctx context.Context, p kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error)
	Shutdown(ctx context.Context) (kernel.ShutdownResult, error)
}

var (
	_ clientAPI = (*kernel.Client)(nil)
	_ clientAPI = (*Observer)(nil)
)

// Observe records everything the connection c sends. The Observer and c reach the same Kernel; use the
// Observer for every call a test wants to see, and c for the ones it does not care about.
func (k *Kernel) Observe(c *kernel.Client) *Observer { return &Observer{client: c} }

// Client is the connection under the Observer, for the few things a test does not want recorded.
func (o *Observer) Client() *kernel.Client { return o.client }

// Calls is everything observed so far, in order.
func (o *Observer) Calls() []RPC {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]RPC(nil), o.calls...)
}

// CallsOf is the calls of one method, in order.
func (o *Observer) CallsOf(method string) []RPC {
	var out []RPC
	for _, call := range o.Calls() {
		if call.Method == method {
			out = append(out, call)
		}
	}
	return out
}

// Count is how many calls of one method were observed.
func (o *Observer) Count(method string) int { return len(o.CallsOf(method)) }

// WaitForCall waits for the nth call of method (1 is the first) and fails the test after within. The
// failure names the method and lists what was observed instead, so a missing call does not send the
// reader to a print statement.
//
// The wait is bounded by the test's own deadline as well as by within, the way the Kernel's other waits are:
// a test near its timeout must hear that the call never came and still run its cleanups, rather than be cut
// off by the runner's hard timeout in the middle of the wait.
func (o *Observer) WaitForCall(t testing.TB, method string, nth int, within time.Duration) RPC {
	t.Helper()
	ctx, cancel := o.budget(t, within)
	defer cancel()
	for {
		if calls := o.CallsOf(method); len(calls) >= nth {
			return calls[nth-1]
		}
		select {
		case <-ctx.Done():
			seen := o.Calls()
			names := make([]string, len(seen))
			for i, call := range seen {
				names[i] = call.Method
			}
			t.Fatalf("no call %d of %s within %s; %d calls were made: %s",
				nth, method, within, len(seen), strings.Join(names, ", "))
			return RPC{}
		case <-time.After(time.Millisecond):
		}
	}
}

// budget is how long a wait may last: within, or less when the test's own deadline is nearer.
//
// It follows the rule the Fake Kernel's other waits already use (Kernel.context): leave a second to report,
// and when that margin is already gone hand back an expired context, so the wait gives up at once and the
// test's cleanups still run.
func (o *Observer) budget(t testing.TB, within time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	limit := within
	// testing.TB has no Deadline; testing.T does, and it is what a test runner passes.
	if withDeadline, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		if deadline, ok := withDeadline.Deadline(); ok {
			remaining := time.Until(deadline) - time.Second
			if remaining <= 0 {
				expired, cancel := context.WithCancel(context.Background())
				cancel()
				return expired, func() {}
			}
			limit = min(limit, remaining)
		}
	}
	return context.WithTimeout(context.Background(), limit)
}

// The protocol's methods, in the contract's order, each recorded then forwarded.

// Harness is harness.info.
func (o *Observer) Harness(ctx context.Context) (kernel.HarnessInfo, error) {
	o.record(kernel.MethodHarnessInfo, nil)
	return o.client.Harness(ctx)
}

// Create is conversation.create.
func (o *Observer) Create(ctx context.Context, p kernel.CreateParams) (kernel.CreateResult, error) {
	o.record(kernel.MethodConversationCreate, p)
	return o.client.Create(ctx, p)
}

// List is conversation.list.
func (o *Observer) List(ctx context.Context, p kernel.ListParams) (kernel.ListResult, error) {
	o.record(kernel.MethodConversationList, p)
	return o.client.List(ctx, p)
}

// Open is conversation.open.
func (o *Observer) Open(ctx context.Context, p kernel.OpenParams) (kernel.OpenResult, error) {
	o.record(kernel.MethodConversationOpen, p)
	return o.client.Open(ctx, p)
}

// Unwatch is conversation.unwatch.
func (o *Observer) Unwatch(ctx context.Context, id kernel.ID) error {
	o.record(kernel.MethodConversationUnwatch, kernel.IDParams{ID: id})
	return o.client.Unwatch(ctx, id)
}

// Submit is conversation.submit.
func (o *Observer) Submit(ctx context.Context, p kernel.SubmitParams) (kernel.SubmitResult, error) {
	o.record(kernel.MethodConversationSubmit, p)
	return o.client.Submit(ctx, p)
}

// Abort is conversation.abort.
func (o *Observer) Abort(ctx context.Context, p kernel.AbortParams) error {
	o.record(kernel.MethodConversationAbort, p)
	return o.client.Abort(ctx, p)
}

// AbortSubmission is submission.abort.
func (o *Observer) AbortSubmission(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.Status, error) {
	o.record(kernel.MethodSubmissionAbort, p)
	return o.client.AbortSubmission(ctx, p)
}

// SubmissionGet is submission.get.
func (o *Observer) SubmissionGet(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.SubmissionGet, error) {
	o.record(kernel.MethodSubmissionGet, p)
	return o.client.SubmissionGet(ctx, p)
}

// Configure is conversation.configure.
func (o *Observer) Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error) {
	o.record(kernel.MethodConversationConfig, p)
	return o.client.Configure(ctx, p)
}

// Compact is conversation.compact.
func (o *Observer) Compact(ctx context.Context, p kernel.CompactParams) (kernel.CompactResult, error) {
	o.record(kernel.MethodConversationCompact, p)
	return o.client.Compact(ctx, p)
}

// Fork is conversation.fork.
func (o *Observer) Fork(ctx context.Context, p kernel.ForkParams) (kernel.ForkResult, error) {
	o.record(kernel.MethodConversationFork, p)
	return o.client.Fork(ctx, p)
}

// Import is conversation.import.
func (o *Observer) Import(ctx context.Context, p kernel.ImportParams) (kernel.ImportResult, error) {
	o.record(kernel.MethodConversationImport, p)
	return o.client.Import(ctx, p)
}

// Entries is conversation.entries.
func (o *Observer) Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error) {
	o.record(kernel.MethodConversationEntries, p)
	return o.client.Entries(ctx, p)
}

// Usage is conversation.usage.
func (o *Observer) Usage(ctx context.Context, id kernel.ID) (kernel.Usage, error) {
	o.record(kernel.MethodConversationUsage, kernel.UsageParams{ID: id})
	return o.client.Usage(ctx, id)
}

// AbortTask is task.abort.
func (o *Observer) AbortTask(ctx context.Context, p kernel.AbortTaskParams) (kernel.Status, error) {
	o.record(kernel.MethodTaskAbort, p)
	return o.client.AbortTask(ctx, p)
}

// UpdateIndex is index.update.
func (o *Observer) UpdateIndex(ctx context.Context, p kernel.IndexUpdateParams) error {
	o.record(kernel.MethodIndexUpdate, p)
	return o.client.UpdateIndex(ctx, p)
}

// Processes is process.list.
func (o *Observer) Processes(ctx context.Context, id kernel.ID) (kernel.ProcessesResult, error) {
	o.record(kernel.MethodProcessList, kernel.ProcessesParams{ID: id})
	return o.client.Processes(ctx, id)
}

// StopProcess is process.stop.
func (o *Observer) StopProcess(ctx context.Context, p kernel.StopProcessParams) (kernel.Status, error) {
	o.record(kernel.MethodProcessStop, p)
	return o.client.StopProcess(ctx, p)
}

// RespondApproval is approval.respond.
func (o *Observer) RespondApproval(ctx context.Context, p kernel.ApprovalRespondParams) (kernel.Status, error) {
	o.record(kernel.MethodApprovalRespond, p)
	return o.client.RespondApproval(ctx, p)
}

// RespondAsk is ask.respond.
func (o *Observer) RespondAsk(ctx context.Context, p kernel.AskRespondParams) (kernel.Status, error) {
	o.record(kernel.MethodAskRespond, p)
	return o.client.RespondAsk(ctx, p)
}

// MCP is mcp.list.
func (o *Observer) MCP(ctx context.Context) (kernel.MCPServers, error) {
	o.record(kernel.MethodMCPList, nil)
	return o.client.MCP(ctx)
}

// SetMCP is mcp.set.
func (o *Observer) SetMCP(ctx context.Context, p kernel.MCPSetParams) (kernel.MCPServers, error) {
	o.record(kernel.MethodMCPSet, p)
	return o.client.SetMCP(ctx, p)
}

// AuthorizeMCP is mcp.authorize.
func (o *Observer) AuthorizeMCP(ctx context.Context, p kernel.MCPAuthorizeParams) (kernel.MCPAuthorizeResult, error) {
	o.record(kernel.MethodMCPAuthorize, p)
	return o.client.AuthorizeMCP(ctx, p)
}

// CancelMCPAuthorize is mcp.authorize.cancel.
func (o *Observer) CancelMCPAuthorize(ctx context.Context, p kernel.MCPAuthorizeParams) error {
	o.record(kernel.MethodMCPAuthorizeCancel, p)
	return o.client.CancelMCPAuthorize(ctx, p)
}

// Skills is skills.list.
func (o *Observer) Skills(ctx context.Context) (kernel.Skills, error) {
	o.record(kernel.MethodSkillsList, nil)
	return o.client.Skills(ctx)
}

// SetSkillDisabled is skills.setDisabled.
func (o *Observer) SetSkillDisabled(ctx context.Context, p kernel.SkillsSetDisabledParams) (kernel.Skills, error) {
	o.record(kernel.MethodSkillsSetDisabled, p)
	return o.client.SetSkillDisabled(ctx, p)
}

// ModelComplete is model.complete.
func (o *Observer) ModelComplete(ctx context.Context, p kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
	o.record(kernel.MethodModelComplete, p)
	return o.client.ModelComplete(ctx, p)
}

// Shutdown is kernel.shutdown.
func (o *Observer) Shutdown(ctx context.Context) (kernel.ShutdownResult, error) {
	o.record(kernel.MethodKernelShutdown, nil)
	return o.client.Shutdown(ctx)
}

// record keeps one call. It runs before the call is made: the Observer's question is what the window
// asked for, and a call that the Kernel refused was still sent.
func (o *Observer) record(method string, params any) {
	var raw json.RawMessage
	if params != nil {
		if b, err := json.Marshal(params); err == nil {
			raw = b
		}
	}
	o.mu.Lock()
	o.calls = append(o.calls, RPC{Method: method, Params: raw, At: time.Now()})
	o.mu.Unlock()
}

// methods is every method the Observer forwards, for the package's own test to compare with the contract.
func (o *Observer) methods() []string {
	return []string{
		kernel.MethodHarnessInfo, kernel.MethodConversationCreate, kernel.MethodConversationList,
		kernel.MethodConversationOpen, kernel.MethodConversationUnwatch, kernel.MethodConversationSubmit,
		kernel.MethodConversationAbort, kernel.MethodSubmissionAbort, kernel.MethodSubmissionGet,
		kernel.MethodConversationConfig, kernel.MethodConversationCompact, kernel.MethodConversationFork,
		kernel.MethodConversationImport,
		kernel.MethodConversationEntries, kernel.MethodConversationUsage, kernel.MethodTaskAbort,
		kernel.MethodIndexUpdate, kernel.MethodProcessList, kernel.MethodProcessStop,
		kernel.MethodApprovalRespond, kernel.MethodAskRespond, kernel.MethodMCPList, kernel.MethodMCPSet,
		kernel.MethodMCPAuthorize, kernel.MethodMCPAuthorizeCancel, kernel.MethodSkillsList,
		kernel.MethodSkillsSetDisabled, kernel.MethodModelComplete, kernel.MethodKernelShutdown,
	}
}
