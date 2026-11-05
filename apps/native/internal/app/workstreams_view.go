package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// report: the window reads them again now and then, and whenever one of the
// Workstream Sessions it lists starts, stops or waits.
const workstreamsRefresh = 30 * time.Second

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
	// pending are the prompts the Daemon counts for its opened Sessions,
	// read with the list: a Session this window has not loaded shows no
	// working state while it waits on a question.
	pending map[string]pendingPrompts

	// members are the listed Workstream Sessions, found again when the list
	// changes (membersRev); key names them and what each is doing, to read
	// the Workstreams again when that changes.
	members    []sessions.Summary
	membersRev uint64
	membersSet bool
	key        string
	// busy is this frame's activity of the Sessions; waiting counts the
	// agents, not archived, that wait on the user.
	busy    map[string]activity.Activity
	waiting int

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
	// gitRepos are the folders looked up for the form's note.
	gitRepos map[string]bool
}

type pendingPrompts struct{ permissions, questions int }

// workstreamForm is what the New workstream form's fields hold. The models
// and efforts are "" for the defaults: the Daemon's for the orchestrator,
// the orchestrator's for the agents.
type workstreamForm struct {
	name, workspace, goal   string
	model, effort           string
	agentModel, agentEffort string
	modelOpen, agentOpen    bool
	picker, agentPicker     pickerState
	// err is what is wrong with the field errOn.
	err, errOn string
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
		s.form = workstreamForm{}
	}
	a.loadWorkstreams(true)
}

// loadWorkstreams reads the Workstreams from the Gateway of the computer the
// window shows, with the prompts its Daemon counts.
func (a *App) loadWorkstreams(force bool) {
	s := &a.workstreams
	gw := a.cfg.Gateway
	if gw == nil {
		return
	}
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
		var pending map[string]pendingPrompts
		if err == nil {
			pending = a.readPending()
		}
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
				s.older, s.listErr, s.list, s.pending = false, "", list, pending
			}
			if s.listAgain {
				s.listAgain = false
				a.loadWorkstreams(true)
			}
		})
	}()
}

// readPending is the prompts each opened Session of the Daemon waits on.
func (a *App) readPending() map[string]pendingPrompts {
	if a.ctl == nil {
		return nil
	}
	cl, err := a.ctl.Client()
	if err != nil {
		return nil
	}
	res, err := cl.ListOpenedSessions(a.ctx, protocol.ListOpenedSessionsParams{})
	if err != nil {
		return nil
	}
	out := map[string]pendingPrompts{}
	for _, o := range res.Sessions {
		var p pendingPrompts
		if o.PendingPermissionRequestCount != nil {
			p.permissions = int(*o.PendingPermissionRequestCount)
		}
		if o.PendingAskUserRequestCount != nil {
			p.questions = int(*o.PendingAskUserRequestCount)
		}
		if p != (pendingPrompts{}) {
			out[o.SessionID] = p
		}
	}
	return out
}

// noteWorkstreams follows the listed Workstream Sessions: it counts the
// agents waiting on the user, and reads the Workstreams again when one of
// them starts, stops, waits or is archived, or a new one is listed.
func (a *App) noteWorkstreams(listed []sessions.Summary, busy map[string]activity.Activity) {
	s := &a.workstreams
	s.busy = busy
	if !s.membersSet || s.membersRev != a.shownRev {
		var members []sessions.Summary
		for _, x := range listed {
			if id, _, _ := sessions.Workstream(x.Tags); id != "" {
				members = append(members, x)
			}
		}
		s.members, s.membersRev, s.membersSet = members, a.shownRev, true
	}
	var key strings.Builder
	waiting := 0
	for _, x := range s.members {
		how := a.waitsOn(x.SessionID)
		if sessions.IsWorkstreamAgent(x.Tags) && x.ArchivedAt == "" && how != "" {
			waiting++
		}
		fmt.Fprintf(&key, "%s:%t:%s:%s;", x.SessionID, x.ArchivedAt != "", busy[x.SessionID], how)
	}
	s.waiting = waiting
	if k := key.String(); k != s.key {
		s.key = k
		a.loadWorkstreams(true)
	}
}

// waitsOn is how the Session id waits on the user, "" when it does not:
// a permission request, or a question.
func (a *App) waitsOn(id string) string {
	s := &a.workstreams
	doing := s.busy[id]
	if a.ctl != nil {
		if x := a.ctl.Store().Session(id); x != nil && x.LoadState() == session.Loaded {
			if doing != activity.NeedsInput {
				return ""
			}
			if len(a.ctl.PendingAskUsers(id)) > 0 && len(a.ctl.PendingPermissions(id)) == 0 {
				return waitsQuestion
			}
			return waitsApproval
		}
	}
	p := s.pending[id]
	switch {
	case doing == activity.NeedsInput && p.questions > 0 && p.permissions == 0:
		return waitsQuestion
	case doing == activity.NeedsInput:
		return waitsApproval
	case doing == "" && p.questions > 0:
		return waitsQuestion
	}
	return ""
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
		k.Text(c, s.notice, 13, 20).TextColor(t.Foreground).Grow(1).MinWidth(0)
	})
}

// workstreamsPage lists the Workstreams, shows one, or starts one.
func (a *App) workstreamsPage(c *ui.Context) {
	k := a.kit
	s := &a.workstreams
	a.loadWorkstreams(false)
	ui.Column(c).Role(ui.RoleGroup).Label(L("Workstreams")).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(k.Px(720)).AlignSelf(ui.Center).Gap(k.Px(24)).Padding(k.Px(4), k.Px(32), k.Px(48), k.Px(32)).Children(func() {
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
	k.Text(c, L("%s runs an older Droi without workstreams. Update Droi there.", name), 13, 20).Role(ui.RoleStatus).
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
				k.Text(c, strconv.Itoa(len(active)), 13, 20).TextColor(t.MutedForeground).FontFeatures("tnum")
			}
		}, func() {
			if len(s.list) > 0 && a.smallButton(c, kit.Primary, L("New workstream"), "plus", false).Clicked() {
				a.Go(Route{Name: "workstreams", Tab: newWorkstreamTab})
			}
		})
		k.Text(c, L("An orchestrator session splits a long goal among agents. Each agent keeps its own session and worktree, and reports back to the orchestrator."), 13, 20).
			TextColor(t.MutedForeground)
	})
	a.workstreamNotice(c)
	switch {
	case s.list == nil && s.listErr != "":
		a.loadFailed(c, s.listErr, func() { a.loadWorkstreams(true) })
	case s.list == nil:
		a.skeletonRows(c, L("Loading workstreams"), 3, 56)
	case len(s.list) == 0:
		a.noWorkstreams(c)
	default:
		if s.listErr != "" {
			k.Text(c, s.listErr, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
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
	b := ui.ButtonBase(c).Label(title).Expanded(*open).AlignSelf(ui.Start).Height(k.Px(28)).Gap(k.Px(6)).PaddingX(k.Px(6)).
		Margin(0, 0, 0, -k.Px(6)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.Muted.Alpha(0.5))
		color = t.Foreground
	}
	b.Children(func() {
		k.Text(c, title, 13, 20).Role(ui.RoleHeading).FontWeight(600).TextColor(t.Foreground)
		k.Text(c, strconv.Itoa(n), 13, 20).TextColor(t.MutedForeground).FontFeatures("tnum")
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
	ui.Column(c).Role(ui.RoleGroup).Label(L("No workstreams yet")).AlignItems(ui.Center).Gap(k.Px(6)).Padding(k.Px(40), k.Px(24)).
		Radius(k.Px(12)).Border(1, t.Border).Children(func() {
		ui.Box(c).Size(k.Px(36), k.Px(36)).Radius(k.Px(10)).Background(t.Muted).Center().Margin(0, 0, k.Px(6), 0).Children(func() {
			k.Icon(c, "git-fork", 18, t.MutedForeground)
		})
		k.Text(c, L("No workstreams yet"), 14, 20).FontWeight(500)
		k.Text(c, L("Start one for work that spans days and splits into parts, such as a migration done in stages."), 13, 20).
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
		if ag.ArchivedAt == "" && a.waitsOn(ag.SessionID) != "" {
			n++
		}
	}
	return n
}

// waitingMark is a count of agents waiting on the user.
func (a *App) waitingMark(c *ui.Context, n int, size, line float32) ui.Element {
	k, t := a.kit, a.kit.T
	return ui.Row(c).Role(ui.RoleStatus).Label(L("%d waiting on you", n)).Tooltip(L("%d waiting on you", n)).Gap(k.Px(4)).AlignItems(ui.Center).Shrink(0).Children(func() {
		k.Icon(c, "circle-alert", 12, t.Attention)
		k.Text(c, itoa(n), size, line).TextColor(t.Attention).FontFeatures("tnum")
	})
}

func (a *App) workstreamRow(c *ui.Context, w gateway.Workstream) {
	k, t := a.kit, a.kit.T
	title := trimOr(w.Title, w.ID)
	b := ui.ButtonBase(c.Key(w.ID)).Transition(rowMotion).Role(ui.RoleListItem).Label(title).Height(k.Px(56)).PaddingX(k.Px(12)).Gap(k.Px(12)).
		Justify(ui.Start).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Cursor(ui.CursorPointer)
	hover := b.Hovered()
	if hover {
		b.Background(t.Muted.Alpha(0.5))
	}
	b.Children(func() {
		k.Icon(c, "git-fork", 14, t.MutedForeground)
		ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
			k.Text(c, title, 14, 20).FontWeight(500).SingleLine()
			k.Text(c, w.Workspace, 12, 16).TextColor(t.MutedForeground).SingleLine()
		})
		if n := a.waitingAgents(w); n > 0 {
			a.waitingMark(c, n, 12, 16)
		}
		k.Text(c, agentsCount(w), 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(0)
		chevron := k.Icon(c, "chevron-right", 14, t.MutedForeground)
		if !hover {
			chevron.Opacity(0)
		}
	})
	if b.Clicked() {
		a.Go(Route{Name: "workstreams", Workstream: w.ID})
	}
}

// missingWorkstream is the page of a Workstream the list does not have:
// not loaded yet, or gone.
func (a *App) missingWorkstream(c *ui.Context) {
	s := &a.workstreams
	if s.list == nil && s.listErr == "" {
		a.automationHeader(c, []crumb{allWorkstreams(), {label: "…"}}, "", nil, nil)
		a.skeletonRows(c, L("Loading workstreams"), 2, 40)
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
// "working" or "idle", and how a waiting one waits.
func (a *App) agentStatus(ag gateway.WorkstreamAgent) (string, string) {
	if ag.ArchivedAt != "" {
		return "archived", ""
	}
	if how := a.waitsOn(ag.SessionID); how != "" {
		return "waiting", how
	}
	if a.workstreams.busy[ag.SessionID] != "" {
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
			a.Go(Route{Name: "session", SessionID: w.OrchestratorID})
		}
		if !a.following() && a.cfg.OpenPath != nil && w.SharedFolder != "" &&
			a.smallButton(c, kit.Outline, L("Open shared folder"), "folder-open", false).Clicked() {
			dir := w.SharedFolder
			go func() { _ = a.cfg.OpenPath(dir) }()
		}
		if archived {
			return
		}
		more := k.IconButton(c, "ellipsis", L("More actions"), 32).Expanded(s.menuOpen).Disabled(busy)
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
		a.fact(c, L("Workspace"), func() { k.Text(c, w.Workspace, 12, 20).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0) })
		a.fact(c, L("Shared folder"), func() { k.Text(c, w.SharedFolder, 12, 20).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0) })
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
		k.Text(c, L("No agents yet. The orchestrator starts them as it splits the work."), 13, 20).TextColor(t.MutedForeground)
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
					a.agentRow(c, ag, g.status, how[ag.SessionID])
				}
			})
		})
	}
	if list := groups["archived"]; len(list) > 0 {
		ui.Column(c.Key("group:archived")).Gap(k.Px(4)).Children(func() {
			a.foldToggle(c, L("Archived"), len(list), &s.archivedAgentsOpen)
			if s.archivedAgentsOpen {
				ui.Column(c).Role(ui.RoleList).Label(L("Archived")).Children(func() {
					for _, ag := range list {
						a.agentRow(c, ag, "archived", "")
					}
				})
			}
		})
	}
}

// reportKinds name an agent's reports.
var reportKinds = map[string]func() string{
	"done":    func() string { return L("Done") },
	"blocked": func() string { return L("Blocked") },
	"update":  func() string { return L("Update") },
}

// agentRow opens the agent's Session: its title, what it is doing, its
// branch, and its last report and when.
func (a *App) agentRow(c *ui.Context, ag gateway.WorkstreamAgent, status, waits string) {
	k, t := a.kit, a.kit.T
	title := trimOr(ag.Title, ag.SessionID)
	b := ui.ButtonBase(c.Key(ag.SessionID)).Transition(rowMotion).Role(ui.RoleListItem).Label(title).Height(k.Px(48)).PaddingX(k.Px(12)).Gap(k.Px(10)).
		Justify(ui.Start).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Cursor(ui.CursorPointer)
	hover := b.Hovered()
	if hover {
		b.Background(t.Muted.Alpha(0.5))
	}
	label, color := "", t.MutedForeground
	switch status {
	case "waiting":
		label, color = L("Needs approval"), t.AttentionForeground
		if waits == waitsQuestion {
			label = L("Has a question")
		}
	case "working":
		label = L("Working")
	case "idle":
		label = L("Idle")
	default:
		label = L("Archived")
	}
	b.Children(func() {
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
			k.Text(c, title, 13, 20).FontWeight(500).SingleLine()
			ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
				k.Text(c, label, 12, 16).TextColor(color).SingleLine().Shrink(0)
				if ag.Branch != "" {
					ui.Row(c).Role(ui.RoleImage).Label(L("Worktree on %s", ag.Branch)).Gap(k.Px(4)).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
						k.Icon(c, "git-branch", 12, t.MutedForeground)
						k.Text(c, ag.Branch, 12, 16).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0)
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
			k.Text(c, kind, 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(0).Tooltip(r.Text)
		}
		chevron := k.Icon(c, "chevron-right", 14, t.MutedForeground)
		if !hover {
			chevron.Opacity(0)
		}
	})
	if b.Clicked() {
		a.Go(Route{Name: "session", SessionID: ag.SessionID})
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
			k.Text(c, L("Archive “%s”?", title), 15, 22).Role(ui.RoleHeading).FontWeight(600)
			k.Text(c, L("Its agents stop and are archived with the orchestrator. Worktrees without changes are deleted; ones with changes stay, and Droi lists them. The shared folder stays."), 13, 20).
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

func (a *App) archiveWorkstream(id string) {
	gw := a.cfg.Gateway
	if gw == nil {
		return
	}
	a.wsAct("archive", func() (string, error) {
		kept, err := gw.ArchiveWorkstream(a.ctx, id)
		if len(kept) == 0 {
			return L("Archived."), err
		}
		return L("Archived. Kept %d worktrees with changes: %s", len(kept), strings.Join(kept, ", ")), err
	}, func(err error) string { return L("Could not archive the workstream: %s", err) }, func() { go a.reloadList() })
}

// inGitRepo reports that dir is in a git repository's work tree on this
// computer: it or a folder above it has .git. The answers are kept.
func (a *App) inGitRepo(dir string) bool {
	s := &a.workstreams
	if ok, seen := s.gitRepos[dir]; seen {
		return ok
	}
	ok := false
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			ok = true
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	if s.gitRepos == nil {
		s.gitRepos = map[string]bool{}
	}
	s.gitRepos[dir] = ok
	return ok
}

// notGitRepo is whether to say that the folder is no git repository: only
// for a folder there is on this computer, which the window can look into.
func (a *App) notGitRepo(dir string) bool {
	if a.following() || dir == "" {
		return false
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return false
	}
	return !a.inGitRepo(dir)
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
	problem := func(field string) string {
		if f.errOn == field {
			return f.err
		}
		return ""
	}
	invalid := func(in ui.Element, field string) {
		if f.errOn == field {
			in.Border(1, t.Destructive.Alpha(0.6))
		}
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
	ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(20)).Children(func() {
		a.formField(c, L("Name"), "", problem("name"), func() {
			ui.Row(c).Children(func() {
				invalid(a.field(c, &f.name, L("Workstream name"), L("Payment service migration"), false, false), "name")
			})
		})
		a.formField(c, L("Workspace"), L("The repository the agents work in. Each agent gets its own worktree of it."), problem("workspace"), func() {
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				invalid(a.field(c, &f.workspace, L("Workstream workspace"), "/Users/you/project", true, false), "workspace")
				if !a.following() && a.smallButton(c, kit.Outline, L("Choose…"), "folder-open", false).Height(k.Px(36)).Label(L("Choose a workspace")).Clicked() {
					go func() {
						if dir := a.pickFolder(L("Choose a workspace")); dir != "" {
							a.cfg.Update(func() { f.workspace = dir })
						}
					}()
				}
			})
			if dir := strings.TrimSpace(f.workspace); a.notGitRepo(dir) {
				ui.Row(c).Role(ui.RoleStatus).Label(L("This folder is not a git repository, so agents share it instead of working in worktrees of their own.")).
					Gap(k.Px(6)).AlignItems(ui.Start).Children(func() {
					k.Icon(c, "info", 12, t.MutedForeground).Margin(k.Px(3), 0, 0, 0)
					k.Text(c, L("This folder is not a git repository, so agents share it instead of working in worktrees of their own."), 12, 18).
						TextColor(t.MutedForeground).Grow(1).MinWidth(0)
				})
			}
		})
		a.formField(c, L("Goal"), L("The first message to the orchestrator: what to achieve, and what it should know."), problem("goal"), func() {
			border := t.Border
			if f.errOn == "goal" {
				border = t.Destructive.Alpha(0.6)
			}
			ui.Row(c).Radius(k.Px(10)).Border(1, border).Background(t.Background).Children(func() {
				k.TextArea(c, &f.goal, L("Workstream goal"), L("Move the payment service from REST to gRPC, one endpoint at a time. Keep the old API until every client has switched."),
					kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: 14, Line: 20, MinLines: 5, Color: t.Foreground})
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
// shows, then opens its orchestrator's Session.
func (a *App) startWorkstream(shownModel string) {
	s := &a.workstreams
	f := &s.form
	f.err, f.errOn = "", ""
	name, workspace, goal := strings.TrimSpace(f.name), strings.TrimSpace(f.workspace), strings.TrimSpace(f.goal)
	switch {
	case name == "":
		f.err, f.errOn = L("Enter a name."), "name"
	case workspace == "":
		f.err, f.errOn = L("Choose a workspace."), "workspace"
	case goal == "":
		f.err, f.errOn = L("Enter the goal."), "goal"
	}
	gw := a.cfg.Gateway
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
// in its header.
func (v *sessionView) workstreamLink(c *ui.Context, sel *sessions.Summary) {
	a := v.a
	if sel == nil {
		return
	}
	id, _, _ := sessions.Workstream(sel.Tags)
	w := a.findWorkstream(id)
	if w == nil {
		return
	}
	k, t := a.kit, a.kit.T
	label := L("Workstream: %s", trimOr(w.Title, w.ID))
	b := ui.ButtonBase(c).Role(ui.RoleLink).Label(label).Tooltip(label).Height(k.Px(24)).PaddingX(k.Px(6)).Gap(k.Px(4)).Radius(k.Px(6)).
		Shrink(1).MinWidth(0).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		b.Background(t.Muted)
		color = t.Foreground
	}
	b.Children(func() {
		k.Icon(c, "git-fork", 12, color)
		k.Text(c, label, 12, 16).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
	})
	if b.Clicked() {
		a.Go(Route{Name: "workstreams", Workstream: id})
	}
}
