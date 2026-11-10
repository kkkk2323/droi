package app

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// The Host tells of no change to its Workstream records, such as an agent's
// report, and the Daemon of no change to the prompts it counts: the window
// reads them again now and then, and whenever one of the Workstream Sessions
// it lists starts, stops, waits or is archived.
const workstreamsRefresh = 30 * time.Second

// restedRetry is how long after an agent rested its prompts are read again,
// and the gap to the read after that.
const restedRetry = time.Second

// workspaceCheckDelay is how long typing pauses before the form asks the
// Daemon about the folder typed.
const workspaceCheckDelay = 400 * time.Millisecond

// How a Workstream agent waits on the user.
const (
	waitsApproval = "approval"
	waitsQuestion = "question"
)

// workstreamsState is the Workstreams page's, and what the sidebar and the
// Session header take from it, kept between frames.
type workstreamsState struct {
	// list is nil until the first answer; older is a Host without
	// Workstreams, an older Droi.
	list    []gateway.Workstream
	listErr string
	older   bool
	listAt  time.Time
	loading bool
	// listAgain is a forced read asked for while one was under way.
	listAgain bool

	// pending are the prompts the Daemon counts for its opened Sessions.
	// The Daemon sends Prompts only to the Sessions this window loaded, and
	// counts a queued question only while the agent rests, so this is how
	// the window knows that an agent it has not loaded waits on a question.
	// pendingBusy is the read under way; pendingAgain a read asked for
	// during it, made once it answers.
	pending                   map[string]pendingPrompts
	pendingAt                 time.Time
	pendingBusy, pendingAgain bool
	// retries are the reads still to make, one at retryAt, after an agent
	// rested: the Daemon may count its question a moment after it rested.
	retries int
	retryAt time.Time
	// probed are the Sessions heard working that neither the list nor the
	// records had, for which the records were read once.
	probed map[string]bool
	// openedSessions reads the Daemon's opened Sessions; nil asks the
	// Daemon. Tests stand in for it: the Fake Daemon counts no prompts.
	openedSessions func(context.Context) (*protocol.ListOpenedSessionsResult, error)

	// members are the listed Workstream Sessions, and children the listed
	// continuations by the Session they continue, found again when the list
	// changes (membersRev); membersKey names the members and which are
	// archived.
	members    []sessions.Summary
	children   map[string][]sessions.Summary
	membersRev uint64
	membersSet bool
	membersKey string
	// seen is what each member was doing when last noted.
	seen map[string]activity.Activity
	// busy is this frame's activity of the Sessions.
	busy map[string]activity.Activity
	// waiting counts the agents, not archived, that wait on the user;
	// lifted is what an orchestrator's agents make it in the sidebar, and
	// orchWaiting and orchWorking how many of them wait and work, all by the
	// orchestrator's newest Session.
	waiting                  int
	lifted                   map[string]activity.Activity
	orchWaiting, orchWorking map[string]int

	// newOpen shows the New Workstream dialog, with the fields form holds.
	newOpen bool
	form    workstreamForm
	// acting is the action under way; notice is what the last one said,
	// an error when noticeErr.
	acting, notice string
	noticeErr      bool
	// script is the Host's list of the models that may use Script.
	script   scriptModels
	noticeAt time.Time
	// archiveFor is the Workstream whose archiving the dialog asks about.
	archiveFor string
	// settingsFor is the Workstream whose settings the dialog shows, "" for
	// none, and settings what its fields hold.
	settingsFor string
	settings    settingsForm
	// query filters the agents the side panel lists, fileQuery its files.
	query, fileQuery string
	// paneDrawer shows the side panel over the page of a narrow window;
	// files is the shared folder's list, read in the background.
	paneDrawer bool
	files      filesCache
	// tabMenuOpen is the orchestrator tab's menu; paneDrag the side panel's
	// width, in px of the default text size, while its edge is dragged.
	tabMenuOpen bool
	paneDrag    float32
	// uiRaw is the saved state of the Workstreams' windows as last decoded
	// into ui.
	uiRaw string
	ui    map[string]workstreamUI
	// checks are the Daemon's answers about the folders typed as the form's
	// workspace; typed and typedAt are the folder typed last and when, to
	// ask once typing pauses.
	checks  map[string]workspaceCheck
	typed   string
	typedAt time.Time
	// The form's fields, for the focus to go to the one in error.
	nameBox, workspaceBox ui.Handle
}

type pendingPrompts struct{ permissions, questions int }

// workspaceCheck is what the Daemon of the computer the window shows says
// of a folder: why it cannot work there, and whether it is in a git
// repository. unknown is a Daemon that could not be asked.
type workspaceCheck struct {
	done, git, unknown bool
	problem            string
	// branch is the repository's current branch.
	branch string
}

// workstreamForm is what the New Workstream dialog's fields hold.
type workstreamForm struct {
	name, workspace string
	symbol          string
	symbolOpen      bool
	// err is what is wrong with the field errOn, while it holds errFor.
	err, errOn, errFor string
}

// loadWorkstreams reads the Workstreams from the Gateway of the computer the
// window shows, and the prompts its Daemon counts.
func (a *App) loadWorkstreams(force bool) {
	s := &a.workstreams
	gw := a.cfg.Gateway
	if gw == nil {
		return
	}
	a.loadPending(force)
	if s.loading {
		s.listAgain = s.listAgain || force
		return
	}
	if !force && !s.listAt.IsZero() && a.cfg.Now().Sub(s.listAt) < workstreamsRefresh {
		return
	}
	s.loading = true
	go func() {
		list, err := gw.Workstreams(a.ctx)
		a.cfg.Update(func() {
			s.loading, s.listAt = false, a.cfg.Now()
			switch {
			case errors.Is(err, gateway.ErrNotFound):
				s.older, s.list, s.listErr = true, nil, ""
			case err != nil:
				s.listErr = L("Workstreams did not load: %s", err)
				// Soon again: the Gateway may only be starting.
				s.listAt = s.listAt.Add(5*time.Second - workstreamsRefresh)
			default:
				if list == nil {
					list = []gateway.Workstream{}
				}
				s.older, s.listErr, s.list = false, "", list
				a.pruneWsStates()
			}
			if s.listAgain {
				s.listAgain = false
				a.loadWorkstreams(true)
			}
		})
	}()
}

// loadPending reads the prompts each opened Session of the Daemon waits on,
// one read at a time.
func (a *App) loadPending(force bool) {
	s := &a.workstreams
	if a.ctl == nil {
		return
	}
	if s.pendingBusy {
		s.pendingAgain = s.pendingAgain || force
		return
	}
	if !force && !s.pendingAt.IsZero() && a.cfg.Now().Sub(s.pendingAt) < workstreamsRefresh {
		return
	}
	read := s.openedSessions
	if read == nil {
		read = func(ctx context.Context) (*protocol.ListOpenedSessionsResult, error) {
			cl, err := a.ctl.Client()
			if err != nil {
				return nil, err
			}
			return cl.ListOpenedSessions(ctx, protocol.ListOpenedSessionsParams{})
		}
	}
	s.pendingBusy = true
	go func() {
		res, err := read(a.ctx)
		var pending map[string]pendingPrompts
		var working map[string]string
		if err == nil {
			pending = map[string]pendingPrompts{}
			working = map[string]string{}
			for _, o := range res.Sessions {
				if o.WorkingState != "" && o.WorkingState != protocol.DroidWorkingStateIdle {
					working[o.SessionID] = string(o.WorkingState)
				}
				var p pendingPrompts
				if o.PendingPermissionRequestCount != nil {
					p.permissions = int(*o.PendingPermissionRequestCount)
				}
				if o.PendingAskUserRequestCount != nil {
					p.questions = int(*o.PendingAskUserRequestCount)
				}
				if p != (pendingPrompts{}) {
					pending[o.SessionID] = p
				}
			}
		}
		a.cfg.Update(func() {
			s.pendingBusy, s.pendingAt = false, a.cfg.Now()
			if err == nil {
				s.pending = pending
				a.noteWorking(working)
			}
			if s.pendingAgain {
				s.pendingAgain = false
				a.loadPending(true)
			}
		})
	}()
}

// noteWorking takes the working states the Daemon lists for the Sessions
// this window heard nothing of: a Session already at work when the window
// started tells of no change.
func (a *App) noteWorking(working map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, state := range working {
		if _, heard := a.busy[id]; !heard {
			a.busy[id] = state
		}
	}
}

// loaded reports whether this window has Session id loaded, and so hears
// its Prompts.
func (a *App) loaded(id string) bool {
	if a.ctl == nil {
		return false
	}
	s := a.ctl.Store().Session(id)
	return s != nil && s.LoadState() == session.Loaded
}

// noteWorkstreams follows the listed Workstream Sessions: it reads the
// Workstreams again when one of them starts, stops, waits or is archived, or
// a new one is listed, and counts the agents waiting on the user.
func (a *App) noteWorkstreams(c *ui.Context, listed []sessions.Summary, busy map[string]activity.Activity) {
	s := &a.workstreams
	s.busy = busy
	changed := false
	if !s.membersSet || s.membersRev != a.shownRev {
		var members []sessions.Summary
		children := map[string][]sessions.Summary{}
		var key strings.Builder
		for _, x := range listed {
			if x.ParentID != "" && x.ParentID != x.SessionID {
				children[x.ParentID] = append(children[x.ParentID], x)
			}
			if id, _, _ := sessions.Workstream(x.Tags); id != "" {
				members = append(members, x)
				key.WriteString(x.SessionID)
				if x.ArchivedAt != "" {
					key.WriteString("/archived")
				}
				key.WriteByte(',')
			}
		}
		s.members, s.children, s.membersRev, s.membersSet = members, children, a.shownRev, true
		if k := key.String(); k != s.membersKey {
			s.membersKey, changed = k, true
		}
	}
	if s.seen == nil {
		s.seen = map[string]activity.Activity{}
	}
	rested := false
	for _, x := range s.members {
		now := busy[x.SessionID]
		was, known := s.seen[x.SessionID]
		if known && was == now {
			continue
		}
		s.seen[x.SessionID], changed = now, true
		if !known || !sessions.IsWorkstreamAgent(x.Tags) || a.loaded(x.SessionID) {
			continue
		}
		// The prompts the Daemon counted for an agent this window has not
		// loaded hold until it works again; a question it asked during the
		// turn is counted once it rests.
		switch {
		case was != "" && now == "":
			rested = true
		case was == "" && now != "":
			delete(s.pending, x.SessionID)
		}
	}
	if rested {
		a.loadPending(true)
		s.retries, s.retryAt = 2, a.cfg.Now().Add(restedRetry)
	}
	if s.retries > 0 {
		if left := s.retryAt.Sub(a.cfg.Now()); left > 0 {
			c.After(left)
		} else {
			s.retries--
			s.retryAt = a.cfg.Now().Add(2 * restedRetry)
			a.loadPending(true)
		}
	}
	for id, state := range busy {
		if state == "" || s.probed[id] || a.knownToWorkstreams(id, listed) {
			continue
		}
		if s.probed == nil || len(s.probed) > 256 {
			s.probed = map[string]bool{}
		}
		s.probed[id] = true
		changed = true
	}
	if changed {
		a.loadWorkstreams(true)
	}
	if a.countWaiting() {
		// The count comes from the Daemon's list, which tells of no change.
		if left := workstreamsRefresh - a.cfg.Now().Sub(s.pendingAt); left > 0 {
			c.After(left)
		} else {
			a.loadPending(false)
		}
	}
}

// knownToWorkstreams reports whether Session id is one the list or the
// records have: a Session heard working that neither has may be an agent the
// Host just started, whose Session the Daemon lists late.
func (a *App) knownToWorkstreams(id string, listed []sessions.Summary) bool {
	for _, w := range a.workstreams.list {
		if w.OrchestratorID == id {
			return true
		}
		for _, ag := range w.Agents {
			if ag.SessionID == id {
				return true
			}
		}
	}
	return findSummary(listed, id) != nil
}

// countWaiting counts the agents waiting on the user and lifts their
// orchestrators. It reports whether a count comes from the Daemon's list of
// opened Sessions.
func (a *App) countWaiting() (fromList bool) {
	s := &a.workstreams
	if s.lifted == nil {
		s.lifted, s.orchWaiting, s.orchWorking = map[string]activity.Activity{}, map[string]int{}, map[string]int{}
	}
	clear(s.lifted)
	clear(s.orchWaiting)
	clear(s.orchWorking)
	s.waiting = 0
	for _, w := range s.list {
		if w.ArchivedAt != "" {
			continue
		}
		var lift activity.Activity
		n, working := 0, 0
		for _, ag := range w.Agents {
			if ag.ArchivedAt != "" {
				continue
			}
			id := a.resolve(ag.SessionID)
			how, listed := a.waitsOn(id)
			switch {
			case how != "":
				n++
				fromList = fromList || listed
			case s.busy[id] != "":
				lift = activity.Working
				working++
			}
		}
		s.waiting += n
		if n > 0 {
			lift = activity.NeedsInput
		}
		if lift != "" && w.OrchestratorID != "" {
			orch := a.resolve(w.OrchestratorID)
			s.lifted[orch] = lift
			s.orchWaiting[orch], s.orchWorking[orch] = n, working
		}
	}
	return fromList
}

// liftOrchestrators makes an orchestrator whose agent waits on the user
// count as needing input in busy, and one whose agent works as working, so
// that the sidebar ranks and marks it as it would its agents.
func (a *App) liftOrchestrators(busy map[string]activity.Activity) {
	for id, lift := range a.workstreams.lifted {
		if lift == activity.NeedsInput || busy[id] == "" {
			busy[id] = lift
		}
	}
}

// liftedStatus is what the sidebar row of an orchestrator says while it is
// idle itself and its agents are not: how many wait on the user, else how
// many work. ok is false when its own state is the one to show.
func (a *App) liftedStatus(id string) (label string, waiting, ok bool) {
	s := &a.workstreams
	if _, lifted := s.lifted[id]; !lifted || s.busy[id] != "" {
		return "", false, false
	}
	if n := s.orchWaiting[id]; n > 0 {
		return L("%d waiting on you", n), true, true
	}
	n := s.orchWorking[id]
	if n == 1 {
		return L("1 agent working"), false, true
	}
	return L("%d agents working", n), false, true
}

// resolve is the newest listed continuation of Session id, id itself when
// none is listed: a handoff goes on in a new Session, which the Host's
// records may not name yet.
func (a *App) resolve(id string) string {
	children := a.workstreams.children
	for range 64 {
		kids := children[id]
		if len(kids) == 0 {
			return id
		}
		next := kids[0]
		for _, k := range kids[1:] {
			if k.UpdatedAt > next.UpdatedAt {
				next = k
			}
		}
		id = next.SessionID
	}
	return id
}

// waitsOn is how the Session id waits on the user, "" when it does not: a
// permission request, or a question. fromList is whether that comes from the
// Daemon's list of opened Sessions, read now and then, rather than from
// what this window hears.
func (a *App) waitsOn(id string) (how string, fromList bool) {
	s := &a.workstreams
	doing := s.busy[id]
	if a.loaded(id) {
		if doing != activity.NeedsInput {
			return "", false
		}
		if len(a.ctl.PendingAskUsers(id)) > 0 && len(a.ctl.PendingPermissions(id)) == 0 {
			return waitsQuestion, false
		}
		return waitsApproval, false
	}
	p := s.pending[id]
	switch {
	case doing == activity.NeedsInput && p.questions > 0 && p.permissions == 0:
		return waitsQuestion, true
	case doing == activity.NeedsInput:
		return waitsApproval, false
	case doing == "" && p.questions > 0:
		return waitsQuestion, true
	}
	return "", false
}

func (a *App) findWorkstream(id string) *gateway.Workstream {
	if id == "" {
		return nil
	}
	for i := range a.workstreams.list {
		if a.workstreams.list[i].ID == id {
			return &a.workstreams.list[i]
		}
	}
	return nil
}

// wsAct runs one action on the Workstreams off the frame, then reads them
// again and says how it went: fn answers the notice of its success, failed
// the notice of its error; then runs on success.
func (a *App) wsAct(action string, fn func() (string, error), failed func(error) string, then func()) {
	s := &a.workstreams
	if s.acting != "" {
		return
	}
	s.acting, s.notice = action, ""
	go func() {
		done, err := fn()
		a.cfg.Update(func() {
			s.acting = ""
			if err == nil && then != nil {
				// then may go to another page, which drops the notice.
				then()
			}
			s.noticeAt, s.noticeErr, s.notice = a.cfg.Now(), err != nil, done
			if err != nil {
				s.notice = failed(err)
			}
			a.loadWorkstreams(true)
		})
	}()
}

// workstreamNotice is what the last action said: an error until the next
// action, a success for a few seconds.
func (a *App) workstreamNotice(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	if s.notice == "" {
		return
	}
	icon, color := "circle-check", t.Success
	if s.noticeErr {
		icon, color = "circle-alert", t.DestructiveForeground
	} else {
		left := noticeFor - a.cfg.Now().Sub(s.noticeAt)
		if left <= 0 {
			return
		}
		c.After(left)
	}
	ui.Row(c.Key("notice")).Role(ui.RoleStatus).Label(s.notice).Gap(k.Px(8)).AlignItems(ui.Start).
		Transition(ui.ElementTransition{Duration: 150 * time.Millisecond, Enter: &ui.Motion{Y: -4}, Exit: &ui.Motion{}}).Children(func() {
		k.Icon(c, icon, 14, color).Margin(k.Px(3), 0, 0, 0)
		k.Text(c, s.notice, k.Dense(12, 13), k.Dense(18, 20)).TextColor(t.Foreground).Grow(1).MinWidth(0)
	})
}

// listName joins the parts of an accessible name with the language's comma.
func listName(parts ...string) string {
	name := parts[0]
	for _, p := range parts[1:] {
		name = L("%s, %s", name, p)
	}
	return name
}

// agentStatus is where an agent's row goes: "archived", "waiting",
// "working" or "idle", and how a waiting one waits. An agent handed off to
// a new Session is that Session.
func (a *App) agentStatus(ag gateway.WorkstreamAgent) (string, string) {
	if ag.ArchivedAt != "" {
		return "archived", ""
	}
	id := a.resolve(ag.SessionID)
	if how, _ := a.waitsOn(id); how != "" {
		return "waiting", how
	}
	if a.workstreams.busy[id] != "" {
		return "working", ""
	}
	return "idle", ""
}

// fitEffort keeps effort for the model id when it supports it, else "".
func fitEffort(choices []models.Choice, id, effort string) string {
	for _, m := range choices {
		if m.ID == id && slices.Contains(m.ReasoningEfforts, effort) {
			return effort
		}
	}
	return ""
}

// saveAgentModel sets a Workstream's agent model settings, shown at once.
func (a *App) saveAgentModel(id, model, effort string) {
	gw := a.cfg.Gateway
	if gw == nil || a.workstreams.acting != "" {
		return
	}
	if w := a.findWorkstream(id); w != nil {
		w.AgentModel, w.AgentReasoningEffort = model, effort
	}
	a.wsAct("save", func() (string, error) {
		err := gw.SetWorkstreamAgentSettings(a.ctx, gateway.WorkstreamSettings{ID: id, AgentModel: model, AgentReasoningEffort: effort})
		return L("Saved. Agents switch before their next turn."), err
	}, func(err error) string { return L("Could not save the agent model: %s", err) }, nil)
}

// archiveWorkstreamDialog asks before the Workstream archiveFor names is
// archived.
func (a *App) archiveWorkstreamDialog(c *ui.Context) {
	s := &a.workstreams
	if s.archiveFor == "" {
		return
	}
	k, t := a.kit, a.kit.T
	width, _ := c.Size()
	id := s.archiveFor
	title := id
	if w := a.findWorkstream(id); w != nil {
		title = trimOr(w.Title, w.ID)
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(t.Scrim)
		panel.Role(ui.RoleAlertDialog).Label(L("Archive “%s”?", title)).Width(min(k.Px(420), width-k.Px(32))).Padding(k.Px(20)).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Shadow(0, k.Px(20), k.Px(25), -k.Px(5), t.Shadow)
		ui.Column(c).FillWidth().Gap(k.Px(6)).Children(func() {
			k.Text(c, L("Archive “%s”?", title), k.Dense(14, 15), 22).Role(ui.RoleHeading).FontWeight(600)
			k.Text(c, L("Its agents stop and are archived with the orchestrator. Worktrees without changes are deleted; ones with changes stay, and Droi lists them. The shared folder stays."), k.Dense(12, 13), k.Dense(18, 20)).
				TextColor(t.MutedForeground)
			ui.Row(c).Gap(k.Px(8)).Justify(ui.End).Margin(k.Px(14), 0, 0, 0).Children(func() {
				if a.smallButton(c, kit.Outline, L("Cancel"), "", false).Clicked() {
					open = false
				}
				if a.dangerButton(c, L("Archive Workstream"), s.acting != "").Clicked() {
					open = false
					a.archiveWorkstream(id)
				}
			})
		})
	})
	if !open {
		s.archiveFor = ""
	}
}

// archivedNotice says a Workstream was archived, with the worktrees kept.
func archivedNotice(kept []string) string {
	switch len(kept) {
	case 0:
		return L("Archived.")
	case 1:
		return L("Archived. Kept 1 worktree with changes: %s", kept[0])
	}
	return L("Archived. Kept %d worktrees with changes: %s", len(kept), strings.Join(kept, ", "))
}

func (a *App) archiveWorkstream(id string) {
	gw := a.cfg.Gateway
	if gw == nil {
		return
	}
	a.wsAct("archive", func() (string, error) {
		kept, err := gw.ArchiveWorkstream(a.ctx, id)
		return archivedNotice(kept), err
	}, archiveFailed, func() { go a.reloadList() })
}

// archiveFailed is what the window says of an archive that failed: one the
// Host's bound, or the window's own wait, cut short is finished by
// archiving again.
func archiveFailed(err error) string {
	if errors.Is(err, gateway.ErrWorkstreamArchiveUnfinished) || errors.Is(err, context.DeadlineExceeded) {
		return L("Archiving took too long, and some agents are not archived yet. Archive the workstream again to finish.")
	}
	return L("Could not archive the workstream: %s", err)
}

// checkWorkspace is what the Daemon said of the folder dir, asked once
// typing pauses; a zero check until it answers.
func (a *App) checkWorkspace(c *ui.Context, dir string) workspaceCheck {
	s := &a.workstreams
	now := a.cfg.Now()
	if dir != s.typed {
		s.typed, s.typedAt = dir, now
	}
	if dir == "" {
		return workspaceCheck{}
	}
	if ch, ok := s.checks[dir]; ok {
		return ch
	}
	if left := workspaceCheckDelay - now.Sub(s.typedAt); left > 0 {
		c.After(left)
		return workspaceCheck{}
	}
	a.askWorkspace(dir)
	return workspaceCheck{}
}

// workspaceRetry is how long the form waits to ask a Daemon that could not
// be asked again.
const workspaceRetry = 2 * time.Second

// askWorkspace asks the Daemon whether it can work in dir, and whether dir
// is in a git repository. An answer a Daemon that could not be asked leaves
// is kept as none, to ask again.
func (a *App) askWorkspace(dir string) {
	s := &a.workstreams
	if s.checks == nil {
		s.checks = map[string]workspaceCheck{}
	}
	if a.ctl == nil {
		return
	}
	s.checks[dir] = workspaceCheck{}
	go func() {
		ch := workspaceCheck{done: true}
		cl, err := a.ctl.Client()
		var valid *protocol.ValidateWorkingDirectoryResult
		if err == nil {
			valid, err = cl.ValidateWorkingDirectory(a.ctx, protocol.ValidateWorkingDirectoryParams{WorkingDirectory: dir})
		}
		switch {
		case err != nil:
			ch.unknown = true
		case !valid.IsValid:
			ch.problem = valid.Error
			if ch.problem == "" {
				ch.problem = L("%s is not a usable directory.", dir)
			}
		default:
			resolved := cmp.Or(valid.ResolvedPath, dir)
			res, gerr := cl.ListGitBranches(a.ctx, protocol.ListGitBranchesParams{Cwd: resolved})
			ch.git, ch.unknown = gerr == nil && res.IsGitRepository, gerr != nil
			if ch.git {
				ch.branch = res.CurrentBranch
			}
		}
		a.cfg.Update(func() {
			if !ch.unknown {
				s.checks[dir] = ch
				return
			}
			delete(s.checks, dir)
			if s.typed == dir {
				s.typedAt = a.cfg.Now().Add(workspaceRetry - workspaceCheckDelay)
			}
		})
	}()
}
