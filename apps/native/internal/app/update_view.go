package app

import (
	"context"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/md"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/updates"
)

func describeUpdate(s updates.State) string {
	switch s.Status {
	case updates.Checking:
		return L("Checking…")
	case updates.UpToDate:
		return L("Up to date")
	case updates.Available:
		return L("%s is available", s.Version)
	case updates.Downloading:
		return L("Downloading %d%%", s.Percent)
	case updates.Ready:
		return L("%s is installed", s.Version)
	case updates.Failed:
		return s.Message
	}
	return ""
}

func (a *App) checkForUpdate() { go a.cfg.Updater.Check(context.Background()) }
func (a *App) installUpdate()  { go a.cfg.Updater.Install(context.Background()) }
func (a *App) relaunch() {
	if a.cfg.Relaunch != nil {
		a.cfg.Relaunch()
	}
}

// RelaunchIfIdle starts the installed update when nobody would notice: no
// Session is working or waiting for an answer and the window is in the
// background. Drafts are saved as they are typed, so none is lost.
func (a *App) RelaunchIfIdle() bool {
	if a.cfg.Updater == nil || a.cfg.Updater.State().Status != updates.Ready || a.focused() {
		return false
	}
	_, _, reported, _, _, _ := a.snapshot()
	if len(a.activity(reported)) > 0 {
		return false
	}
	a.relaunch()
	return true
}

// updateControl is the About row's control: one button that walks the
// update through check → download → restart, the state written beside it.
func (a *App) updateControl(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := a.cfg.Updater.State()
	working := s.Status == updates.Checking || s.Status == updates.Downloading
	ui.Row(c).Gap(k.Px(12)).AlignItems(ui.Center).Children(func() {
		if d := describeUpdate(s); d != "" {
			k.Text(c, d, 12, 16).Role(ui.RoleStatus).TextColor(t.MutedForeground).MaxWidth(k.Px(260)).MaxLines(2)
		}
		switch s.Status {
		case updates.Available:
			if a.smallButton(c, kit.Primary, L("Update to %s", s.Version), "arrow-down-to-line", false).Clicked() {
				a.installUpdate()
			}
		case updates.Ready:
			if a.smallButton(c, kit.Primary, L("Restart to update"), "", false).Clicked() {
				a.relaunch()
			}
		default:
			label := L("Check for updates")
			if s.Status == updates.Failed {
				label = L("Try again")
			}
			if a.smallButton(c, kit.Outline, label, "", working).Clicked() {
				a.checkForUpdate()
			}
		}
	})
}

// showsNotes is whether the About row shows a release's notes: while it
// waits, downloads or is installed.
func showsNotes(s updates.State) bool {
	return s.Notes != "" && (s.Status == updates.Available || s.Status == updates.Downloading || s.Status == updates.Ready)
}

// updateNotes are what the release changes, under the About row.
func (a *App) updateNotes(c *ui.Context, s updates.State) {
	k := a.kit
	ui.Column(c).Role(ui.RoleGroup).Label(L("Release notes")).Children(func() {
		k.Text(c, L("What’s new in %s", s.Version), 13, 20).FontWeight(500)
		p := newProse(k)
		p.size, p.lh, p.color = 13, 22, k.T.MutedForeground
		f := flow{c: c}
		p.nodes(c, &f, md.ParseTree(s.Notes))
	})
}

// updateCard is the corner card once a release waits, downloads or is
// installed; closing it hides that one step.
func (a *App) updateCard(c *ui.Context) {
	if a.cfg.Updater == nil {
		return
	}
	s := a.cfg.Updater.State()
	if s.Status != updates.Available && s.Status != updates.Downloading && s.Status != updates.Ready {
		return
	}
	step := string(s.Status) + ":" + s.Version
	if a.updateDismissed == step {
		return
	}
	k, t := a.kit, a.kit.T
	icon, title := "arrow-down-to-line", L("Droi %s is available", s.Version)
	switch s.Status {
	case updates.Ready:
		icon, title = "refresh-cw", L("Droi %s is ready", s.Version)
	case updates.Downloading:
		title = L("Downloading Droi %s…", s.Version)
	}
	a.cornerCard(c, L("Update"), icon, func() {
		k.Text(c, title, 14, 20).FontWeight(500)
		switch s.Status {
		case updates.Downloading:
			ui.Box(c).Role(ui.RoleProgress).Label(L("Download progress")).FillWidth().Height(k.Px(4)).Radius(k.Px(2)).Background(t.Muted).Margin(k.Px(8), 0, 0, 0).Clip().Children(func() {
				ui.Box(c).WidthPercent(float32(s.Percent)).FillHeight().Radius(k.Px(2)).Background(t.Primary)
			})
		case updates.Ready:
			if a.smallButton(c, kit.Primary, L("Restart now"), "", false).AlignSelf(ui.Start).Margin(k.Px(8), 0, 0, 0).Clicked() {
				a.relaunch()
			}
		default:
			ui.Row(c).Wrap().Gap(k.Px(4)).Margin(k.Px(8), 0, 0, 0).Children(func() {
				if a.smallButton(c, kit.Primary, L("Update"), "", false).Clicked() {
					a.installUpdate()
				}
				if a.smallButton(c, kit.Ghost, L("Details"), "", false).Clicked() {
					a.Go(Route{Name: "settings", Tab: "about"})
				}
				if a.smallButton(c, kit.Ghost, L("Skip this version"), "", false).Clicked() {
					prefs.SkippedUpdate.Set(a.prefs, s.Version)
					a.cfg.Updater.Skip(s.Version)
				}
			})
		}
	}, func() { a.updateDismissed = step })
}
