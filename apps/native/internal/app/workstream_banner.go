package app

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// workstreamBanners are what the orchestrator's page keeps over its
// composer: the agent that has waited longest on the user, and the count of
// agents at work while the panel that lists them is out of sight.
func (a *App) workstreamBanners(c *ui.Context, ctx *wsContext) {
	if ctx.w == nil {
		return
	}
	var waiting []paneRow
	working := 0
	for _, r := range a.paneRows(ctx, a.listedNow()) {
		switch r.group {
		case "waiting":
			waiting = append(waiting, r)
		case "working":
			if !r.starting {
				working++
			}
		}
	}
	if len(waiting) > 0 {
		a.attentionCard(c, ctx, waiting)
	}
	if own := a.workstreams.busy[ctx.orch.SessionID]; working > 0 && (own == "" || own == activity.NeedsInput) &&
		(ctx.st.Closed || ctx.st.panel() != panelAgents || a.narrow) {
		a.workingRow(c, ctx, working)
	}
}

// waitedSince is when agent id started to wait on the user, the zero time
// when this window does not know.
func (a *App) waitedSince(id string) time.Time {
	if !a.loaded(id) {
		return time.Time{}
	}
	var since time.Time
	for _, p := range a.ctl.PendingPermissions(id) {
		if since.IsZero() || p.ReceivedAt.Before(since) {
			since = p.ReceivedAt
		}
	}
	for _, q := range a.ctl.PendingAskUsers(id) {
		if since.IsZero() || q.ReceivedAt.Before(since) {
			since = q.ReceivedAt
		}
	}
	return since
}

// attentionCard asks the user to look at the agent that has waited longest
// on them.
func (a *App) attentionCard(c *ui.Context, ctx *wsContext, waiting []paneRow) {
	k, t := a.kit, a.kit.T
	slices.SortStableFunc(waiting, func(x, y paneRow) int {
		xs, ys := a.waitedSince(x.id), a.waitedSince(y.id)
		switch {
		case xs.IsZero() && ys.IsZero():
			return 0
		case xs.IsZero():
			return 1
		case ys.IsZero():
			return -1
		}
		return xs.Compare(ys)
	})
	first := waiting[0]
	reason := first.activity
	if reason == "" {
		reason = L("Needs your attention")
	}
	ui.Row(c.Key("attention")).Role(ui.RoleGroup).Label(L("Needs your attention")).Margin(0, 0, k.Px(8), 0).Padding(k.Px(10), k.Px(12)).Gap(k.Px(10)).
		AlignItems(ui.Center).Radius(k.Px(12)).Border(1, t.Attention.Alpha(0.35)).Background(t.Attention.Alpha(0.06)).Children(func() {
		k.Icon(c, "circle-alert", 16, t.Attention)
		ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(1)).Children(func() {
			ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Children(func() {
				k.Text(c, first.title, k.Dense(12, 13), k.Dense(18, 19.5)).FontWeight(500).SingleLine().Shrink(1).MinWidth(0)
				if n := len(waiting); n > 1 {
					ui.Row(c).Shrink(0).PaddingX(k.Px(6)).Height(k.Px(18)).AlignItems(ui.Center).Radius(k.Px(9)).Background(t.Attention.Alpha(0.18)).Children(func() {
						k.Text(c, L("%d waiting", n), 11, 16).FontWeight(500).TextColor(t.AttentionForeground).FontFeatures("tnum")
					})
				}
			})
			k.Text(c, reason, k.Dense(11, 12), k.Dense(16, 17)).TextColor(t.MutedForeground).SingleLine()
		})
		if a.smallButton(c, kit.Outline, L("View Agent"), "", false).Clicked() {
			a.openAgent(ctx.id, first.tab)
		}
	})
}

// workingRow is the line at the right over the composer that counts the
// agents at work and opens the panel that lists them.
func (a *App) workingRow(c *ui.Context, ctx *wsContext, n int) {
	k, t := a.kit, a.kit.T
	label := L("1 agent working")
	if n > 1 {
		label = L("%d agents working", n)
	}
	ui.Row(c.Key("working")).Justify(ui.End).Margin(0, 0, k.Px(4), 0).Children(func() {
		b := ui.ButtonBase(c).Label(label).Height(k.Px(26)).PaddingX(k.Px(8)).Gap(k.Px(6)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
		color := t.MutedForeground
		if b.Hovered() {
			b.Background(t.Muted.Alpha(0.6))
			color = t.Foreground
		}
		b.Children(func() {
			k.Spinner(c, 12, t.Info)
			k.Text(c, label, k.Dense(11, 12), k.Dense(16, 18)).TextColor(color)
			k.Icon(c, "arrow-up-right", 12, color)
		})
		if b.Clicked() {
			a.showPanel(ctx.id, panelAgents)
		}
	})
}

// composerCompactBelow is the width of the page, in DIPs, under which the
// composer's bar shows icons: the model, Spec, autonomy and the tools with
// the Orchestrator's mark and the send button do not fit one row in words.
const composerCompactBelow = 760

// composerCompact is whether the page the composer is in is narrower than
// composerCompactBelow: the window less the sidebar and, on a Workstream's
// page, its panel.
func (a *App) composerCompact(c *ui.Context, v *sessionView) bool {
	w, _ := c.Size()
	if !a.narrow {
		if a.sidebarShown() {
			w -= a.sidebarWidth()
		}
		if v.ws != nil && !v.ws.st.Closed {
			w -= a.paneWidth()
		}
	}
	return w < a.kit.Px(composerCompactBelow)
}

// orchestratorChip marks the composer of the orchestrator's page.
func (a *App) orchestratorChip(c *ui.Context, tight bool) {
	k, t := a.kit, a.kit.T
	pad, size := float32(8), float32(12)
	if tight {
		pad, size = 6, 11
	}
	ui.Row(c).Role(ui.RoleImage).Label(L("Orchestrator")).Height(k.Px(22)).PaddingX(k.Px(pad)).Margin(0, 0, 0, k.Px(2)).AlignItems(ui.Center).Radius(k.Px(6)).Shrink(0).
		Background(t.Info.Alpha(0.12)).Children(func() {
		k.Text(c, L("Orchestrator"), size, 16).FontWeight(500).TextColor(t.InfoForeground).SingleLine()
	})
}

// workspaceBranch is the current branch of the repository dir, "" for none
// or until the Daemon has said.
func (a *App) workspaceBranch(dir string) string {
	if dir == "" {
		return ""
	}
	ch, ok := a.workstreams.checks[dir]
	if !ok {
		a.askWorkspace(dir)
		return ""
	}
	return ch.branch
}

// welcomeBlock stands in for an empty orchestrator Session: what the
// Workstream is, where it works, and how agents and shared files fit.
func (a *App) welcomeBlock(c *ui.Context, ctx *wsContext) {
	k, t := a.kit, a.kit.T
	dir := ctx.orch.Cwd
	if ctx.w != nil && ctx.w.Workspace != "" {
		dir = ctx.w.Workspace
	}
	branch := a.workspaceBranch(dir)
	shared := ""
	if ctx.w != nil {
		shared = ctx.w.SharedFolder
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().MaxWidth(k.Px(768)).AlignSelf(ui.Center).Padding(k.Px(28), k.Px(24), k.Px(24), k.Px(24)).Gap(k.Px(14)).Children(func() {
			ui.Column(c).Role(ui.RoleGroup).Label(L("Welcome to your workstream!")).Gap(k.Px(6)).Children(func() {
				k.Text(c, L("Welcome to your workstream!"), k.Dense(14, 15), 22).Role(ui.RoleHeading).FontWeight(600)
				k.Text(c, L("Tell me what to work on first, or I’ll start planning from the workstream name."), k.Dense(13, 14), k.Dense(20, 21)).TextColor(t.MutedForeground)
			})
			ui.Column(c).Role(ui.RoleGroup).Label(L("Details")).Children(func() {
				row := func(icon, label, value string) {
					ui.Row(c).FillWidth().Height(k.Px(k.Dense(34, 38))).Gap(k.Px(10)).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
						k.Icon(c, icon, 14, t.MutedForeground)
						k.Text(c, label, k.Dense(12, 13), 18).TextColor(t.MutedForeground).Width(k.Px(150)).Shrink(0)
						k.Text(c, value, k.Dense(12, 13), 18).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0)
					})
				}
				row("folder", L("Working directory"), dir)
				if branch != "" {
					row("git-branch", L("Branch"), branch)
				}
			})
			k.Text(c, L("How this workstream works"), k.Dense(13, 14), 20).Role(ui.RoleHeading).FontWeight(500)
			ui.Row(c).Wrap().Gap(k.Px(12)).Children(func() {
				a.welcomeCard(c, ctx, "bot", L("Agents work on their own"), L("Each owns a piece of the work and reports back to me. Answer their questions here."), L("Agents"), panelAgents)
				if shared != "" {
					a.welcomeCard(c, ctx, "file", L("Shared notes and plans"), L("What I and the agents write goes into the workstream’s shared folder."), L("Files"), panelFiles)
				}
			})
		})
	})
}

// welcomeCard is a card of the welcome block: what a part of the Workstream
// is, with a button that opens the panel tab that shows it.
func (a *App) welcomeCard(c *ui.Context, ctx *wsContext, icon, title, text, label, panel string) {
	k, t := a.kit, a.kit.T
	ui.Column(c.Key("card:"+panel)).Role(ui.RoleGroup).Label(title).Grow(1).MinWidth(k.Px(220)).Padding(k.Px(16)).Gap(k.Px(8)).
		Radius(k.Px(12)).Border(1, t.Border).Children(func() {
		ui.Box(c).Size(k.Px(32), k.Px(32)).Radius(k.Px(8)).Background(t.Muted).Center().Margin(0, 0, k.Px(2), 0).Children(func() {
			k.Icon(c, icon, 16, t.MutedForeground)
		})
		k.Text(c, title, k.Dense(13, 14), 20).FontWeight(500)
		k.Text(c, text, k.Dense(12, 13), k.Dense(18, 20)).TextColor(t.MutedForeground)
		ui.Row(c).Margin(k.Px(4), 0, 0, 0).Children(func() {
			b := k.Button(c, kit.Outline, k.Dense(28, 32), false, label).Radius(k.Px(6))
			b.Children(func() {
				k.Icon(c, icon, 14, t.Foreground)
				k.Text(c, label, k.Dense(12, 13), 18).FontWeight(500)
				k.Icon(c, "arrow-up-right", 12, t.MutedForeground)
			})
			if b.Clicked() {
				a.showPanel(ctx.id, panel)
			}
		})
	})
}
