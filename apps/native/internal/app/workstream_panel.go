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
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
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
		row := paneRow{tab: ag.SessionID, id: id, title: trimOr(ag.Title, L("Agent")), group: group, unread: a.sidebar.unread[id]}
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
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(10)).Gap(k.Px(4)).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {
			ui.Row(c).Role(ui.RoleTabList).Label(L("Panel tabs")).Grow(1).Gap(k.Px(4)).Children(func() {
				for _, tab := range [][3]string{{panelAgents, "bot", L("Agents")}, {panelFiles, "file", L("Files")}} {
					on := panel == tab[0]
					b := kit.Selected(ui.ButtonBase(c).Role(ui.RoleTab).Label(tab[2]), on).Height(k.Px(30)).PaddingX(k.Px(10)).Gap(k.Px(6)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
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
						k.Icon(c, tab[1], 14, color)
						k.Text(c, tab[2], k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(color)
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
		if panel == panelFiles {
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
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, k.Px(8), k.Px(12), k.Px(8)).Children(func() {
		for _, g := range groupOrder {
			a.paneGroup(c, ctx, g, shown, listed)
		}
		a.paneGroup(c, ctx, "archived", shown, listed)
	})
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
	var list []paneRow
	for _, r := range rows {
		if r.group == group {
			list = append(list, r)
		}
	}
	if len(list) == 0 {
		return
	}
	if group == "working" {
		slices.SortStableFunc(list, func(x, y paneRow) int { return cmp.Compare(strings.ToLower(x.title), strings.ToLower(y.title)) })
	} else {
		slices.SortStableFunc(list, func(x, y paneRow) int { return y.at.Compare(x.at) })
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
	ui.Row(c.Key(key)).Role(ui.RoleListItem).Children(func() {
		b := ui.ButtonBase(c).Label(name).Grow(1).MinWidth(0).MinHeight(k.Px(k.Dense(36, 40))).Padding(k.Px(5), k.Px(8), k.Px(5), k.Px(8)).Gap(k.Px(8)).
			Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer).Disabled(r.starting)
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
					kit.Dot(c, k.Px(8), t.Attention).Role(ui.RoleImage).Label(L("Waiting for your response"))
				case r.group == "working":
					k.Spinner(c, 12, t.Info).Role(ui.RoleStatus).Label(L("Working"))
				case r.unread:
					kit.Dot(c, k.Px(6), t.Info).Role(ui.RoleImage).Label(L("Unread messages"))
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(1)).Children(func() {
				title := k.Text(c, r.title, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.Foreground).SingleLine()
				if r.unread {
					title.FontWeight(500)
				}
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
	ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, k.Px(8), k.Px(12), k.Px(8)).Children(func() {
		ui.Column(c).Role(ui.RoleList).Label(L("Files")).Gap(1).Children(func() {
			for _, f := range files {
				a.fileRow(c, dir, f)
			}
		})
	})
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

// fileRow is a shared file's row: it opens in the app that takes it.
func (a *App) fileRow(c *ui.Context, dir string, f sharedFile) {
	k, t := a.kit, a.kit.T
	ui.Row(c.Key(f.rel)).Role(ui.RoleListItem).Children(func() {
		b := ui.ButtonBase(c).Label(f.rel).Tooltip(f.rel).Grow(1).MinWidth(0).Height(k.Px(k.Dense(30, 34))).PaddingX(k.Px(8)).Gap(k.Px(8)).
			Justify(ui.Start).Radius(k.Px(8)).Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(t.Muted.Alpha(0.6))
		}
		b.Children(func() {
			k.Icon(c, "file-text", 14, t.MutedForeground)
			k.Text(c, f.rel, k.Dense(12, 13), k.Dense(18, 19.5)).TextColor(t.Foreground).SingleLine().Grow(1).MinWidth(0)
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
