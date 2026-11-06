package app

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
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
	"github.com/kkkk2323/droi/apps/native/pkg/settingsview"
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

// newWorkstreamTab is the Route.Tab of the New workstream form.
const newWorkstreamTab = "new"

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

	form workstreamForm
	// acting is the action under way; notice is what the last one said,
	// an error when noticeErr.
	acting, notice string
	noticeErr      bool
	noticeAt       time.Time
	menuOpen       bool
	confirmArchive bool
	// archivedOpen unfolds the archived Workstreams, archivedAgentsOpen a
	// Workstream's archived agents.
	archivedOpen, archivedAgentsOpen bool
	agentModelOpen                   bool
	agentPicker                      pickerState
	// checks are the Daemon's answers about the folders typed as the form's
	// workspace; typed and typedAt are the folder typed last and when, to
	// ask once typing pauses.
	checks  map[string]workspaceCheck
	typed   string
	typedAt time.Time
	// The form's fields, for the focus to go to the one in error.
	nameBox, workspaceBox, goalBox ui.Handle
}

type pendingPrompts struct{ permissions, questions int }

// workspaceCheck is what the Daemon of the computer the window shows says
// of a folder: why it cannot work there, and whether it is in a git
// repository. unknown is a Daemon that could not be asked.
type workspaceCheck struct {
	done, git, unknown bool
	problem            string
}

// workstreamForm is what the New workstream form's fields hold. The models
// and efforts are "" for the defaults: the Daemon's for the orchestrator,
// the orchestrator's for the agents.
type workstreamForm struct {
	name, workspace, goal   string
	model, effort           string
	agentModel, agentEffort string
	modelOpen, agentOpen    bool
	picker, agentPicker     pickerState
	// err is what is wrong with the field errOn, while it holds errFor.
	err, errOn, errFor string
}

func allWorkstreams() crumb {
	return crumb{label: L("Workstreams"), name: L("All workstreams"), to: Route{Name: "workstreams"}}
}

// enterWorkstreams starts a page of Workstreams afresh, with the records
// read again.
func (a *App) enterWorkstreams(r Route) {
	s := &a.workstreams
	s.menuOpen, s.confirmArchive, s.agentModelOpen, s.notice = false, false, false, ""
	if r.Tab == newWorkstreamTab {
		s.form, s.checks, s.typed = workstreamForm{}, nil, ""
	}
	a.loadWorkstreams(true)
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

// workstreamsPage lists the Workstreams, shows one, or starts one.
func (a *App) workstreamsPage(c *ui.Context) {
	k := a.kit
	s := &a.workstreams
	a.loadWorkstreams(false)
	if !s.listAt.IsZero() {
		// An idle window reads them again too.
		if left := workstreamsRefresh - a.cfg.Now().Sub(s.listAt); left > 0 {
			c.After(left)
		}
	}
	ui.Column(c).Role(ui.RoleGroup).Label(L("Workstreams")).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(k.Px(720)).AlignSelf(ui.Center).Gap(k.Px(k.Dense(20, 24))).Padding(k.Px(4), k.Px(32), k.Px(48), k.Px(32)).Children(func() {
				id := a.route.Workstream
				switch {
				case s.older:
					a.olderHostWorkstreams(c)
				case a.route.Tab == newWorkstreamTab:
					a.workstreamEditor(c)
				case id == "":
					a.workstreamList(c)
				case a.findWorkstream(id) == nil:
					a.missingWorkstream(c)
				default:
					a.workstreamView(c, *a.findWorkstream(id))
				}
			})
		})
		if w := a.findWorkstream(a.route.Workstream); w != nil && s.confirmArchive {
			a.archiveWorkstreamDialog(c, *w)
		}
	})
}

// olderHostWorkstreams says the computer's Droi has no Workstreams.
func (a *App) olderHostWorkstreams(c *ui.Context) {
	k, t := a.kit, a.kit.T
	name := a.computerName()
	if name == "" {
		name = L("This computer")
	}
	a.automationHeader(c, nil, L("Workstreams"), nil, nil)
	k.Text(c, L("%s runs an older Droi without workstreams. Update Droi there.", name), k.Dense(12, 13), k.Dense(18, 20)).Role(ui.RoleStatus).
		TextColor(t.MutedForeground).Margin(-k.Px(16), 0, 0, 0)
}

func (a *App) workstreamList(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	var active, archived []gateway.Workstream
	for _, w := range s.list {
		if w.ArchivedAt != "" {
			archived = append(archived, w)
		} else {
			active = append(active, w)
		}
	}
	ui.Column(c).Gap(k.Px(4)).Children(func() {
		a.automationHeader(c, nil, L("Workstreams"), func() {
			if len(active) > 0 {
				k.Text(c, strconv.Itoa(len(active)), k.Dense(12, 13), k.Dense(18, 20)).TextColor(t.MutedForeground).FontFeatures("tnum")
			}
		}, func() {
			if len(s.list) > 0 && a.smallButton(c, kit.Primary, L("New workstream"), "plus", false).Clicked() {
				a.Go(Route{Name: "workstreams", Tab: newWorkstreamTab})
			}
		})
		k.Text(c, L("An orchestrator session splits a long goal among agents. Each agent keeps its own session and worktree, and reports back to the orchestrator."), k.Dense(12, 13), k.Dense(18, 20)).
			TextColor(t.MutedForeground)
	})
	a.workstreamNotice(c)
	switch {
	case s.list == nil && s.listErr != "":
		a.loadFailed(c, s.listErr, func() { a.loadWorkstreams(true) })
	case s.list == nil:
		a.skeletonRows(c, L("Loading workstreams"), 3, k.Dense(48, 56))
	case len(s.list) == 0:
		a.noWorkstreams(c)
	default:
		if s.listErr != "" {
			k.Text(c, s.listErr, k.Dense(12, 13), k.Dense(18, 20)).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
		}
		if len(active) > 0 {
			ui.Column(c).Role(ui.RoleList).Label(L("Workstreams")).Children(func() {
				for _, w := range active {
					a.workstreamRow(c, w)
				}
			})
		}
		if len(archived) > 0 {
			ui.Column(c).Gap(k.Px(4)).Children(func() {
				a.foldToggle(c, L("Archived"), len(archived), &s.archivedOpen)
				if s.archivedOpen {
					ui.Column(c).Role(ui.RoleList).Label(L("Archived")).Children(func() {
						for _, w := range archived {
							a.workstreamRow(c, w)
						}
					})
				}
			})
		}
	}
}

// foldToggle heads a folded part of a page: its title and count, and a
// chevron that turns as it opens.
func (a *App) foldToggle(c *ui.Context, title string, n int, open *bool) {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Label(title).Expanded(*open).AlignSelf(ui.Start).Height(k.Px(k.Dense(24, 28))).Gap(k.Px(6)).PaddingX(k.Px(6)).
		Margin(0, 0, 0, -k.Px(6)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.Muted.Alpha(0.5))
		color = t.Foreground
	}
	b.Children(func() {
		k.Text(c, title, k.Dense(12, 13), k.Dense(18, 20)).Role(ui.RoleHeading).FontWeight(600).TextColor(t.Foreground)
		k.Text(c, strconv.Itoa(n), k.Dense(12, 13), k.Dense(18, 20)).TextColor(t.MutedForeground).FontFeatures("tnum")
		rot := float32(0)
		if *open {
			rot = 90
		}
		k.Icon(c, "chevron-right", 12, color).Rotate(rot)
	})
	if b.Clicked() {
		*open = !*open
	}
}

// noWorkstreams is the list before the first Workstream.
func (a *App) noWorkstreams(c *ui.Context) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Role(ui.RoleGroup).Label(L("No workstreams yet")).AlignItems(ui.Center).Gap(k.Px(6)).Padding(k.Px(k.Dense(32, 40)), k.Px(24)).
		Radius(k.Px(12)).Border(1, t.Border).Children(func() {
		ui.Box(c).Size(k.Px(36), k.Px(36)).Radius(k.Px(10)).Background(t.Muted).Center().Margin(0, 0, k.Px(6), 0).Children(func() {
			k.Icon(c, "git-fork", 18, t.MutedForeground)
		})
		k.Text(c, L("No workstreams yet"), k.Dense(13, 14), 20).FontWeight(500)
		k.Text(c, L("Start one for work that spans days and splits into parts, such as a migration done in stages."), k.Dense(12, 13), k.Dense(18, 20)).
			TextColor(t.MutedForeground).MaxWidth(k.Px(360)).TextAlign(ui.Center)
		if a.smallButton(c, kit.Primary, L("New workstream"), "plus", false).Margin(k.Px(10), 0, 0, 0).Clicked() {
			a.Go(Route{Name: "workstreams", Tab: newWorkstreamTab})
		}
	})
}

// agentsCount is how many agents a Workstream has: those not archived, or
// all of an archived one's.
func agentsCount(w gateway.Workstream) string {
	n := 0
	for _, ag := range w.Agents {
		if ag.ArchivedAt == "" || w.ArchivedAt != "" {
			n++
		}
	}
	if n == 1 {
		return L("1 agent")
	}
	return L("%d agents", n)
}

// waitingAgents counts a Workstream's agents waiting on the user.
func (a *App) waitingAgents(w gateway.Workstream) int {
	n := 0
	for _, ag := range w.Agents {
		if how, _ := a.waitsOn(a.resolve(ag.SessionID)); ag.ArchivedAt == "" && how != "" {
			n++
		}
	}
	return n
}

// waitingMark is a count of agents waiting on the user: a live status where
// it stands alone, an image inside a button whose name says it already.
func (a *App) waitingMark(c *ui.Context, n int, size, line float32, live bool) ui.Element {
	k, t := a.kit, a.kit.T
	role := ui.RoleImage
	if live {
		role = ui.RoleStatus
	}
	return ui.Row(c).Role(role).Label(L("%d waiting on you", n)).Tooltip(L("%d waiting on you", n)).Gap(k.Px(4)).AlignItems(ui.Center).Shrink(0).Children(func() {
		k.Icon(c, "circle-alert", 12, t.Attention)
		k.Text(c, itoa(n), size, line).TextColor(t.Attention).FontFeatures("tnum")
	})
}

// listRow is a row of a list on these pages: a list item holding the button
// that opens it, named label for assistive technology.
func (a *App) listRow(c *ui.Context, key, label string, height float32, body func(hover bool)) bool {
	k, t := a.kit, a.kit.T
	clicked := false
	ui.Row(c.Key(key)).Role(ui.RoleListItem).Transition(rowMotion).Children(func() {
		b := ui.ButtonBase(c).Label(label).Grow(1).MinWidth(0).Height(k.Px(height)).PaddingX(k.Px(12)).Gap(k.Px(k.Dense(8, 10))).
			Justify(ui.Start).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Cursor(ui.CursorPointer)
		hover := b.Hovered()
		if hover {
			b.Background(t.Muted.Alpha(0.5))
		}
		b.Children(func() { body(hover) })
		clicked = b.Clicked()
	})
	return clicked
}

// listName joins the parts of an accessible name with the language's comma.
func listName(parts ...string) string {
	name := parts[0]
	for _, p := range parts[1:] {
		name = L("%s, %s", name, p)
	}
	return name
}

func (a *App) workstreamRow(c *ui.Context, w gateway.Workstream) {
	k, t := a.kit, a.kit.T
	title := trimOr(w.Title, w.ID)
	count, waiting := agentsCount(w), a.waitingAgents(w)
	label := listName(title, count)
	if waiting > 0 {
		label = listName(label, L("%d waiting on you", waiting))
	}
	if a.listRow(c, w.ID, label, k.Dense(48, 56), func(hover bool) {
		k.Icon(c, "git-fork", 14, t.MutedForeground)
		ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
			k.Text(c, title, k.Dense(13, 14), k.Dense(18, 20)).FontWeight(500).SingleLine()
			k.Text(c, w.Workspace, k.Dense(11, 12), 16).TextColor(t.MutedForeground).SingleLine()
		})
		if waiting > 0 {
			a.waitingMark(c, waiting, k.Dense(11, 12), 16, false)
		}
		k.Text(c, count, k.Dense(11, 12), 16).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(0)
		chevron := k.Icon(c, "chevron-right", 14, t.MutedForeground)
		if !hover {
			chevron.Opacity(0)
		}
	}) {
		a.Go(Route{Name: "workstreams", Workstream: w.ID})
	}
}

// missingWorkstream is the page of a Workstream the list does not have:
// not loaded yet, or gone.
func (a *App) missingWorkstream(c *ui.Context) {
	k := a.kit
	s := &a.workstreams
	if s.list == nil && s.listErr == "" {
		a.automationHeader(c, []crumb{allWorkstreams(), {label: "…"}}, "", nil, nil)
		a.skeletonRows(c, L("Loading workstreams"), 2, k.Dense(36, 40))
		return
	}
	if s.list == nil {
		a.automationHeader(c, []crumb{allWorkstreams(), {label: "…"}}, "", nil, nil)
		a.loadFailed(c, s.listErr, func() { a.loadWorkstreams(true) })
		return
	}
	a.automationHeader(c, []crumb{allWorkstreams(), {label: L("Not found")}}, L("Not found"), nil, nil)
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

func (a *App) workstreamView(c *ui.Context, w gateway.Workstream) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	title := trimOr(w.Title, w.ID)
	busy := s.acting != ""
	archived := w.ArchivedAt != ""
	a.automationHeader(c, []crumb{allWorkstreams(), {label: title}}, title, func() {
		if archived {
			a.statusPill(c, false, L("Archived"))
		}
	}, func() {
		if w.OrchestratorID != "" && a.smallButton(c, kit.Primary, L("Open orchestrator"), "message-square", false).Clicked() {
			a.Go(Route{Name: "session", SessionID: a.resolve(w.OrchestratorID)})
		}
		if !a.following() && a.cfg.OpenPath != nil && w.SharedFolder != "" &&
			a.smallButton(c, kit.Outline, L("Open shared folder"), "folder-open", false).Clicked() {
			dir := w.SharedFolder
			go func() { _ = a.cfg.OpenPath(dir) }()
		}
		if archived {
			return
		}
		more := k.IconButton(c, "ellipsis", L("More actions"), k.Dense(28, 32)).Expanded(s.menuOpen).Disabled(busy)
		if more.Clicked() {
			s.menuOpen = !s.menuOpen
		}
		k.MenuPopup(c, more, &s.menuOpen, true, L("More actions"), func() {
			if k.MenuItem(c, &s.menuOpen, "archive", L("Archive workstream…")).Clicked() {
				s.confirmArchive = true
			}
		})
	})
	a.workstreamNotice(c)
	ui.Column(c).Role(ui.RoleGroup).Label(L("Details")).Gap(k.Px(8)).Children(func() {
		a.fact(c, L("Workspace"), func() {
			k.Text(c, w.Workspace, k.Dense(11, 12), 20).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0)
		})
		a.fact(c, L("Shared folder"), func() {
			k.Text(c, w.SharedFolder, k.Dense(11, 12), 20).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0)
		})
		a.fact(c, L("Agent model"), func() {
			var choices []models.Choice
			if dv := a.sessionDefaults(); dv != nil {
				choices = dv.Models
			}
			id := w.ID
			model, effort := w.AgentModel, w.AgentReasoningEffort
			settingsview.ModelEffortField(c, a.kit, a.prefs, L("Agent model"), L("Same as orchestrator"), busy || archived, &s.agentModelOpen, &s.agentPicker,
				choices, model, effort,
				func(next string) { a.saveAgentModel(id, next, fitEffort(choices, next, effort)) },
				func(next string) { a.saveAgentModel(id, model, next) })
		})
	})
	groups := map[string][]gateway.WorkstreamAgent{}
	how := map[string]string{}
	for _, ag := range w.Agents {
		status, waits := a.agentStatus(ag)
		groups[status] = append(groups[status], ag)
		how[ag.SessionID] = waits
	}
	if len(w.Agents) == 0 {
		k.Text(c, L("No agents yet. The orchestrator starts them as it splits the work."), k.Dense(12, 13), k.Dense(18, 20)).TextColor(t.MutedForeground)
		return
	}
	for _, g := range []struct{ status, title string }{
		{"waiting", L("Waiting on you")}, {"working", L("Working")}, {"idle", L("Idle")},
	} {
		list := groups[g.status]
		if len(list) == 0 {
			continue
		}
		ui.Column(c.Key("group:" + g.status)).Gap(k.Px(4)).Children(func() {
			a.sectionTitle(c, g.title, strconv.Itoa(len(list)))
			ui.Column(c).Role(ui.RoleList).Label(g.title).Children(func() {
				for _, ag := range list {
					a.agentRow(c, ag, g.status, g.title, how[ag.SessionID])
				}
			})
		})
	}
	if list := groups["archived"]; len(list) > 0 {
		ui.Column(c.Key("group:archived")).Gap(k.Px(4)).Children(func() {
			// An archived Workstream's agents are all it has left to show.
			open := archived || s.archivedAgentsOpen
			if archived {
				a.sectionTitle(c, L("Archived"), strconv.Itoa(len(list)))
			} else {
				a.foldToggle(c, L("Archived"), len(list), &s.archivedAgentsOpen)
			}
			if open {
				ui.Column(c).Role(ui.RoleList).Label(L("Archived")).Children(func() {
					for _, ag := range list {
						a.agentRow(c, ag, "archived", L("Archived"), "")
					}
				})
			}
		})
	}
}

// reportKinds name an agent's reports.
var reportKinds = map[string]func() string{
	"done":    func() string { return L("Finished") },
	"blocked": func() string { return L("Blocked") },
	"update":  func() string { return L("Progress update") },
}

// agentRow opens the agent's Session, its newest after a handoff: its
// title, what it is doing, its branch, and its last report and when. group
// is the title of the group it is in.
func (a *App) agentRow(c *ui.Context, ag gateway.WorkstreamAgent, status, group, waits string) {
	k, t := a.kit, a.kit.T
	title := trimOr(ag.Title, ag.SessionID)
	state, color := group, t.MutedForeground
	label := listName(title, group)
	if status == "waiting" {
		state, color = L("Needs approval"), t.AttentionForeground
		if waits == waitsQuestion {
			state = L("Has a question")
		}
		label = listName(label, state)
	}
	if a.listRow(c, ag.SessionID, label, k.Dense(44, 48), func(hover bool) {
		ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
			switch status {
			case "waiting":
				k.Icon(c, "circle-alert", 14, t.Attention)
			case "working":
				k.Spinner(c, 14, t.Info)
			case "idle":
				kit.Dot(c, k.Px(8), t.MutedForeground.Alpha(0.5))
			default:
				k.Icon(c, "archive", 14, t.MutedForeground)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
			k.Text(c, title, k.Dense(12, 13), k.Dense(18, 20)).FontWeight(500).SingleLine()
			ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
				k.Text(c, state, k.Dense(11, 12), 16).TextColor(color).SingleLine().Shrink(0)
				if ag.Branch != "" {
					ui.Row(c).Role(ui.RoleImage).Label(L("Worktree on %s", ag.Branch)).Gap(k.Px(4)).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
						k.Icon(c, "git-branch", 12, t.MutedForeground)
						k.Text(c, ag.Branch, k.Dense(11, 12), 16).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0)
					})
				}
			})
		})
		if r := ag.LastReport; r != nil {
			kind := r.Kind
			if name, ok := reportKinds[r.Kind]; ok {
				kind = name()
			}
			if at, err := time.Parse(time.RFC3339, r.At); err == nil {
				kind += " · " + relativeTime(at, a.cfg.Now())
			}
			k.Text(c, kind, k.Dense(11, 12), 16).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(0).Tooltip(r.Text)
		}
		chevron := k.Icon(c, "chevron-right", 14, t.MutedForeground)
		if !hover {
			chevron.Opacity(0)
		}
	}) {
		a.Go(Route{Name: "session", SessionID: a.resolve(ag.SessionID)})
	}
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

// archiveWorkstreamDialog asks before the Workstream w is archived.
func (a *App) archiveWorkstreamDialog(c *ui.Context, w gateway.Workstream) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	width, _ := c.Size()
	id, title := w.ID, trimOr(w.Title, w.ID)
	ui.DialogBase(c, &s.confirmArchive, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Role(ui.RoleAlertDialog).Label(L("Archive “%s”?", title)).Width(min(k.Px(420), width-k.Px(32))).Padding(k.Px(20)).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Gap(k.Px(6)).Children(func() {
			k.Text(c, L("Archive “%s”?", title), k.Dense(14, 15), 22).Role(ui.RoleHeading).FontWeight(600)
			k.Text(c, L("Its agents stop and are archived with the orchestrator. Worktrees without changes are deleted; ones with changes stay, and Droi lists them. The shared folder stays."), k.Dense(12, 13), k.Dense(18, 20)).
				TextColor(t.MutedForeground)
			ui.Row(c).Gap(k.Px(8)).Justify(ui.End).Margin(k.Px(14), 0, 0, 0).Children(func() {
				if a.smallButton(c, kit.Outline, L("Cancel"), "", false).Clicked() {
					s.confirmArchive = false
				}
				if a.dangerButton(c, L("Archive workstream"), s.acting != "").Clicked() {
					s.confirmArchive = false
					a.archiveWorkstream(id)
				}
			})
		})
	})
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

// workstreamEditor is the New workstream form.
func (a *App) workstreamEditor(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	f := &s.form
	var choices []models.Choice
	shownModel, shownEffort := f.model, f.effort
	if dv := a.sessionDefaults(); dv != nil {
		choices = dv.Models
		if shownModel == "" {
			shownModel, shownEffort = dv.ModelID, dv.ReasoningEffort
		}
	}
	dir := strings.TrimSpace(f.workspace)
	check := a.checkWorkspace(c, dir)
	values := map[string]string{"name": f.name, "workspace": f.workspace, "goal": f.goal}
	problem := func(field string) string {
		switch {
		case f.errOn == field && values[field] == f.errFor:
			return f.err
		case field == "workspace":
			return check.problem
		}
		return ""
	}
	invalid := func(in ui.Element, field string) ui.Element {
		if problem(field) != "" {
			in.Border(1, t.Destructive.Alpha(0.6))
		}
		return in
	}
	title := L("New workstream")
	a.automationHeader(c, []crumb{allWorkstreams(), {label: title}}, title, nil, func() {
		if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
			a.Go(Route{Name: "workstreams"})
		}
		shown := L("Start workstream")
		if s.acting == "start" {
			shown = L("Starting…")
		}
		if a.smallButton(c, kit.Primary, shown, "", s.acting != "").Label(L("Start workstream")).Clicked() {
			a.startWorkstream(shownModel)
		}
	})
	a.workstreamNotice(c)
	note := L("This folder is not a git repository, so agents share it instead of working in worktrees of their own.")
	ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(k.Dense(16, 20))).Children(func() {
		a.formField(c, L("Name"), "", problem("name"), func() {
			ui.Row(c).Children(func() {
				invalid(a.field(c, &f.name, L("Workstream name"), L("Payment service migration"), false, false), "name").Bind(&s.nameBox)
			})
		})
		a.formField(c, L("Workspace"), L("The repository the agents work in. Each agent gets its own worktree of it."), problem("workspace"), func() {
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				invalid(a.field(c, &f.workspace, L("Workstream workspace"), "/Users/you/project", true, false), "workspace").Bind(&s.workspaceBox)
				if !a.following() && a.smallButton(c, kit.Outline, L("Choose…"), "folder-open", false).Height(k.Px(k.Dense(30, 36))).Label(L("Choose a workspace")).Clicked() {
					go func() {
						if dir := a.pickFolder(L("Choose a workspace")); dir != "" {
							a.cfg.Update(func() { f.workspace = dir })
						}
					}()
				}
			})
			if check.done && !check.unknown && check.problem == "" && !check.git {
				ui.Row(c).Role(ui.RoleStatus).Label(note).Gap(k.Px(6)).AlignItems(ui.Start).Children(func() {
					k.Icon(c, "info", 12, t.MutedForeground).Margin(k.Px(3), 0, 0, 0)
					k.Text(c, note, k.Dense(11, 12), 18).TextColor(t.MutedForeground).Grow(1).MinWidth(0)
				})
			}
		})
		a.formField(c, L("Goal"), L("The first message to the orchestrator: what to achieve, and what it should know."), problem("goal"), func() {
			border := t.Border
			if problem("goal") != "" {
				border = t.Destructive.Alpha(0.6)
			}
			ui.Row(c).Radius(k.Px(10)).Border(1, border).Background(t.Background).Children(func() {
				k.TextArea(c, &f.goal, L("Workstream goal"), L("Move the payment service from REST to gRPC, one endpoint at a time. Keep the old API until every client has switched."),
					kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: k.Dense(13, 14), Line: k.Dense(18, 20), MinLines: 5, Color: t.Foreground}).Bind(&s.goalBox)
			})
		})
		a.formField(c, L("Orchestrator model"), L("The orchestrator plans and coordinates; it does not edit code."), "", func() {
			ui.Row(c).Children(func() {
				settingsview.ModelEffortField(c, a.kit, a.prefs, L("Orchestrator model"), "", false, &f.modelOpen, &f.picker, choices, shownModel, shownEffort,
					func(next string) { f.model, f.effort = next, fitEffort(choices, next, shownEffort) },
					func(next string) { f.model, f.effort = shownModel, next })
			})
		})
		a.formField(c, L("Agent model"), L("Every agent of this workstream uses it. Change it later on the workstream’s page: agents switch before their next turn, except one whose model you changed in its own session."), "", func() {
			ui.Row(c).Children(func() {
				settingsview.ModelEffortField(c, a.kit, a.prefs, L("Agent model"), L("Same as orchestrator"), false, &f.agentOpen, &f.agentPicker, choices, f.agentModel, f.agentEffort,
					func(next string) { f.agentModel, f.agentEffort = next, fitEffort(choices, next, f.agentEffort) },
					func(next string) { f.agentEffort = next })
			})
		})
	})
}

// startWorkstream starts the form's Workstream on the computer the window
// shows, then opens its orchestrator's Session; a field in error takes the
// focus.
func (a *App) startWorkstream(shownModel string) {
	s := &a.workstreams
	f := &s.form
	f.err, f.errOn = "", ""
	name, workspace, goal := strings.TrimSpace(f.name), strings.TrimSpace(f.workspace), strings.TrimSpace(f.goal)
	switch {
	case name == "":
		f.err, f.errOn = L("Enter a name."), "name"
		s.nameBox.Focus()
	case workspace == "":
		f.err, f.errOn = L("Choose a workspace."), "workspace"
		s.workspaceBox.Focus()
	case goal == "":
		f.err, f.errOn = L("Enter the goal."), "goal"
		s.goalBox.Focus()
	}
	if f.err == "" && s.checks[workspace].problem != "" {
		// The folder may have been made since: the Host decides, and the
		// form asks again.
		delete(s.checks, workspace)
		a.askWorkspace(workspace)
	}
	gw := a.cfg.Gateway
	if f.err != "" {
		f.errFor = map[string]string{"name": f.name, "workspace": f.workspace, "goal": f.goal}[f.errOn]
	}
	if f.err != "" || gw == nil {
		return
	}
	model := f.model
	if model == "" && f.effort != "" {
		model = shownModel
	}
	in := gateway.NewWorkstream{Title: name, Workspace: workspace, Goal: goal, Model: model, ReasoningEffort: f.effort,
		AgentModel: f.agentModel, AgentReasoningEffort: f.agentEffort}
	var made gateway.WorkstreamCreated
	a.wsAct("start", func() (string, error) {
		var err error
		made, err = gw.CreateWorkstream(a.ctx, in)
		return "", err
	}, func(err error) string { return L("Could not start the workstream: %s", err) }, func() {
		go a.refreshList()
		a.Go(Route{Name: "session", SessionID: made.OrchestratorID})
	})
}

// workstreamLink is a Workstream Session's link to its Workstream's page,
// in its header. An archived Session the list leaves out has its tags in
// the Store once opened.
func (v *sessionView) workstreamLink(c *ui.Context, sel *sessions.Summary, s *session.Session) {
	a := v.a
	var id string
	switch {
	case sel != nil:
		id, _, _ = sessions.Workstream(sel.Tags)
	case s != nil:
		for _, t := range s.Settings().Tags {
			if t.Name == sessions.WorkstreamTag {
				id = t.Metadata["workstreamId"]
			}
		}
	}
	w := a.findWorkstream(id)
	if w == nil {
		return
	}
	k, t := a.kit, a.kit.T
	label := L("Workstream: %s", trimOr(w.Title, w.ID))
	b := ui.ButtonBase(c).Role(ui.RoleLink).Label(label).Tooltip(label).Height(k.Px(k.Dense(22, 24))).PaddingX(k.Px(6)).Gap(k.Px(4)).Radius(k.Px(6)).
		Shrink(1).MinWidth(0).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.Muted)
		color = t.Foreground
	}
	b.Children(func() {
		k.Icon(c, "git-fork", 12, color)
		k.Text(c, label, k.Dense(11, 12), 16).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
	})
	if b.Clicked() {
		a.Go(Route{Name: "workstreams", Workstream: id})
	}
}
