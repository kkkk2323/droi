package kernel

import "context"

// The contract's methods, one Go call each, in its order. Every one answers at
// once: none of them waits on a task or a turn, which arrives as notifications.

// Harness is harness.info: what Kernel runs and offers.
func (c *Client) Harness(ctx context.Context) (HarnessInfo, error) {
	return call[HarnessInfo](c, ctx, MethodHarnessInfo, nil)
}

// Create makes a conversation, or answers the one an earlier call with the
// same RequestID made.
func (c *Client) Create(ctx context.Context, p CreateParams) (CreateResult, error) {
	return call[CreateResult](c, ctx, MethodConversationCreate, p)
}

// List is one page of Droi's conversation list.
func (c *Client) List(ctx context.Context, p ListParams) (ListResult, error) {
	return call[ListResult](c, ctx, MethodConversationList, p)
}

// Open subscribes this connection to a conversation and answers its first
// view; its changes then come as conversation.view notifications.
func (c *Client) Open(ctx context.Context, p OpenParams) (OpenResult, error) {
	res, err := call[OpenResult](c, ctx, MethodConversationOpen, p)
	if err != nil {
		return res, err
	}
	c.watch(p.ID)
	return res, nil
}

// Unwatch stops this connection's subscription; the conversation, and every
// other connection's subscription, stay. A failed unwatch keeps counting the
// conversation as watched, because Kernel still sends its views.
func (c *Client) Unwatch(ctx context.Context, id ID) error {
	err := c.Call(ctx, MethodConversationUnwatch, IDParams{ID: id}, nil)
	if err == nil {
		c.forget(id)
	}
	return err
}

// Submit sends a message, or asks for a run; it answers the submission, not
// the turn.
func (c *Client) Submit(ctx context.Context, p SubmitParams) (SubmitResult, error) {
	return call[SubmitResult](c, ctx, MethodConversationSubmit, p)
}

// Abort stops what the conversation runs, or with Background also the tasks
// that outlive the turn.
func (c *Client) Abort(ctx context.Context, p AbortParams) error {
	return c.Call(ctx, MethodConversationAbort, p, nil)
}

// AbortSubmission stops one submission of a conversation.
func (c *Client) AbortSubmission(ctx context.Context, p AbortSubmissionParams) (Status, error) {
	return call[Status](c, ctx, MethodSubmissionAbort, p)
}

// Import writes a transcript that already exists: without a conversation ID it creates the
// conversation, with one it appends a batch. A key makes a batch idempotent.
func (c *Client) Import(ctx context.Context, p ImportParams) (ImportResult, error) {
	return call[ImportResult](c, ctx, MethodConversationImport, p)
}

// SubmissionGet answers where one submission of a conversation got to: its
// status, the user entry it produced and the answer it got. A turn's entries
// name the task that wrote them rather than the submission, so this is the only
// exact boundary for "which entries did this submit produce".
func (c *Client) SubmissionGet(ctx context.Context, p AbortSubmissionParams) (SubmissionGet, error) {
	return call[SubmissionGet](c, ctx, MethodSubmissionGet, p)
}

// Configure changes the agent a conversation runs with and answers it
// resolved.
func (c *Client) Configure(ctx context.Context, p ConfigureParams) (ConfigureResult, error) {
	return call[ConfigureResult](c, ctx, MethodConversationConfig, p)
}

// Compact starts a compaction and answers its task id.
func (c *Client) Compact(ctx context.Context, p CompactParams) (CompactResult, error) {
	return call[CompactResult](c, ctx, MethodConversationCompact, p)
}

// Fork copies a conversation up to an entry into a new one.
func (c *Client) Fork(ctx context.Context, p ForkParams) (ForkResult, error) {
	return call[ForkResult](c, ctx, MethodConversationFork, p)
}

// Entries is one page of a conversation's entries.
func (c *Client) Entries(ctx context.Context, p EntriesParams) (EntriesResult, error) {
	return call[EntriesResult](c, ctx, MethodConversationEntries, p)
}

// Usage is what a conversation spent and how full its context is.
func (c *Client) Usage(ctx context.Context, id ID) (Usage, error) {
	return call[Usage](c, ctx, MethodConversationUsage, UsageParams{ID: id})
}

// AbortTask stops a task of the task graph.
func (c *Client) AbortTask(ctx context.Context, p AbortTaskParams) (Status, error) {
	return call[Status](c, ctx, MethodTaskAbort, p)
}

// UpdateIndex writes Droi's metadata of a conversation.
func (c *Client) UpdateIndex(ctx context.Context, p IndexUpdateParams) error {
	return c.Call(ctx, MethodIndexUpdate, p, nil)
}

// Processes is a conversation's background processes.
func (c *Client) Processes(ctx context.Context, id ID) (ProcessesResult, error) {
	return call[ProcessesResult](c, ctx, MethodProcessList, ProcessesParams{ID: id})
}

// StopProcess kills a background process's whole group.
func (c *Client) StopProcess(ctx context.Context, p StopProcessParams) (Status, error) {
	return call[Status](c, ctx, MethodProcessStop, p)
}

// RespondApproval answers one approval.requested. The first answer wins: a
// later one gets CodeRequestGone.
func (c *Client) RespondApproval(ctx context.Context, p ApprovalRespondParams) (Status, error) {
	return call[Status](c, ctx, MethodApprovalRespond, p)
}

// RespondAsk answers one ask.requested, or dismisses it.
func (c *Client) RespondAsk(ctx context.Context, p AskRespondParams) (Status, error) {
	return call[Status](c, ctx, MethodAskRespond, p)
}

// MCP is the MCP server list. A repository's own servers are included when
// MCPListParams names it; without one, every repository the kernel has seen.
func (c *Client) MCP(ctx context.Context, p MCPListParams) (MCPServers, error) {
	return call[MCPServers](c, ctx, MethodMCPList, p)
}

// SetMCP adds or changes one MCP server, or removes it, and answers the list.
func (c *Client) SetMCP(ctx context.Context, p MCPSetParams) (MCPServers, error) {
	return call[MCPServers](c, ctx, MethodMCPSet, p)
}

// AuthorizeMCP starts a server's OAuth flow and answers the page the user
// opens.
func (c *Client) AuthorizeMCP(ctx context.Context, p MCPAuthorizeParams) (MCPAuthorizeResult, error) {
	return call[MCPAuthorizeResult](c, ctx, MethodMCPAuthorize, p)
}

// CancelMCPAuthorize stops a server's OAuth flow.
func (c *Client) CancelMCPAuthorize(ctx context.Context, p MCPAuthorizeParams) error {
	return c.Call(ctx, MethodMCPAuthorizeCancel, p, nil)
}

// Skills is the skills Droi may offer.
func (c *Client) Skills(ctx context.Context) (Skills, error) {
	return call[Skills](c, ctx, MethodSkillsList, nil)
}

// SetSkillDisabled turns one skill off or on and answers the list.
func (c *Client) SetSkillDisabled(ctx context.Context, p SkillsSetDisabledParams) (Skills, error) {
	return call[Skills](c, ctx, MethodSkillsSetDisabled, p)
}

// ModelComplete asks a model once, with no conversation, no tools and nothing
// stored. It answers CodeUnsupported until harness.info says
// capabilities.models.
func (c *Client) ModelComplete(ctx context.Context, p ModelCompleteParams) (ModelCompleteResult, error) {
	return call[ModelCompleteResult](c, ctx, MethodModelComplete, p)
}

// PolicySet fills the kernel's snapshot of the approval policy. A missing ID is
// the default policy, a number is that conversation's own; the answer is the
// policy in effect. The policy lives in the Host: the kernel answers nothing
// from an empty snapshot, and broadcasts policy.changed after every change.
func (c *Client) PolicySet(ctx context.Context, p PolicySetParams) (PolicyResult, error) {
	return call[PolicyResult](c, ctx, MethodPolicySet, p)
}

// Resources asks what the kernel would load for a conversation: its skills, the
// slash commands it will expand, and the AGENTS.md chain it applies.
func (c *Client) Resources(ctx context.Context, p ResourcesListParams) (ResourcesResult, error) {
	return call[ResourcesResult](c, ctx, MethodResourcesList, p)
}

// Shutdown asks Kernel to close in order and end: it answers {status: "closing"}
// first, so this call returns while the close is still running. Wait on Done
// (or the process) for it to be over.
func (c *Client) Shutdown(ctx context.Context) (ShutdownResult, error) {
	return call[ShutdownResult](c, ctx, MethodKernelShutdown, nil)
}
