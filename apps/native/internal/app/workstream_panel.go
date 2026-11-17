package app

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
	"github.com/kkkk2323/droi/apps/native/pkg/subagents"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// startingFor is how long a row stands in for an agent whose Session the
// Host is still starting.
const startingFor = 60 * time.Second

// paneRow is a row of the side panel's list of agents.
type paneRow struct {
	// tab is the id of the agent's tab: the Host's record of its Session.
	tab   string
	id    string
	title string
	// group is "waiting", "working", "idle" or "archived".
	group string
	// activity says what it waits for or does, "" for an idle one.
	activity string
	at       time.Time
	unread   bool
	starting bool
	// index numbers the starting rows, which have no Session to name them.
	index int
	// direct marks an agent that can use only Direct tools; directHint says why.
	direct     bool
	directHint string
	// reportKind is the agent's last report kind ("done", "blocked", "update", "").
	reportKind string
	// subagents lists Task subagents invoked by this agent (or its continuations).
	subagents []subagentTreeRow
}

type subagentTreeRow struct {
	id     string
	title  string
	at     time.Time
	run    subagents.Run
	hasRun bool
}

// groupOrder is the groups of the agents list, as Factory orders them.
var groupOrder = []string{"waiting", "working", "idle"}

// paneRows are the agents of Workstream ctx in the groups of the list.
func (a *App) paneRows(ctx *wsContext, listed []sessions.Summary) []paneRow {
	if ctx.w == nil {
		return nil
	}
	var rows []paneRow
	for _, ag := range ctx.w.Agents {
		group, how := a.agentStatus(ag)
		id := a.resolve(ag.SessionID)
		reportKind := ""
		if ag.LastReport != nil {
			reportKind = ag.LastReport.Kind
		}
		row := paneRow{
			tab:        ag.SessionID,
			id:         id,
			title:      trimOr(ag.Title, L("Agent")),
			group:      group,
			unread:     a.sidebar.unread[id],
			reportKind: reportKind,
			subagents:  a.workstreamSubagents(ag.SessionID, listed),
		}
		if s := findSummary(listed, id); s != nil && s.Title != "" && ag.Title == "" {
			row.title = s.Title
		}
		row.activity = a.agentActivity(id, group, how)
		row.at = a.agentTime(ag, id, listed)
		row.direct, row.directHint = a.directMark(ag)
		rows = append(rows, row)
	}
	now := a.cfg.Now()
	for i, st := range ctx.w.Starting {
		at, err := time.Parse(time.RFC3339, st.StartedAt)
		if err == nil && now.Sub(at) > startingFor {
			continue
		}
		rows = append(rows, paneRow{title: trimOr(st.Title, L("New agent")), group: "working", activity: L("Starting"), at: at, starting: true, index: i})
	}
	return rows
}

// agentTime is when an agent last did something: the Host's note of it, else
// its Session's last use, else its start.
func (a *App) agentTime(ag gateway.WorkstreamAgent, id string, listed []sessions.Summary) time.Time {
	if t, err := time.Parse(time.RFC3339, ag.LastActivityAt); err == nil {
		return t
	}
	if s := findSummary(listed, id); s != nil {
		if at := sessions.UsedAt(*s, a.sidebar.times); at > 0 {
			return time.UnixMilli(at)
		}
	}
	t, _ := time.Parse(time.RFC3339, ag.CreatedAt)
	return t
}

// agentActivity is what the list says of an agent: what it waits for, or
// what it does; nothing for an idle one.
func (a *App) agentActivity(id, group, how string) string {
	switch group {
	case "waiting":
		return a.waitingText(id, how)
	case "working":
		if s := a.sessionOf(id); s != nil {
			if st := string(s.WorkingState()); st != "" && st != "idle" {
				return transcript.WorkingLabel(st)
			}
		}
		return L("Working")
	}
	return ""
}

// waitingText is why an agent waits on the user, as the list says it.
func (a *App) waitingText(id, how string) string {
	if how == waitsQuestion {
		n := a.workstreams.pending[id].questions
		if a.loaded(id) {
			n = len(a.ctl.PendingAskUsers(id))
		}
		switch n {
		case 0, 1:
			return L("Has 1 question for you")
		}
		return L("Has %d questions for you", n)
	}
	if a.loaded(id) {
		perms := a.ctl.PendingPermissions(id)
		if len(perms) > 0 && len(perms[0].ToolUses) > 0 {
			use := perms[0].ToolUses[0]
			if use.ToolUse.Name == transcript.SpecTool {
				return L("Needs spec approval")
			}
			if len(perms[0].ToolUses) > 1 {
				return L("Needs approval to run %s and %d more", use.ToolUse.Name, len(perms[0].ToolUses)-1)
			}
			return L("Needs approval to run %s", use.ToolUse.Name)
		}
	}
	return L("Needs your approval")
}

// workstreamPane is the side panel of a Workstream's page: the tabs that
// pick what it lists, the agents or the shared folder's files.
func (a *App) workstreamPane(c *ui.Context, ctx *wsContext, listed []sessions.Summary, drawer bool) {
	k, t := a.kit, a.kit.T
	w := a.paneWidth()
	panel := ctx.st.panel()
	col := ui.Column(c.Key("pane"))
	if drawer {
		col.Fill()
	} else {
		col.Role(ui.RoleGroup).Label(L("Workstream panel")).Width(w).Shrink(0).FillHeight().Background(t.Background).BorderWidth(0, 0, 0, 1).BorderColor(t.Border)
	}
	col.Children(func() {
		if !drawer {
			a.paneEdge(c, w)
		}
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).Gap(k.Px(4)).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {
			ui.Row(c).Role(ui.RoleTabList).Label(L("Panel tabs")).Grow(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
				for _, tab := range [][3]string{{panelAgents, "bot", L("Agents")}, {panelGoal, "target", L("Goal")}, {panelPRs, "git-pull-request", L("PRs")}, {panelFiles, "file", L("Files")}} {
					on := panel == tab[0]
					b := kit.Selected(ui.ButtonBase(c).Role(ui.RoleTab).Label(tab[2]), on).Height(k.Px(30)).PaddingX(k.Px(7)).Gap(k.Px(5)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
					color := t.MutedForeground
					switch {
					case on:
						b.Background(t.SidebarAccent)
						color = t.Foreground
					case b.Hovered():
						b.Background(t.SidebarAccent.Alpha(0.5))
						color = t.Foreground
					}
					b.Children(func() {
						k.Icon(c, tab[1], 13, color)
						k.Text(c, tab[2], k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(color).SingleLine()
					})
					if b.Clicked() {
						name := tab[0]
						a.setWsState(ctx.id, func(u *workstreamUI) { u.Panel = name })
					}
				}
			})
			if k.IconButton(c, "panel-right-close", L("Hide panel"), 32).Clicked() {
				if drawer {
					a.workstreams.paneDrawer = false
				} else {
					a.setWsState(ctx.id, func(u *workstreamUI) { u.Closed = true })
				}
			}
		})
		switch panel {
		case panelGoal:
			a.goalPanel(c, ctx)
			return
		case panelPRs:
			a.prsPanel(c, ctx)
			return
		case panelFiles:
			a.filesPanel(c, ctx)
			return
		}
		a.agentsPanel(c, ctx, listed)
	})
}

// agentsPanel is the Agents tab: a search box, and the agents in groups.
func (a *App) agentsPanel(c *ui.Context, ctx *wsContext, listed []sessions.Summary) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	a.loadWorkstreamPRs(ctx, false)
	rows := a.paneRows(ctx, listed)
	query := strings.ToLower(strings.TrimSpace(s.query))
	ui.Row(c).Shrink(0).Padding(k.Px(10), k.Px(12), k.Px(6), k.Px(12)).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Radius(k.Px(8)).Border(1, t.Input).Background(t.Background).AlignItems(ui.Center).PaddingX(k.Px(8)).Gap(k.Px(6)).Children(func() {
			k.Icon(c, "search", 14, t.MutedForeground)
			ui.TextInputBase(c, &s.query).Label(L("Search agents")).Placeholder(L("Search agents…")).Grow(1).MinWidth(0).Height(k.Px(k.Dense(30, 34))).
				FontSize(k.Px(k.Dense(12, 13))).TextColor(t.Foreground)
		})
	})
	var shown []paneRow
	for _, r := range rows {
		if query == "" || strings.Contains(strings.ToLower(r.title), query) || strings.Contains(strings.ToLower(r.id), query) {
			shown = append(shown, r)
		}
	}
	if len(shown) == 0 {
		title, text := L("No agents yet"), L("Agents show up here as the orchestrator delegates work.")
		switch {
		case ctx.w == nil && s.listErr == "":
			title, text = L("Loading agents…"), ""
		case ctx.w == nil:
			title, text = L("Could not load agents"), ""
		case query != "" && len(rows) > 0:
			title, text = L("No agents match your search"), ""
		}
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(k.Px(2)).PaddingX(k.Px(24)).Children(func() {
			k.Text(c, title, k.Dense(12, 13), k.Dense(18, 20)).FontWeight(500).Role(ui.RoleStatus)
			if text != "" {
				k.Text(c, text, k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground).TextAlign(ui.Center)
			}
		})
		return
	}
	var navOrder []string
	for _, g := range append(slices.Clone(groupOrder), "archived") {
		if ctx.st.folded(g) {
			continue
		}
		for _, r := range sortedGroupRows(shown, g) {
			if !r.starting && r.tab != "" {
				navOrder = append(navOrder, r.tab)
			}
		}
	}
	a.handleAgentsPanelKeys(c, ctx, navOrder)
	orchSubs := a.workstreamSubagents(ctx.orch.SessionID, listed)
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, k.Px(8), k.Px(12), k.Px(8)).Children(func() {
		for _, g := range groupOrder {
			a.paneGroup(c, ctx, g, shown, listed)
		}
		a.paneGroup(c, ctx, "archived", shown, listed)
		if len(orchSubs) > 0 && query == "" {
			a.orchSubagentsGroup(c, ctx, orchSubs)
		}
	})
}

// handleAgentsPanelKeys moves focus and previews the agent tab on ArrowUp,
// ArrowDown, Home and End when an agent row holds the keyboard focus.
func (a *App) handleAgentsPanelKeys(c *ui.Context, ctx *wsContext, navOrder []string) {
	if len(navOrder) == 0 || len(a.workstreams.agentHandles) == 0 {
		return
	}
	cur := -1
	for i, tab := range navOrder {
		if h, ok := a.workstreams.agentHandles[tab]; ok && h.Focused(c) {
			cur = i
			break
		}
	}
	if cur < 0 {
		return
	}
	next := -1
	switch {
	case c.Shortcut(0, ui.KeyDown):
		next = min(cur+1, len(navOrder)-1)
	case c.Shortcut(0, ui.KeyUp):
		next = max(cur-1, 0)
	case c.Shortcut(0, ui.KeyHome):
		next = 0
	case c.Shortcut(0, ui.KeyEnd):
		next = len(navOrder) - 1
	default:
		return
	}
	target := navOrder[next]
	if h, ok := a.workstreams.agentHandles[target]; ok {
		h.Focus(c)
	}
	a.workstreams.scrollToAgent = target
	if target != ctx.st.Active {
		// Keep keyboard focus on the panel list rather than jumping into the
		// newly opened agent's composer.
		a.view(target).focused = true
		drawerOpen := a.workstreams.paneDrawer
		a.openAgent(ctx.id, target)
		a.workstreams.paneDrawer = drawerOpen
	}
}

func sortedGroupRows(rows []paneRow, group string) []paneRow {
	var list []paneRow
	for _, r := range rows {
		if r.group == group {
			list = append(list, r)
		}
	}
	if group == "working" {
		slices.SortStableFunc(list, func(x, y paneRow) int { return cmp.Compare(strings.ToLower(x.title), strings.ToLower(y.title)) })
	} else {
		slices.SortStableFunc(list, func(x, y paneRow) int { return y.at.Compare(x.at) })
	}
	return list
}

// groupTitle is a group of the agents list as it is named.
func groupTitle(group string) string {
	switch group {
	case "waiting":
		return L("Waiting on you")
	case "working":
		return L("Working")
	case "idle":
		return L("Idle")
	}
	return L("Archived")
}

// paneGroup is a group of the agents list: its title with the count, and
// its rows. The archived agents stay folded until opened.
func (a *App) paneGroup(c *ui.Context, ctx *wsContext, group string, rows []paneRow, listed []sessions.Summary) {
	k, t := a.kit, a.kit.T
	list := sortedGroupRows(rows, group)
	if len(list) == 0 {
		return
	}
	title := groupTitle(group)
	open := !ctx.st.folded(group)
	ui.Column(c.Key("group:"+group)).Margin(k.Px(4), 0, 0, 0).Children(func() {
		head := ui.ButtonBase(c).Label(title).Expanded(open).AlignSelf(ui.Stretch).Height(k.Px(k.Dense(24, 28))).Gap(k.Px(6)).PaddingX(k.Px(6)).Radius(k.Px(8)).
			Justify(ui.Start).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		hovered := head.Hovered()
		if hovered {
			head.Background(t.Muted.Alpha(0.5))
			color = t.Foreground
		}
		head.Children(func() {
			switch group {
			case "waiting":
				k.Icon(c, "circle-alert", 12, t.AttentionForeground)
			case "working":
				k.Spinner(c, 12, t.Info)
			case "idle":
				k.Icon(c, "circle-check", 12, t.Success)
			case "archived":
				k.Icon(c, "archive", 12, t.MutedForeground)
			}
			k.Text(c, title, k.Dense(11, 12), k.Dense(16, 18)).Role(ui.RoleHeading).FontWeight(500).TextColor(color)
			k.Text(c, strconv.Itoa(len(list)), k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground).FontFeatures("tnum")
			rot := float32(0)
			if open {
				rot = 90
			}
			ch := k.Icon(c, "chevron-right", 12, color).Rotate(rot)
			if open && !hovered && !head.Focused() {
				ch.Opacity(0)
			}
		})
		if head.Clicked() {
			a.setWsState(ctx.id, func(u *workstreamUI) { u.toggleFold(group) })
		}
		if !open {
			return
		}
		ui.Column(c).Role(ui.RoleList).Label(title).Gap(1).Children(func() {
			for _, r := range list {
				a.paneAgentRow(c, ctx, r)
			}
		})
	})
}

// paneAgentRow is an agent's row: what it is doing or waiting for under its
// name, and when it last did anything unless it works or waits. A click
// shows its Session in a tab.
func (a *App) paneAgentRow(c *ui.Context, ctx *wsContext, r paneRow) {
	k, t := a.kit, a.kit.T
	selected := ctx.agentShown && !r.starting && ctx.st.Active == r.tab
	key := r.tab
	if r.starting {
		key = "starting:" + strconv.Itoa(r.index)
	}
	name := listName(r.title, groupTitle(r.group))
	if r.activity != "" {
		name = listName(r.title, r.activity)
	}
	prs := a.agentPRs(r.tab, r.id)
	ui.Column(c.Key(key)).Role(ui.RoleListItem).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(k.Px(4)).Children(func() {
			b := ui.ButtonBase(c).Label(name).Grow(1).MinWidth(0).MinHeight(k.Px(k.Dense(36, 40))).Padding(k.Px(5), k.Px(8), k.Px(5), k.Px(8)).Gap(k.Px(8)).
				Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer).Disabled(r.starting)
			if !r.starting && r.tab != "" {
				if a.workstreams.agentHandles == nil {
					a.workstreams.agentHandles = map[string]ui.Handle{}
				}
				h := a.workstreams.agentHandles[r.tab]
				b.Bind(&h)
				a.workstreams.agentHandles[r.tab] = h
				if a.workstreams.scrollToAgent == r.tab {
					b.ScrollIntoView()
					a.workstreams.scrollToAgent = ""
				}
			}
			kit.Selected(b, selected)
			switch {
			case selected:
				b.Background(t.SidebarAccent)
			case b.Hovered() && !r.starting:
				b.Background(t.Muted.Alpha(0.6))
			}
			b.Children(func() {
				ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
					switch {
					case r.group == "waiting":
						k.Icon(c, "circle-alert", 14, t.AttentionForeground).Role(ui.RoleImage).Label(L("Waiting for your response"))
					case r.group == "working":
						k.Spinner(c, 12, t.Info).Role(ui.RoleStatus).Label(L("Working"))
					case r.group == "archived":
						k.Icon(c, "archive", 14, t.MutedForeground).Role(ui.RoleImage).Label(L("Archived"))
					case r.reportKind == "done":
						k.Icon(c, "circle-check", 14, t.Success).Role(ui.RoleImage).Label(L("Done"))
					case r.reportKind == "blocked":
						k.Icon(c, "circle-alert", 14, t.AttentionForeground).Role(ui.RoleImage).Label(L("Blocked"))
					default:
						k.Icon(c, "circle-pause", 14, t.MutedForeground).Role(ui.RoleImage).Label(L("Idle"))
					}
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(1)).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(k.Px(5)).MinWidth(0).Children(func() {
						title := k.Text(c, r.title, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.Foreground).SingleLine().Shrink(1).MinWidth(0)
						if r.unread {
							title.FontWeight(500)
							kit.Dot(c, k.Px(6), t.Info).Role(ui.RoleImage).Label(L("Unread messages"))
						}
					})
					if r.activity != "" {
						color := t.MutedForeground
						if r.group == "waiting" {
							color = t.AttentionForeground
						}
						k.Text(c, r.activity, k.Dense(11, 12), k.Dense(16, 17)).TextColor(color).SingleLine()
					}
				})
				if r.direct {
					a.directBadge(c, r.directHint)
				}
				if (r.group == "idle" || r.group == "archived") && !r.at.IsZero() {
					k.Text(c, relativeTime(r.at, a.cfg.Now()), k.Dense(10, 11), k.Dense(15, 16.5)).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
				}
			})
			if b.Clicked() && !r.starting {
				a.openAgent(ctx.id, r.tab)
			}
		})
		if len(prs) > 0 {
			ui.Row(c.Key("prs:"+key)).Margin(0, k.Px(8), k.Px(3), k.Px(32)).Gap(k.Px(4)).AlignItems(ui.Center).Children(func() {
				shown := prs
				more := 0
				if len(shown) > 2 {
					more = len(shown) - 2
					shown = shown[:2]
				}
				for _, pr := range shown {
					a.agentPRBadge(c, pr)
				}
				if more > 0 {
					a.agentMorePRsBadge(c, ctx.id, more, len(prs))
				}
			})
		}
		if len(r.subagents) > 0 && !r.starting && r.tab != "" {
			a.subagentsToggleAndList(c, ctx, r.tab, r.title, r.subagents)
		}
	})
}

func (a *App) orchSubagentsGroup(c *ui.Context, ctx *wsContext, subs []subagentTreeRow) {
	k, t := a.kit, a.kit.T
	key := "orch:" + ctx.orch.SessionID
	title := L("Orchestrator subagents")
	open := ctx.st.subagentsExpanded(key)
	running := 0
	for _, sub := range subs {
		if sub.hasRun && subagents.IsRunning(sub.run.Status) {
			running++
		}
	}
	ui.Column(c.Key("group:orch-subagents")).Margin(k.Px(4), 0, 0, 0).Children(func() {
		head := ui.ButtonBase(c).Label(title).Expanded(open).AlignSelf(ui.Stretch).Height(k.Px(k.Dense(24, 28))).Gap(k.Px(6)).PaddingX(k.Px(6)).Radius(k.Px(8)).
			Justify(ui.Start).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		hovered := head.Hovered()
		if hovered {
			head.Background(t.Muted.Alpha(0.5))
			color = t.Foreground
		}
		head.Children(func() {
			if running > 0 {
				k.Spinner(c, 12, t.Info)
			} else {
				k.Icon(c, "git-fork", 12, color)
			}
			k.Text(c, title, k.Dense(11, 12), k.Dense(16, 18)).Role(ui.RoleHeading).FontWeight(500).TextColor(color).SingleLine()
			k.Text(c, strconv.Itoa(len(subs)), k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground).FontFeatures("tnum")
			rot := float32(0)
			if open {
				rot = 90
			}
			ch := k.Icon(c, "chevron-right", 12, color).Rotate(rot)
			if open && !hovered && !head.Focused() {
				ch.Opacity(0)
			}
		})
		if head.Clicked() {
			a.setWsState(ctx.id, func(u *workstreamUI) { u.toggleSubagents(key) })
		}
		if !open {
			return
		}
		ui.Column(c.Key("sublist:"+key)).Role(ui.RoleList).Label(title).Gap(1).
			Margin(0, 0, k.Px(4), k.Px(12)).BorderWidth(0, 0, 0, 1).BorderColor(t.Border.Alpha(0.7)).Padding(0, 0, 0, k.Px(6)).
			Children(func() {
				for _, sub := range subs {
					a.subagentItemRow(c, sub)
				}
			})
	})
}

func (a *App) subagentsToggleAndList(c *ui.Context, ctx *wsContext, key, ownerTitle string, subs []subagentTreeRow) {
	k, t := a.kit, a.kit.T
	open := ctx.st.subagentsExpanded(key)
	running := 0
	for _, sub := range subs {
		if sub.hasRun && subagents.IsRunning(sub.run.Status) {
			running++
		}
	}
	label := L("%d subagents", len(subs))
	if len(subs) == 1 {
		label = L("1 subagent")
	}
	if running > 0 {
		label = L("%s · %d running", label, running)
	}
	a11y := L("%s, %s", ownerTitle, label)
	tog := ui.ButtonBase(c.Key("subtog:"+key)).Label(a11y).Expanded(open).
		Margin(0, 0, k.Px(2), k.Px(24)).Height(k.Px(k.Dense(22, 24))).PaddingX(k.Px(6)).Gap(k.Px(5)).
		Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
	fg := t.MutedForeground
	if tog.Hovered() {
		tog.Background(t.Muted.Alpha(0.5))
		fg = t.Foreground
	}
	tog.Children(func() {
		rot := float32(0)
		if open {
			rot = 90
		}
		k.Icon(c, "chevron-right", 11, fg).Rotate(rot)
		k.Icon(c, "git-fork", 11, fg)
		k.Text(c, label, k.Dense(11, 12), k.Dense(15, 17)).TextColor(fg).SingleLine()
	})
	if tog.Clicked() {
		a.setWsState(ctx.id, func(u *workstreamUI) { u.toggleSubagents(key) })
	}
	if !open {
		return
	}
	ui.Column(c.Key("sublist:"+key)).Role(ui.RoleList).Label(a11y).Gap(1).
		Margin(0, 0, k.Px(4), k.Px(30)).BorderWidth(0, 0, 0, 1).BorderColor(t.Border.Alpha(0.7)).Padding(0, 0, 0, k.Px(6)).
		Children(func() {
			for _, sub := range subs {
				a.subagentItemRow(c, sub)
			}
		})
}

func (a *App) subagentItemRow(c *ui.Context, sub subagentTreeRow) {
	k, t := a.kit, a.kit.T
	statusText := L("Completed")
	switch {
	case sub.hasRun && subagents.IsRunning(sub.run.Status):
		statusText = L("Running")
	case sub.hasRun && sub.run.Status == subagents.Failed:
		statusText = L("Failed")
	case sub.hasRun && sub.run.Status == subagents.Cancelled:
		statusText = L("Stopped")
	case !sub.hasRun:
		statusText = L("Subagent")
	}
	b := ui.ButtonBase(c.Key("sub:" + sub.id)).Label(listName(sub.title, statusText)).Tooltip(sub.title).
		Grow(1).MinWidth(0).Height(k.Px(k.Dense(26, 28))).PaddingX(k.Px(6)).Gap(k.Px(6)).
		Justify(ui.Start).Radius(k.Px(6)).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(t.Muted.Alpha(0.55))
	}
	b.Children(func() {
		ui.Box(c).Size(k.Px(14), k.Px(14)).Center().Shrink(0).Children(func() {
			switch {
			case sub.hasRun && subagents.IsRunning(sub.run.Status):
				k.Spinner(c, 11, t.Info).Role(ui.RoleStatus).Label(statusText)
			case sub.hasRun && sub.run.Status == subagents.Completed:
				k.Icon(c, "circle-check", 12, t.Success).Role(ui.RoleImage).Label(statusText)
			case sub.hasRun && sub.run.Status == subagents.Failed:
				k.Icon(c, "circle-x", 12, t.DestructiveForeground).Role(ui.RoleImage).Label(statusText)
			case sub.hasRun && sub.run.Status == subagents.Cancelled:
				k.Icon(c, "circle-slash", 12, t.AttentionForeground).Role(ui.RoleImage).Label(statusText)
			default:
				k.Icon(c, "circle-dashed", 12, t.MutedForeground).Role(ui.RoleImage).Label(statusText)
			}
		})
		k.Text(c, sub.title, k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.Foreground).SingleLine().Grow(1).MinWidth(0)
		if !sub.at.IsZero() {
			k.Text(c, relativeTime(sub.at, a.cfg.Now()), k.Dense(10, 11), k.Dense(14, 16)).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		}
	})
	if b.Clicked() {
		a.Go(Route{Name: "session", SessionID: sub.id})
	}
}

func (a *App) workstreamSubagents(parentID string, listed []sessions.Summary) []subagentTreeRow {
	if parentID == "" || len(listed) == 0 {
		return nil
	}
	resolved := a.resolve(parentID)
	callers := []string{parentID}
	if resolved != "" && resolved != parentID {
		callers = append(callers, resolved)
	}
	for _, seed := range slices.Clone(callers) {
		if sel := findSummary(listed, seed); sel != nil {
			for _, cont := range sessions.ContinuationChain(listed, *sel) {
				if !slices.Contains(callers, cont.SessionID) {
					callers = append(callers, cont.SessionID)
				}
			}
		}
	}
	callerSet := map[string]bool{}
	for _, id := range callers {
		callerSet[id] = true
	}
	seen := map[string]bool{}
	var found []sessions.Summary
	// BFS so nested Task subagents under a Worker's subagents stay grouped under that Worker.
	for frontier := callers; len(frontier) > 0; {
		var next []string
		for _, s := range listed {
			if s.CallingSessionID == "" || !callerSet[s.CallingSessionID] || seen[s.SessionID] {
				continue
			}
			if sessions.IsWorkstreamOrchestrator(s.Tags) || sessions.IsWorkstreamAgent(s.Tags) {
				continue
			}
			seen[s.SessionID] = true
			found = append(found, s)
			callerSet[s.SessionID] = true
			next = append(next, s.SessionID)
		}
		frontier = next
	}
	if len(found) == 0 {
		return nil
	}
	out := make([]subagentTreeRow, 0, len(found))
	for _, s := range found {
		run, ok := a.subagentRun(s.SessionID)
		if a.workstreams.busy[s.SessionID] != "" {
			run, ok = subagents.Run{Status: subagents.Running}, true
		}
		var at time.Time
		if ms := sessions.UsedAt(s, a.sidebar.times); ms > 0 {
			at = time.UnixMilli(ms)
		}
		out = append(out, subagentTreeRow{
			id:     s.SessionID,
			title:  trimOr(s.Title, L("Subagent")),
			at:     at,
			run:    run,
			hasRun: ok,
		})
	}
	slices.SortStableFunc(out, func(x, y subagentTreeRow) int { return y.at.Compare(x.at) })
	return out
}

// goalPanel is the Goal tab: renders plan.md (or PLAN.md / promise.md)
// from the Workstream's shared folder, or an empty state when absent.
func (a *App) goalPanel(c *ui.Context, ctx *wsContext) {
	k, t := a.kit, a.kit.T
	dir := ""
	if ctx.w != nil {
		dir = ctx.w.SharedFolder
	}
	empty := func(title, text string) {
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(k.Px(2)).PaddingX(k.Px(24)).Children(func() {
			k.Text(c, title, k.Dense(12, 13), k.Dense(18, 20)).FontWeight(500).Role(ui.RoleStatus)
			if text != "" {
				k.Text(c, text, k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground).TextAlign(ui.Center)
			}
		})
	}
	if dir == "" || a.following() {
		empty(L("Not available for this workstream"), "")
		return
	}
	c.After(time.Second)
	g, ready := a.cachedGoal(dir)
	if !ready {
		return
	}
	if g.file == "" || strings.TrimSpace(g.text) == "" {
		empty(L("No plan yet"), L("The orchestrator’s plan.md in the shared folder shows up here."))
		return
	}
	ui.Row(c).Shrink(0).Height(k.Px(36)).PaddingX(k.Px(12)).Gap(k.Px(8)).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
		k.Icon(c, "file-text", 14, t.MutedForeground)
		k.Text(c, g.file, k.Dense(12, 13), 18).FontWeight(500).TextColor(t.Foreground).SingleLine().Grow(1).MinWidth(0)
		if a.cfg.OpenPath != nil {
			if k.IconButton(c, "arrow-up-right", L("Open %s", g.file), 28).Clicked() {
				p := g.fullPath
				go func() { _ = a.cfg.OpenPath(p) }()
			}
		}
	})
	a.workstreams.goalMD.Own = true
	room := max(k.Px(200), a.paneWidth()-k.Px(28))
	a.workstreams.goalMD.SetRoom(room, room)
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(k.Px(12), k.Px(14), k.Px(16), k.Px(14)).Children(func() {
		markdown.View(c, g.text, a.workstreams.goalMD.Options(k, sessionview.MarkdownStyle{Size: 13}))
	})
}

var goalCandidates = []string{"plan.md", "PLAN.md", "promise.md"}

type goalCache struct {
	dir      string
	at       time.Time
	file     string
	fullPath string
	modTime  time.Time
	size     int64
	text     string
	have     bool
	reading  bool
}

type goalDoc struct {
	file, fullPath string
	modTime        time.Time
	size           int64
	text           string
}

func (a *App) cachedGoal(dir string) (goalDoc, bool) {
	gc := &a.workstreams.goal
	stale := !gc.have || gc.dir != dir || a.cfg.Now().Sub(gc.at) >= time.Second
	if stale && !gc.reading {
		gc.reading = true
		prevPath, prevMod, prevSize := gc.fullPath, gc.modTime, gc.size
		prevFile, prevText := gc.file, gc.text
		go func() {
			doc := readGoalDoc(dir, prevPath, prevMod, prevSize, prevFile, prevText)
			a.cfg.Update(func() {
				gc.reading, gc.have = false, true
				gc.dir, gc.at = dir, a.cfg.Now()
				gc.file, gc.fullPath, gc.modTime, gc.size, gc.text = doc.file, doc.fullPath, doc.modTime, doc.size, doc.text
			})
		}()
	}
	if !gc.have || gc.dir != dir {
		return goalDoc{}, false
	}
	return goalDoc{file: gc.file, fullPath: gc.fullPath, modTime: gc.modTime, size: gc.size, text: gc.text}, true
}

func readGoalDoc(dir, prevPath string, prevMod time.Time, prevSize int64, prevFile, prevText string) goalDoc {
	for _, name := range goalCandidates {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		if p == prevPath && info.ModTime().Equal(prevMod) && info.Size() == prevSize && prevFile == name {
			return goalDoc{file: name, fullPath: p, modTime: info.ModTime(), size: info.Size(), text: prevText}
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return goalDoc{}
		}
		return goalDoc{file: name, fullPath: p, modTime: info.ModTime(), size: info.Size(), text: string(b)}
	}
	return goalDoc{}
}

// directBadge is the small mark on an agent that can use only Direct tools.
func (a *App) directBadge(c *ui.Context, hint string) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Role(ui.RoleStatus).Label(L("Direct") + ". " + hint).Tooltip(hint).Height(k.Px(16)).PaddingX(k.Px(5)).AlignItems(ui.Center).Shrink(0).Radius(k.Px(4)).
		Background(t.Attention.Alpha(0.12)).Children(func() {
		k.Text(c, L("Direct"), 10, 14).FontWeight(500).TextColor(t.AttentionForeground)
	})
}

// filesPanel is the Files tab: the Workstream's shared folder, searched by
// name. Only a folder on this computer is read here.
func (a *App) filesPanel(c *ui.Context, ctx *wsContext) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	dir := ""
	if ctx.w != nil {
		dir = ctx.w.SharedFolder
	}
	empty := func(title, text string) {
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(k.Px(2)).PaddingX(k.Px(24)).Children(func() {
			k.Text(c, title, k.Dense(12, 13), k.Dense(18, 20)).FontWeight(500).Role(ui.RoleStatus)
			k.Text(c, text, k.Dense(11, 12), k.Dense(16, 18)).TextColor(t.MutedForeground).TextAlign(ui.Center)
		})
	}
	if dir == "" || a.following() {
		empty(L("Not available for this workstream"), "")
		return
	}
	ui.Row(c).Shrink(0).Padding(k.Px(10), k.Px(12), k.Px(6), k.Px(12)).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Radius(k.Px(8)).Border(1, t.Input).Background(t.Background).AlignItems(ui.Center).PaddingX(k.Px(8)).Gap(k.Px(6)).Children(func() {
			k.Icon(c, "search", 14, t.MutedForeground)
			ui.TextInputBase(c, &s.fileQuery).Label(L("Search files")).Placeholder(L("Search files")).Grow(1).MinWidth(0).Height(k.Px(k.Dense(30, 34))).
				FontSize(k.Px(k.Dense(12, 13))).TextColor(t.Foreground)
		})
	})
	// The folder is read again once a second at most, while the panel shows.
	c.After(time.Second)
	files, ready := a.cachedFiles(dir, strings.ToLower(strings.TrimSpace(s.fileQuery)))
	if !ready {
		return
	}
	if len(files) == 0 {
		if strings.TrimSpace(s.fileQuery) != "" {
			empty(L("No files match your search"), "")
		} else {
			empty(L("No shared files yet"), L("Files the workstream’s agents save to its shared folder show up here."))
		}
		return
	}
	tree := buildFileTree(files)
	searching := strings.TrimSpace(s.fileQuery) != ""
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, k.Px(8), k.Px(12), k.Px(8)).Children(func() {
		ui.Column(c).Role(ui.RoleList).Label(L("Files")).Gap(1).Children(func() {
			a.renderFileTree(c, dir, tree, 0, searching)
		})
	})
}

type fileTreeNode struct {
	name      string
	path      string
	dirs      []*fileTreeNode
	files     []sharedFile
	fileCount int
}

func buildFileTree(files []sharedFile) *fileTreeNode {
	root := &fileTreeNode{}
	dirMap := map[string]*fileTreeNode{"": root}
	ensureDir := func(relDir string) *fileTreeNode {
		if n, ok := dirMap[relDir]; ok {
			return n
		}
		parts := strings.Split(filepath.ToSlash(relDir), "/")
		cur := root
		var acc string
		for _, p := range parts {
			if p == "" || p == "." {
				continue
			}
			if acc == "" {
				acc = p
			} else {
				acc += "/" + p
			}
			child, ok := dirMap[acc]
			if !ok {
				child = &fileTreeNode{name: p, path: acc}
				dirMap[acc] = child
				cur.dirs = append(cur.dirs, child)
			}
			cur = child
		}
		return cur
	}
	for _, f := range files {
		slash := filepath.ToSlash(f.rel)
		dir := filepath.ToSlash(filepath.Dir(slash))
		if dir == "." {
			dir = ""
		}
		parent := ensureDir(dir)
		parent.files = append(parent.files, f)
	}
	var finalize func(*fileTreeNode) int
	finalize = func(n *fileTreeNode) int {
		slices.SortFunc(n.dirs, func(x, y *fileTreeNode) int {
			return cmp.Compare(strings.ToLower(x.name), strings.ToLower(y.name))
		})
		slices.SortFunc(n.files, func(x, y sharedFile) int {
			return y.modTime.Compare(x.modTime)
		})
		total := len(n.files)
		for _, d := range n.dirs {
			total += finalize(d)
		}
		n.fileCount = total
		return total
	}
	finalize(root)
	return root
}

func (a *App) renderFileTree(c *ui.Context, rootDir string, node *fileTreeNode, depth int, forceOpen bool) {
	k, t := a.kit, a.kit.T
	s := &a.workstreams
	for _, d := range node.dirs {
		dirKey := rootDir + "\x00" + d.path
		open := forceOpen || (s.expandedDirs != nil && s.expandedDirs[dirKey])
		ui.Column(c.Key("dir:" + d.path)).Children(func() {
			row := ui.ButtonBase(c).Label(d.name).Expanded(open).Grow(1).MinWidth(0).
				Height(k.Px(k.Dense(28, 32))).Padding(0, k.Px(8), 0, k.Px(8+float32(depth)*14)).Gap(k.Px(6)).
				Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(t.Muted.Alpha(0.6))
			}
			row.Children(func() {
				rot := float32(0)
				if open {
					rot = 90
				}
				k.Icon(c, "chevron-right", 12, t.MutedForeground).Rotate(rot)
				folderIcon := "folder"
				if open {
					folderIcon = "folder-open"
				}
				k.Icon(c, folderIcon, 14, t.MutedForeground)
				k.Text(c, d.name, k.Dense(12, 13), k.Dense(18, 19.5)).FontWeight(500).TextColor(t.Foreground).SingleLine().Grow(1).MinWidth(0)
				k.Text(c, strconv.Itoa(d.fileCount), k.Dense(10, 11), k.Dense(15, 16.5)).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
			})
			if row.Clicked() {
				if s.expandedDirs == nil {
					s.expandedDirs = map[string]bool{}
				}
				s.expandedDirs[dirKey] = !s.expandedDirs[dirKey]
			}
			if open {
				a.renderFileTree(c, rootDir, d, depth+1, forceOpen)
			}
		})
	}
	for _, f := range node.files {
		a.fileTreeRow(c, rootDir, f, depth)
	}
}

// sharedFile is a file of the shared folder, by its path under it.
type sharedFile struct {
	rel     string
	modTime time.Time
}

// filesCache is the last read of a shared folder.
type filesCache struct {
	dir, query string
	at         time.Time
	list       []sharedFile
	have       bool
	reading    bool
	// reads counts the folder's reads, for a test of how often they are.
	reads int
}

// cachedFiles is the shared folder's files, read in the background and not
// again for a second: a frame must not walk a folder. ready is false until
// the first read of a folder is in.
func (a *App) cachedFiles(dir, query string) (list []sharedFile, ready bool) {
	fc := &a.workstreams.files
	stale := !fc.have || fc.dir != dir || fc.query != query || a.cfg.Now().Sub(fc.at) >= time.Second
	if stale && !fc.reading {
		fc.reading = true
		fc.reads++
		go func() {
			found := sharedFiles(dir, query)
			a.cfg.Update(func() {
				fc.reading, fc.have = false, true
				fc.dir, fc.query, fc.list, fc.at = dir, query, found, a.cfg.Now()
			})
		}()
	}
	// Another folder's list is no stand-in; the same folder's older list is.
	if !fc.have || fc.dir != dir {
		return nil, false
	}
	return fc.list, true
}

// sharedFiles lists the files under dir whose path has query, newest first,
// at most maxSharedFiles of them: all are read before the newest are kept.
func sharedFiles(dir, query string) []sharedFile {
	var out []sharedFile
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if query != "" && !strings.Contains(strings.ToLower(rel), query) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		out = append(out, sharedFile{rel: rel, modTime: info.ModTime()})
		return nil
	})
	slices.SortFunc(out, func(x, y sharedFile) int { return y.modTime.Compare(x.modTime) })
	return out[:min(len(out), maxSharedFiles)]
}

const maxSharedFiles = 500

// fileTreeRow is a shared file's row in the folder tree: it opens in the app that takes it.
func (a *App) fileTreeRow(c *ui.Context, dir string, f sharedFile, depth int) {
	k, t := a.kit, a.kit.T
	name := filepath.Base(f.rel)
	ui.Row(c.Key(f.rel)).Role(ui.RoleListItem).Children(func() {
		b := ui.ButtonBase(c).Label(f.rel).Tooltip(f.rel).Grow(1).MinWidth(0).Height(k.Px(k.Dense(28, 32))).
			Padding(0, k.Px(8), 0, k.Px(8+float32(depth)*14)).Gap(k.Px(8)).
			Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(t.Muted.Alpha(0.6))
		}
		b.Children(func() {
			k.Icon(c, "file-text", 14, t.MutedForeground)
			k.Text(c, name, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.Foreground).SingleLine().Grow(1).MinWidth(0)
			k.Text(c, relativeTime(f.modTime, a.cfg.Now()), k.Dense(10, 11), k.Dense(15, 16.5)).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		})
		if b.Clicked() && a.cfg.OpenPath != nil {
			path := filepath.Join(dir, f.rel)
			go func() { _ = a.cfg.OpenPath(path) }()
		}
	})
}

// sessionOf is Session id as the Store has it, nil when it is not loaded.
func (a *App) sessionOf(id string) *session.Session {
	if a.ctl == nil {
		return nil
	}
	return a.ctl.Store().Session(id)
}
