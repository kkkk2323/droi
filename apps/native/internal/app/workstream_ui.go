package app

import (
	"encoding/json"
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// The side panel's tabs.
const (
	panelAgents = "agents"
	panelFiles  = "files"
)

// workstreamUI is what the window keeps of one Workstream's page between
// runs: the agent tabs open beside the orchestrator's, the one shown ("" for
// the orchestrator's), and the side panel.
type workstreamUI struct {
	Tabs   []string `json:"tabs,omitempty"`
	Active string   `json:"active,omitempty"`
	// Panel is the side panel's tab, Agents when empty; Closed is the
	// panel put away, which the orchestrator's page opens by default.
	Panel  string `json:"panel,omitempty"`
	Closed bool   `json:"closed,omitempty"`
	// ArchivedOpen unfolds the panel's archived agents; Folded are the other
	// groups the user folded.
	ArchivedOpen bool     `json:"archivedOpen,omitempty"`
	Folded       []string `json:"folded,omitempty"`
}

// folded is whether the panel keeps group folded: the archived agents until
// opened, any other group once folded.
func (u workstreamUI) folded(group string) bool {
	if group == "archived" {
		return !u.ArchivedOpen
	}
	return slices.Contains(u.Folded, group)
}

// toggleFold folds group, or unfolds it.
func (u *workstreamUI) toggleFold(group string) {
	if group == "archived" {
		u.ArchivedOpen = !u.ArchivedOpen
		return
	}
	if i := slices.Index(u.Folded, group); i >= 0 {
		u.Folded = slices.Delete(u.Folded, i, i+1)
		return
	}
	u.Folded = append(u.Folded, group)
}

func (u workstreamUI) panel() string {
	if u.Panel == panelFiles {
		return panelFiles
	}
	return panelAgents
}

// wsState is the saved state of Workstream id's page.
func (a *App) wsState(id string) workstreamUI {
	s := &a.workstreams
	if raw := prefs.WorkstreamUI.Get(a.prefs); s.ui == nil || raw != s.uiRaw {
		s.ui = map[string]workstreamUI{}
		_ = json.Unmarshal([]byte(raw), &s.ui)
		if s.ui == nil {
			s.ui = map[string]workstreamUI{}
		}
		s.uiRaw = raw
	}
	return s.ui[id]
}

// setWsState changes the saved state of Workstream id's page.
func (a *App) setWsState(id string, change func(*workstreamUI)) {
	s := &a.workstreams
	u := a.wsState(id)
	change(&u)
	if u.Active != "" && !slices.Contains(u.Tabs, u.Active) {
		u.Active = ""
	}
	if len(u.Tabs) == 0 && u.Panel == "" && !u.Closed && !u.ArchivedOpen && len(u.Folded) == 0 {
		delete(s.ui, id)
	} else {
		s.ui[id] = u
	}
	b, _ := json.Marshal(s.ui)
	s.uiRaw = string(b)
	prefs.WorkstreamUI.Set(a.prefs, s.uiRaw)
}

// sessionWorkstream is the Workstream Session id belongs to, its role in it
// and, for an agent, its orchestrator, as the list tags it.
func (a *App) sessionWorkstream(id string) (wsID, role, orchestrator string) {
	a.mu.Lock()
	sel := findSummary(a.listed, id)
	a.mu.Unlock()
	if sel == nil {
		return "", "", ""
	}
	return sessions.Workstream(sel.Tags)
}

// orchestratorSession is the newest Session of Workstream wsID's
// orchestrator, "" when neither the list nor the records name one.
func (a *App) orchestratorSession(wsID string) string {
	for _, m := range a.workstreams.members {
		if id, role, _ := sessions.Workstream(m.Tags); id == wsID && role == sessions.WorkstreamOrchestrator {
			return a.resolve(m.SessionID)
		}
	}
	if w := a.findWorkstream(wsID); w != nil && w.OrchestratorID != "" {
		return a.resolve(w.OrchestratorID)
	}
	return ""
}

// openAgent shows agent's Session in a tab of its Workstream's page, the
// orchestrator's, opening the tab when there is none.
func (a *App) openAgent(wsID, agent string) {
	a.workstreams.paneDrawer = false
	a.setWsState(wsID, func(u *workstreamUI) {
		if !slices.Contains(u.Tabs, agent) {
			u.Tabs = append(u.Tabs, agent)
		}
		u.Active = agent
	})
	a.showAgent(agent)
	if orch := a.orchestratorSession(wsID); orch != "" && (a.route.Name != "session" || a.route.SessionID != orch) {
		a.Go(Route{Name: "session", SessionID: orch})
	}
}

// showAgent loads agent's Session for the tab that shows it, and marks it
// seen.
func (a *App) showAgent(agent string) {
	id := a.resolve(agent)
	delete(a.unread, id)
	a.viewed(id)
	a.view(id).Open()
	a.remember(id)
}

// showTab makes the tab of agent the shown one, "" the orchestrator's.
func (a *App) showTab(wsID, agent string) {
	a.setWsState(wsID, func(u *workstreamUI) { u.Active = agent })
	if agent != "" {
		a.showAgent(agent)
		return
	}
	if orch := a.orchestratorSession(wsID); orch != "" {
		a.showAgent(orch)
	}
}

// closeTabs closes the agent tabs that drop picks, and shows the tab beside
// the shown one if it went, the orchestrator's when none is left.
func (a *App) closeTabs(wsID string, drop func(i int, tab string) bool) {
	var next string
	a.setWsState(wsID, func(u *workstreamUI) {
		at := slices.Index(u.Tabs, u.Active)
		var kept []string
		for i, tab := range u.Tabs {
			if !drop(i, tab) {
				kept = append(kept, tab)
			}
		}
		if u.Active != "" && !slices.Contains(kept, u.Active) {
			switch {
			case len(kept) == 0:
				u.Active = ""
			case at < len(kept):
				u.Active = kept[at]
			default:
				u.Active = kept[len(kept)-1]
			}
		}
		u.Tabs = kept
		next = u.Active
	})
	a.showTab(wsID, next)
}

// inWorkstream is route r, but for an agent's Session: that is a tab of its
// Workstream's page, the orchestrator's, which opens the tab.
func (a *App) inWorkstream(r Route) Route {
	if r.Name != "session" {
		return r
	}
	wsID, role, _ := a.sessionWorkstream(r.SessionID)
	if wsID == "" || role != sessions.WorkstreamAgent {
		return r
	}
	orch := a.orchestratorSession(wsID)
	if orch == "" || orch == r.SessionID {
		return r
	}
	tab := a.agentTabID(wsID, r.SessionID)
	a.setWsState(wsID, func(u *workstreamUI) {
		if !slices.Contains(u.Tabs, tab) {
			u.Tabs = append(u.Tabs, tab)
		}
		u.Active = tab
	})
	a.showAgent(tab)
	r.SessionID = orch
	return r
}

// agentTabID is the id that names the tab of the agent whose Session is id:
// the Host's record of it, which a handoff may have left on an older Session.
func (a *App) agentTabID(wsID, id string) string {
	if w := a.findWorkstream(wsID); w != nil {
		for _, ag := range w.Agents {
			if ag.SessionID == id || a.resolve(ag.SessionID) == id {
				return ag.SessionID
			}
		}
	}
	return id
}

// sessionPage is Session id's page: a Workstream's when it is the
// orchestrator of one, else the Session alone.
func (a *App) sessionPage(c *ui.Context, id string, listed []sessions.Summary) {
	sel := findSummary(listed, id)
	if sel != nil && sessions.IsWorkstreamOrchestrator(sel.Tags) {
		a.workstreamPage(c, *sel, listed)
		return
	}
	v := a.view(id)
	v.ws, v.chrome = nil, nil
	v.build(c, a, sel, listed)
}

// workstreamPage is a Workstream's page: the orchestrator's Session, or the
// Session of an agent whose tab is shown, under the tab bar, with the side
// panel beside them.
func (a *App) workstreamPage(c *ui.Context, orch sessions.Summary, listed []sessions.Summary) {
	wsID, _, _ := sessions.Workstream(orch.Tags)
	st := a.wsState(wsID)
	active, agent := orch.SessionID, false
	if st.Active != "" && slices.Contains(st.Tabs, st.Active) {
		active, agent = a.resolve(st.Active), true
	}
	ctx := &wsContext{id: wsID, orch: orch, w: a.findWorkstream(wsID), st: st, agentShown: agent}
	v := a.view(active)
	v.ws = ctx
	v.chrome = func(c *ui.Context, s *session.Session, loaded bool, workspace string, nav subagentNav) {
		a.workstreamTabs(c, ctx, v, s, loaded, workspace, nav)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			v.build(c, a, findSummary(listed, active), listed)
		})
		if !st.Closed && !a.narrow {
			a.workstreamPane(c, ctx, listed, false)
		}
	})
	if a.narrow {
		a.panelDrawer(c, ctx, listed)
	} else {
		a.workstreams.paneDrawer = false
	}
}

// showPanel brings the side panel into view, on tab when it is not "": beside
// the page, or over it in a narrow window.
func (a *App) showPanel(wsID, tab string) {
	a.setWsState(wsID, func(u *workstreamUI) {
		u.Closed = false
		if tab != "" {
			u.Panel = tab
		}
	})
	if a.narrow {
		a.workstreams.paneDrawer = true
	}
}

// panelDrawer is the side panel from the right edge over a dimmed page, as
// the sidebar's drawer is from the left.
func (a *App) panelDrawer(c *ui.Context, ctx *wsContext, listed []sessions.Summary) {
	s := &a.workstreams
	if !s.paneDrawer {
		return
	}
	k, t := a.kit, a.kit.T
	w, _ := c.Size()
	ui.DialogBase(c, &s.paneDrawer, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Role(ui.RoleDialog).Label(L("Workstream panel")).Absolute().Right(0).Top(0).Bottom(0).Width(min(w*0.9, k.Px(360))).
			Background(t.Background).TextColor(t.Foreground).Clip().Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).Fill().Children(func() { a.workstreamPane(c, ctx, listed, true) })
	})
}

// wsContext is the Workstream the page being drawn belongs to.
type wsContext struct {
	id   string
	orch sessions.Summary
	// w are the Host's records of it, nil until they are read.
	w  *gateway.Workstream
	st workstreamUI
	// agentShown: the page shows an agent's Session, not the orchestrator's.
	agentShown bool
}

// title is the Workstream's name: its orchestrator's Session title, which
// the sidebar shows, renamed from either.
func (x *wsContext) title() string {
	if x.orch.Title != "" {
		return x.orch.Title
	}
	if x.w != nil && x.w.Title != "" {
		return x.w.Title
	}
	return x.id
}

// agentTitle is what an agent's tab and rows call it: the Host's record of
// it, else its listed Session's title.
func (a *App) agentTitle(ctx *wsContext, id string, listed []sessions.Summary) string {
	if ctx.w != nil {
		for _, ag := range ctx.w.Agents {
			if ag.SessionID == id && ag.Title != "" {
				return ag.Title
			}
		}
	}
	if s := findSummary(listed, a.resolve(id)); s != nil && s.Title != "" {
		return s.Title
	}
	return L("Agent")
}

// workstreamTabs is the tab bar, in the place of a Session's header: the
// orchestrator's tab that stays, the agents' tabs, and what the shown
// Session's header has at its right, with the Workstream's settings and the
// side panel's switch.
func (a *App) workstreamTabs(c *ui.Context, ctx *wsContext, v *sessionView, s *session.Session, loaded bool, workspace string, nav subagentNav) {
	k, t := a.kit, a.kit.T
	leading := float32(0)
	if !a.sidebarShown() {
		leading = 100
		if !a.cfg.InsetTop {
			leading = 32
		}
	}
	padX := k.Px(12)
	if a.narrow {
		padX = k.Px(8)
	}
	listed := a.listedNow()
	ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(padX).Gap(k.Px(4)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().AlignItems(ui.Center).Children(func() {
		if a.narrow {
			a.openSessionsButton(c)
		} else {
			sp := ui.Box(c).Shrink(0)
			sp.Width(sp.Animate("width", leading, 200*time.Millisecond))
		}
		ui.Row(c).Role(ui.RoleTabList).Label(L("Workstream tabs")).Grow(1).MinWidth(0).Gap(k.Px(4)).AlignItems(ui.Center).Children(func() {
			a.orchestratorTab(c, ctx)
			for i, tab := range ctx.st.Tabs {
				a.agentTab(c, ctx, i, tab, listed)
			}
		})
		ui.Row(c).Gap(k.Px(2)).Shrink(0).Children(func() {
			v.jobsMenu(c)
			v.subagentMenu(c, nav)
			v.gitButton(c, s, loaded)
			v.openInButton(c, workspace)
			if k.IconButton(c, "settings", L("Workstream settings"), 32).Clicked() {
				a.openWorkstreamSettings(ctx.id)
			}
			if ctx.st.Closed || a.narrow {
				if k.IconButton(c, "panel-right-open", L("Show panel"), 32).Clicked() {
					a.showPanel(ctx.id, "")
				}
			}
		})
	})
}

// orchestratorTab is the tab of the Workstream's orchestrator: its symbol and
// name, in the tint that marks it, and the menu of the Workstream.
func (a *App) orchestratorTab(c *ui.Context, ctx *wsContext) {
	k, t := a.kit, a.kit.T
	title := ctx.title()
	selected := !ctx.agentShown
	tab := ui.Row(c.Key("tab:orchestrator")).Height(k.Px(30)).Shrink(1).MinWidth(k.Px(96)).MaxWidth(k.Px(260)).Radius(k.Px(8)).AlignItems(ui.Center)
	switch {
	case selected:
		tab.Background(t.Info.Alpha(0.14))
	case tab.Hovered():
		tab.Background(t.Info.Alpha(0.07))
	}
	menuOpen := &a.workstreams.tabMenuOpen
	tab.Children(func() {
		b := ui.ButtonBase(c).Role(ui.RoleTab).Label(title).Tooltip(title).Grow(1).MinWidth(0).FillHeight().PaddingX(k.Px(10)).Gap(k.Px(6)).
			Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer)
		kit.Selected(b, selected)
		b.Children(func() {
			k.Icon(c, ctx.symbol(), 14, t.Info)
			k.Text(c, title, k.Dense(12, 13), k.Dense(18, 19.5)).FontWeight(500).TextColor(t.Info).SingleLine().Shrink(1).MinWidth(0)
			if direct, hint := a.orchestratorDirect(ctx.w); direct {
				a.directBadge(c, hint)
			}
		})
		if b.Clicked() && !selected {
			a.showTab(ctx.id, "")
		}
		more := k.IconButton(c, "ellipsis", L("Workstream menu"), 24).Expanded(*menuOpen).Margin(0, k.Px(3), 0, 0)
		if more.Clicked() {
			*menuOpen = !*menuOpen
		}
		k.MenuPopup(c, more, menuOpen, false, L("Workstream menu"), func() {
			if k.MenuItem(c, menuOpen, "settings", L("Workstream settings")).Clicked() {
				a.openWorkstreamSettings(ctx.id)
			}
			if !a.following() && a.cfg.OpenPath != nil && ctx.w != nil && ctx.w.SharedFolder != "" &&
				k.MenuItem(c, menuOpen, "folder-open", L("Open shared folder")).Clicked() {
				dir := ctx.w.SharedFolder
				go func() { _ = a.cfg.OpenPath(dir) }()
			}
			if k.MenuItem(c, menuOpen, "archive", L("Archive Workstream…")).Clicked() {
				a.workstreams.archiveFor = ctx.id
			}
		})
	})
}

// agentTab is an agent's tab: its name and a button that closes it; the menu
// of the right button closes this tab, those after it, or all.
func (a *App) agentTab(c *ui.Context, ctx *wsContext, i int, id string, listed []sessions.Summary) {
	k, t := a.kit, a.kit.T
	title := a.agentTitle(ctx, id, listed)
	selected := ctx.agentShown && ctx.st.Active == id
	tab := ui.Row(c.Key("tab:" + id)).Height(k.Px(30)).Shrink(1).MinWidth(k.Px(96)).MaxWidth(k.Px(180)).Radius(k.Px(8)).AlignItems(ui.Center)
	switch {
	case selected:
		tab.Background(t.SidebarAccent)
	case tab.Hovered():
		tab.Background(t.SidebarAccent.Alpha(0.5))
	}
	last := i == len(ctx.st.Tabs)-1
	tab.Children(func() {
		b := ui.ButtonBase(c).Role(ui.RoleTab).Label(title).Tooltip(ctx.title() + " › " + title).Grow(1).MinWidth(0).FillHeight().PaddingX(k.Px(8)).Gap(k.Px(6)).
			Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer)
		kit.Selected(b, selected)
		b.Children(func() {
			k.Icon(c, "bot", 14, t.MutedForeground)
			k.Text(c, title, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.Foreground).SingleLine().Shrink(1).MinWidth(0)
		})
		if b.Clicked() && !selected {
			a.showTab(ctx.id, id)
		}
		b.ContextMenu(func(m *ui.Menu) {
			if m.Item(L("Close")).Chosen() {
				a.closeTabs(ctx.id, func(_ int, tab string) bool { return tab == id })
			}
			if m.Item(L("Close tabs to the right")).Disabled(last).Chosen() {
				a.closeTabs(ctx.id, func(j int, _ string) bool { return j > i })
			}
			if m.Item(L("Close all")).Chosen() {
				a.closeTabs(ctx.id, func(int, string) bool { return true })
			}
		})
		x := k.IconButton(c, "x", L("Close %s", title), 24).Margin(0, k.Px(3), 0, 0)
		if x.Clicked() {
			a.closeTabs(ctx.id, func(_ int, tab string) bool { return tab == id })
		}
	})
}

// symbol is the Workstream's icon: its orchestrator's tag names it.
func (x *wsContext) symbol() string { return sessions.WorkstreamSymbol(x.orch.Tags) }

// listedNow is the Session list the frame shows.
func (a *App) listedNow() []sessions.Summary {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listed
}

const (
	paneMinWidth = 300
	paneMaxWidth = 640
)

// paneWidth is the side panel's width: the one its edge is being dragged
// to, else the one saved.
func (a *App) paneWidth() float32 {
	w := a.workstreams.paneDrag
	if w == 0 {
		w = float32(prefs.WorkstreamPane.Get(a.prefs))
	}
	return a.kit.Px(min(max(w, paneMinWidth), paneMaxWidth))
}

// paneEdge is the line between the page and the side panel, which resizes
// it as the sidebar's edge resizes the sidebar.
func (a *App) paneEdge(c *ui.Context, w float32) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	grip := k.Px(6)
	px := w * 16 / k.Rem
	edge := ui.Box(c).Absolute().Left(-grip/2).Top(0).Bottom(0).Width(grip).Cursor(ui.CursorResizeEW).
		Role(ui.RoleSplitter).Label(L("Resize panel")).Focusable().FocusRing(false).
		Range(paneMinWidth, paneMaxWidth, math.Round(float64(px))).Step(16)
	dx, _, held := edge.Dragged()
	switch {
	case held && dx != 0:
		if s.paneDrag == 0 {
			s.paneDrag = px
		}
		// The panel is on the right: dragging left widens it.
		s.paneDrag -= dx * 16 / k.Rem
	case !held && s.paneDrag != 0:
		a.savePaneWidth(s.paneDrag)
		s.paneDrag = 0
	}
	if edge.Shortcut(0, ui.KeyLeft) {
		a.savePaneWidth(px + 16)
	}
	if edge.Shortcut(0, ui.KeyRight) {
		a.savePaneWidth(px - 16)
	}
	if edge.DoubleClicked() {
		s.paneDrag = 0
		a.prefs.Remove(prefs.WorkstreamPane.Key)
	}
	if held || edge.Hovered() || edge.FocusVisible() {
		edge.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + grip/2 - 0.5, Y: r.Y, W: 2, H: r.H}, t.Tint, 0)
		})
	}
}

func (a *App) savePaneWidth(px float32) {
	px = min(max(px, paneMinWidth), paneMaxWidth)
	prefs.WorkstreamPane.Set(a.prefs, math.Round(float64(px)))
}

// working counts the agents of a Workstream at work, and waiting those that
// wait on the user.
func (a *App) workstreamCounts(w *gateway.Workstream) (working, waiting int) {
	if w == nil {
		return 0, 0
	}
	for _, ag := range w.Agents {
		switch status, _ := a.agentStatus(ag); status {
		case "working":
			working++
		case "waiting":
			waiting++
		}
	}
	return working, waiting
}
