package app

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// The window's half of the Kernel: the connection the Host hands it, the conversations its pages have open, and
// the pump that takes Kernel's pushes to them. A Session whose id the Config resolves to a conversation is drawn
// from here; every other one is still the Daemon's (plan A of M2: the list stays on the Daemon until it moves).

// errNoKernel is what a page's call answers before the Host has handed over a connection, and after it ended.
var errNoKernel = errors.New("the kernel is not connected")

// kernelConn is the App's Kernel: every call goes to the client of the connection in use, which the Host hands
// over on every connect and reconnect. Pages hold this, never a client, so none of them calls a dead one.
type kernelConn struct {
	mu  sync.Mutex
	c   *kernel.Client
	gen int64
	// models are what harness.info offered on the connection in use, for the composer's picker.
	models []kernel.ModelInfo
}

// offered is the models harness.info offered; nil before it answered.
func (k *kernelConn) offered() []kernel.ModelInfo {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.models
}

func (k *kernelConn) use(c *kernel.Client, gen int64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.c, k.gen = c, gen
}

func (k *kernelConn) client() (*kernel.Client, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.c == nil {
		return nil, errNoKernel
	}
	return k.c, nil
}

var _ sessionview.Kernel = (*kernelConn)(nil)

func (k *kernelConn) Open(ctx context.Context, p kernel.OpenParams) (kernel.OpenResult, error) {
	c, err := k.client()
	if err != nil {
		return kernel.OpenResult{}, err
	}
	return c.Open(ctx, p)
}

func (k *kernelConn) Unwatch(ctx context.Context, id kernel.ID) error {
	c, err := k.client()
	if err != nil {
		return err
	}
	return c.Unwatch(ctx, id)
}

func (k *kernelConn) Submit(ctx context.Context, p kernel.SubmitParams) (kernel.SubmitResult, error) {
	c, err := k.client()
	if err != nil {
		return kernel.SubmitResult{}, err
	}
	return c.Submit(ctx, p)
}

func (k *kernelConn) Abort(ctx context.Context, p kernel.AbortParams) error {
	c, err := k.client()
	if err != nil {
		return err
	}
	return c.Abort(ctx, p)
}

func (k *kernelConn) AbortSubmission(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.Status, error) {
	c, err := k.client()
	if err != nil {
		return kernel.Status{}, err
	}
	return c.AbortSubmission(ctx, p)
}

func (k *kernelConn) SubmissionGet(ctx context.Context, p kernel.AbortSubmissionParams) (kernel.SubmissionGet, error) {
	c, err := k.client()
	if err != nil {
		return kernel.SubmissionGet{}, err
	}
	return c.SubmissionGet(ctx, p)
}

func (k *kernelConn) Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error) {
	c, err := k.client()
	if err != nil {
		return kernel.ConfigureResult{}, err
	}
	return c.Configure(ctx, p)
}

func (k *kernelConn) Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error) {
	c, err := k.client()
	if err != nil {
		return kernel.EntriesResult{}, err
	}
	return c.Entries(ctx, p)
}

func (k *kernelConn) Usage(ctx context.Context, id kernel.ID) (kernel.Usage, error) {
	c, err := k.client()
	if err != nil {
		return kernel.Usage{}, err
	}
	return c.Usage(ctx, id)
}

func (k *kernelConn) Processes(ctx context.Context, id kernel.ID) (kernel.ProcessesResult, error) {
	c, err := k.client()
	if err != nil {
		return kernel.ProcessesResult{}, err
	}
	return c.Processes(ctx, id)
}

func (k *kernelConn) StopProcess(ctx context.Context, p kernel.StopProcessParams) (kernel.Status, error) {
	c, err := k.client()
	if err != nil {
		return kernel.Status{}, err
	}
	return c.StopProcess(ctx, p)
}

func (k *kernelConn) RespondApproval(ctx context.Context, p kernel.ApprovalRespondParams) (kernel.Status, error) {
	c, err := k.client()
	if err != nil {
		return kernel.Status{}, err
	}
	return c.RespondApproval(ctx, p)
}

func (k *kernelConn) RespondAsk(ctx context.Context, p kernel.AskRespondParams) (kernel.Status, error) {
	c, err := k.client()
	if err != nil {
		return kernel.Status{}, err
	}
	return c.RespondAsk(ctx, p)
}

func (k *kernelConn) UpdateIndex(ctx context.Context, p kernel.IndexUpdateParams) error {
	c, err := k.client()
	if err != nil {
		return err
	}
	return c.UpdateIndex(ctx, p)
}

func (k *kernelConn) ModelComplete(ctx context.Context, p kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
	c, err := k.client()
	if err != nil {
		return kernel.ModelCompleteResult{}, err
	}
	return c.ModelComplete(ctx, p)
}

// titleSource is titles.Config.TitleSource: where the title of a conversation the list holds came from. Only a
// conversation a person started in a chat or a Scratch folder is named; Automations and Workstreams name their own.
func (a *App) titleSource(_ context.Context, conv kernel.ID) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := findSummary(a.listed, a.sessionIDOf(conv))
	if s == nil || (s.Kind != "chat" && s.Kind != "scratch") {
		return "", false
	}
	return s.TitleSource, true
}

// UseKernel hands the App the Kernel connection of generation gen: the Host calls it on every connect and
// reconnect, from any goroutine. The pages' calls go to it from now on, the conversations they have open are
// opened again on it, and its pushes reach them. A generation older than the one in use is ignored.
func (a *App) UseKernel(c *kernel.Client, gen int64) {
	a.kernel.mu.Lock()
	if gen <= a.kernel.gen && a.kernel.c != nil {
		a.kernel.mu.Unlock()
		return
	}
	a.kernel.c, a.kernel.gen = c, gen
	a.kernel.mu.Unlock()
	// The approvals asked on the connection before went with it: a new Kernel asks again for a call it still makes.
	waiting := string(protocol.DroidWorkingStateWaitingForToolConfirmation)
	a.mu.Lock()
	for id := range a.approvals {
		if a.busy[id] == waiting {
			a.busy[id] = string(protocol.DroidWorkingStateStreamingAssistantMessage)
		}
	}
	a.approvals = nil
	a.mu.Unlock()
	go a.pushPolicy(c, gen)
	go a.pumpKernel(c, gen)
	go func() { _ = a.sessions.Reopen(a.ctx, &a.kernel, gen) }()
	go a.readModels(c, gen)
	// The list is Kernel's from now on, and on a new connection it may have moved while the window was away.
	go a.reloadList()
}

// readModels keeps the models harness.info offers on connection gen, for the picker. A failure leaves the list as
// it was: the picker then offers what the last connection did, and a send still names the model the conversation
// has.
func (a *App) readModels(c *kernel.Client, gen int64) {
	info, err := c.Harness(a.ctx)
	if err != nil {
		return
	}
	a.kernel.mu.Lock()
	if a.kernel.gen == gen {
		a.kernel.models = info.Models
	}
	a.kernel.mu.Unlock()
	a.redraw()
}

// pumpKernel takes the pushes of one connection to the conversations open on it, until the connection ends.
// It is the connection's only reader.
func (a *App) pumpKernel(c *kernel.Client, gen int64) {
	for {
		select {
		case <-a.ctx.Done():
			return
		case n, ok := <-c.Notifications():
			if !ok {
				return
			}
			switch n.Method {
			case kernel.NotifyConversationView:
				var view kernel.ViewNotification
				if n.Decode(&view) == nil {
					a.sessions.ApplyView(gen, view)
					if s := a.sessions.State(view.ID); s != nil && s.Busy() {
						a.joinedRun(view.ID)
					}
				}
			case kernel.NotifyConversationBusy:
				var busy kernel.BusyNotification
				if n.Decode(&busy) == nil {
					a.sessions.ApplyBusy(gen, busy)
					a.alertBusy(busy)
					a.titles.Busy(a.ctx, busy)
				}
			case kernel.NotifyIndexChanged:
				var changed kernel.IndexChanged
				if n.Decode(&changed) == nil {
					a.applyIndexChanged(changed)
				}
			case kernel.NotifyApprovalRequested:
				var req kernel.ApprovalRequested
				if n.Decode(&req) == nil {
					a.sessions.ApplyApproval(gen, req)
					a.awaitApproval(req.ID, req.RequestID, true)
				}
			case kernel.NotifyApprovalResolved:
				var res kernel.ApprovalResolved
				if n.Decode(&res) == nil {
					a.sessions.ApplyResolved(gen, res)
					a.awaitApproval(res.ID, res.RequestID, false)
				}
			case kernel.NotifyAskRequested:
				var req kernel.AskRequested
				if n.Decode(&req) == nil {
					a.sessions.ApplyAsk(gen, req)
					// A question the agent works on past does not hold the Session.
					if req.Blocking {
						a.awaitApproval(req.ID, req.RequestID, true)
					}
				}
			case kernel.NotifyAskResolved:
				var res kernel.AskResolved
				if n.Decode(&res) == nil {
					a.sessions.ApplyAskResolved(gen, res)
					a.awaitApproval(res.ID, res.RequestID, false)
				}
			case kernel.NotifyPolicyChanged:
				var changed kernel.PolicyChanged
				if n.Decode(&changed) == nil {
					a.policyChanged(changed)
				}
			}
		}
	}
}

// alertBusy raises the alert a conversation's run calls for when it ends, as the Daemon's working states do: Kernel
// says busy and idle, and awaitApproval marks the run that waits on a card.
func (a *App) alertBusy(n kernel.BusyNotification) {
	id := a.sessionIDOf(n.ID)
	state := string(protocol.DroidWorkingStateIdle)
	a.mu.Lock()
	switch {
	case !n.Busy():
		delete(a.approvals, id)
	case len(a.approvals[id]) > 0:
		// The approval can be heard before the run it belongs to: the two come on queues of their own.
		state = string(protocol.DroidWorkingStateWaitingForToolConfirmation)
	default:
		state = string(protocol.DroidWorkingStateStreamingAssistantMessage)
	}
	a.busy[id] = state
	// A run of a conversation the list does not have yet: one this window or another just made, or an
	// Automation's. Read the list for it once; one Kernel leaves out of the list (a subagent's) stays out.
	read := n.Busy() && a.listDone && findSummary(a.listed, id) == nil && !a.asked[id]
	if read {
		if a.asked == nil {
			a.asked = map[string]bool{}
		}
		a.asked[id] = true
	}
	a.mu.Unlock()
	if read {
		go a.refreshList()
	}
	a.redraw()
	if ev := a.kernelAlerts.Update(id, state); ev != "" {
		a.cfg.Update(func() { a.raise(id, "", ev) })
	}
}

// joinedRun takes a run this connection found under way, in a view, as one it heard start: Kernel says busy when a
// run starts, so a window that connects or opens the conversation later would never hear it working, and its end
// would raise no alert. Only a conversation not heard of yet is taken, so a view older than the idle that followed
// it cannot mark a finished run working again.
func (a *App) joinedRun(conv kernel.ID) {
	id := a.sessionIDOf(conv)
	state := string(protocol.DroidWorkingStateStreamingAssistantMessage)
	a.mu.Lock()
	_, heard := a.busy[id]
	if !heard {
		a.busy[id] = state
	}
	a.mu.Unlock()
	if !heard {
		a.kernelAlerts.Update(id, state)
		a.redraw()
	}
}

// kernelID is the conversation that holds Session id, when Kernel holds it: the one Config.KernelID names, else,
// once the window has a Kernel, the conversation the id is written as.
func (a *App) kernelID(id string) (kernel.ID, bool) {
	if a.cfg.KernelID != nil {
		if conv, ok := a.cfg.KernelID(id); ok {
			return conv, true
		}
	}
	if _, err := a.kernel.client(); err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, false
	}
	return kernel.ID(n), true
}

// sessionIDOf is the window's id for conversation conv: the Session Config.KernelSession names, else the
// conversation's id as the contract writes it.
func (a *App) sessionIDOf(conv kernel.ID) string {
	if a.cfg.KernelSession != nil {
		if id, ok := a.cfg.KernelSession(conv); ok {
			return id
		}
	}
	return conv.String()
}
