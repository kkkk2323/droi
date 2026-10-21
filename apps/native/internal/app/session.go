package app

import (
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/pasted"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// sessionView is the state of one Session's page, kept while the window
// lives so that going back finds it where it was: the Session view, and the
// header, turn rail and composer the window draws with it.
type sessionView struct {
	*sessionview.View
	a  *App
	id string

	rail railState
	// attached are the composer's pictures, decoded.
	attached map[string]*ui.Bitmap

	renaming bool
	rename   string
	openIn   bool
	// actionsOpen: the menu by the header's title.
	actionsOpen bool

	composer composerState
	tools    toolsState
	git      gitState
	// subOpen and trailOpen: the header's subagent menus.
	subOpen, trailOpen bool
	// speedOpen: the footer's model speed details.
	speedOpen bool
	// jobsOpen: the header's background tasks.
	jobsOpen bool

	// focused is whether the composer took the focus once already.
	focused bool
}

func (a *App) view(id string) *sessionView {
	v := a.views[id]
	if v == nil {
		v = &sessionView{a: a, id: id, attached: map[string]*ui.Bitmap{}, rail: railState{preview: -1}}
		v.View = sessionview.New(id, sessionview.Env{
			Controller:     a.ctl,
			Ctx:            a.ctx,
			Kit:            func() *kit.Kit { return a.kit },
			Update:         a.cfg.Update,
			Now:            a.cfg.Now,
			Revisions:      &a.revs,
			OpenSession:    func(id string) { a.Go(Route{Name: "session", SessionID: id}) },
			Quote:          v.quote,
			ScrollShortcut: shortcut(CmdScrollToLatest),
			Overlay:        v.turnRail,
			Local:          func() bool { return !a.following() },
		})
		d := a.drafts.Load(id)
		text, texts := pasted.Split(d.Text)
		v.composer.text, v.composer.images, v.composer.pasted = text, d.Images, pastedTexts(texts)
		a.views[id] = v
	}
	return v
}

// keptViews is how many Sessions keep their view, the open one included:
// a view holds its transcript, parsed Markdown and decoded pictures.
const keptViews = 3

// remember notes that Session id was opened, and drops the views of the
// Sessions opened longest ago, except one that is busy.
func (a *App) remember(id string) {
	a.recent = append(slices.DeleteFunc(a.recent, func(x string) bool { return x == id }), id)
	if len(a.recent) <= keptViews {
		return
	}
	keep := map[string]bool{}
	for _, x := range a.recent[len(a.recent)-keptViews:] {
		keep[x] = true
	}
	var recent []string
	for _, x := range a.recent {
		v := a.views[x]
		if !keep[x] && v != nil && !v.Busy() {
			delete(a.views, x)
			a.forget(x, keep)
			continue
		}
		recent = append(recent, x)
	}
	a.recent = recent
}

// forget drops the messages of Session id, and of the subagents it called,
// from the Store when nothing there is working or waiting for an answer:
// the Store keeps every loaded Session's messages otherwise. Opening it
// again loads it again.
func (a *App) forget(id string, keep map[string]bool) {
	if a.ctl == nil {
		return
	}
	store := a.ctl.Store()
	idle := func(x string) bool {
		s := store.Session(x)
		return s != nil && s.WorkingState() == protocol.DroidWorkingStateIdle &&
			len(a.ctl.PendingPermissions(x))+len(a.ctl.PendingAskUsers(x)) == 0
	}
	ids := []string{id}
	for _, x := range store.SessionIDs() {
		if s := store.Session(x); s != nil {
			if caller, _ := s.CallingSession(); caller == id && !keep[x] {
				ids = append(ids, x)
			}
		}
	}
	for _, x := range ids {
		if idle(x) {
			a.ctl.Forget(x)
		}
	}
}

// build is the web Client's SessionView: the header, the transcript and
// the composer.
func (v *sessionView) build(c *ui.Context, a *App, sel *sessions.Summary, listed []sessions.Summary) {
	s := v.Frame()
	title := L("Untitled session")
	workspace := ""
	if sel != nil {
		title, workspace = sel.Title, sel.Cwd
	}
	if s != nil {
		if st := s.Title(); st != "" {
			title = st
		}
		if cwd := s.Cwd(); cwd != "" {
			workspace = cwd
		}
	}
	loaded := s != nil && s.LoadState() == session.Loaded

	ui.Column(c).Role(ui.RoleGroup).Label(title).Fill().Children(func() {
		v.header(c, s, sel, loaded, title, workspace, a.subagentNavOf(listed, sel, v.id))
		v.Transcript(c, s, listed)
		v.composerArea(c, s, workspace, loaded)
	})
	v.Zoom(c)
}

// header is the PageHeader: the title with its actions menu, and the
// open-in controls. It drags the window.
func (v *sessionView) header(c *ui.Context, s *session.Session, sel *sessions.Summary, loaded bool, title, workspace string, nav subagentNav) {
	a := v.a
	k, t := a.kit, a.kit.T
	// The web Client's leading spacer, always there, so the header's gap
	// stands before the title too: room for the traffic lights and the
	// sidebar toggle while the sidebar is hidden.
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
	ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(padX).Gap(k.Px(4)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {
		if a.narrow {
			a.openSessionsButton(c)
		} else {
			sp := ui.Box(c).Shrink(0)
			sp.Width(sp.Animate("width", leading, 200*time.Millisecond))
		}
		ui.Row(c).Grow(1).MinWidth(0).Gap(k.Px(4)).Children(func() {
			if v.renaming {
				v.renameField(c, title)
				return
			}
			if len(nav.trail) > 0 {
				v.sessionTrail(c, nav, title)
				return
			}
			group := ui.Row(c).MinWidth(0).Gap(k.Px(4))
			hover := group.Hovered()
			group.Children(func() {
				k.IconStroke(c, "message-square", 1.5, 14, t.MutedForeground)
				k.Text(c, title, k.Dense(12, 14), k.Dense(18, 20)).Role(ui.RoleHeading).FontWeight(500).TextColor(t.Foreground).SingleLine().Shrink(1)
				v.sessionActions(c, sel, title, workspace, hover)
			})
		})
		ui.Row(c).Gap(k.Px(2)).Shrink(0).Children(func() {
			v.jobsMenu(c)
			v.subagentMenu(c, nav)
			v.gitButton(c, s, loaded)
			v.openInButton(c, workspace)
		})
	})
}

// sessionActions is the menu by the header's title: rename, pin, copy and
// archive the Session, as its row's context menu in the sidebar does.
// Quiet until the title is hovered.
func (v *sessionView) sessionActions(c *ui.Context, sel *sessions.Summary, title, workspace string, hover bool) {
	a := v.a
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Role(ui.RoleMenuButton).Label(L("Session actions")).Tooltip(L("Session actions")).Expanded(v.actionsOpen).
		Size(k.Px(24), k.Px(24)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
	switch {
	case !hover && !b.Focused() && !v.actionsOpen:
		b.Opacity(0)
	case b.Hovered() || v.actionsOpen:
		b.Background(t.Muted)
	}
	b.Children(func() { k.Icon(c, "ellipsis", 14, t.MutedForeground) })
	if b.Clicked() {
		v.actionsOpen = !v.actionsOpen
	}
	summary := sessions.Summary{SessionID: v.id, Title: title, Cwd: workspace}
	if sel != nil {
		summary = *sel
		summary.Title, summary.Cwd = title, workspace
	}
	separator := func() { ui.Box(c).Height(1).Background(t.Border).Margin(k.Px(4), -k.Px(4)) }
	k.MenuPopup(c, b, &v.actionsOpen, false, L("Session actions"), func() {
		if k.MenuItem(c, &v.actionsOpen, "pencil", L("Rename")).Clicked() {
			v.renaming, v.rename = true, title
		}
		pin, icon := L("Pin"), "pin"
		if prefs.PinnedSessions.Has(a.prefs, v.id) {
			pin, icon = L("Unpin"), "pin-off"
		}
		if k.MenuItem(c, &v.actionsOpen, icon, pin).Clicked() {
			prefs.PinnedSessions.Toggle(a.prefs, v.id)
		}
		separator()
		if k.MenuItem(c, &v.actionsOpen, "copy", L("Copy session ID")).Clicked() {
			c.WriteClipboard(v.id)
		}
		if k.MenuItem(c, &v.actionsOpen, "file-text", L("Copy session details")).Clicked() {
			a.copySessionDetails(summary)
		}
		if sel == nil {
			return
		}
		separator()
		archive, icon := L("Archive"), "archive"
		if sel.ArchivedAt != "" {
			archive, icon = L("Unarchive"), "archive-restore"
		}
		if k.MenuItem(c, &v.actionsOpen, icon, archive).Clicked() {
			if sel.ArchivedAt == "" && sel.Worktree.Ephemeral() {
				a.askArchive(summary)
			} else {
				go a.toggleArchive(summary)
			}
		}
	})
}

func (v *sessionView) renameField(c *ui.Context, title string) {
	a := v.a
	k, t := a.kit, a.kit.T
	in := ui.TextInputBase(c, &v.rename).Label(L("Session title")).AutoFocus().Grow(1).MaxWidth(k.Px(448)).Height(k.Px(28)).
		PaddingX(k.Px(8)).Radius(k.Px(8)).Border(1, t.Border).Background(t.Background).FontSize(k.Px(14)).FontWeight(500).TextColor(t.Foreground)
	save := func() {
		v.renaming = false
		if next := trimOr(v.rename, ""); next != "" && next != title {
			go a.renameSession(v.id, next)
		}
	}
	if in.Submitted() {
		save()
	}
	if in.Shortcut(0, ui.KeyEscape) {
		v.renaming = false
	}
	if k.IconButton(c, "check", L("Save title"), 24).Clicked() {
		save()
	}
	if k.IconButton(c, "x", L("Cancel rename"), 24).Clicked() {
		v.renaming = false
	}
}

// openInApps is the installed apps that open a folder, read once.
var openInApps = sync.OnceValue(func() []host.OpenInApp {
	home, _ := os.UserHomeDir()
	return host.LocateOpenInApps(runtime.GOOS, home, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
})

// appIcons are the apps' icons, read as they are first shown.
var appIcons sync.Map // app id -> *ui.Bitmap (nil when it has none)

func (a *App) appIcon(app host.OpenInApp) *ui.Bitmap {
	if b, ok := appIcons.Load(app.ID); ok {
		bm, _ := b.(*ui.Bitmap)
		return bm
	}
	appIcons.Store(app.ID, (*ui.Bitmap)(nil))
	if a.cfg.FileIcon == nil {
		return nil
	}
	go func() {
		if png, err := a.cfg.FileIcon(app.AppPath, 64); err == nil {
			if bm, err := ui.DecodeBitmap(png); err == nil {
				appIcons.Store(app.ID, bm)
				a.redraw()
			}
		}
	}()
	return nil
}

// preferredApp is the remembered app while it is installed, else the file
// manager (Finder, File Explorer), else the first.
func preferredApp(apps []host.OpenInApp, remembered string) (host.OpenInApp, bool) {
	for _, id := range []string{remembered, "finder", "explorer"} {
		for _, app := range apps {
			if app.ID == id {
				return app, true
			}
		}
	}
	if len(apps) > 0 {
		return apps[0], true
	}
	return host.OpenInApp{}, false
}

// openInButton is Waku's split control: the icon opens the Workspace in
// the preferred app, the chevron lists every installed one.
func (v *sessionView) openInButton(c *ui.Context, workspace string) {
	a := v.a
	k, t := a.kit, a.kit.T
	apps := a.cfg.OpenInApps
	if apps == nil {
		apps = openInApps
	}
	list := apps()
	preferred, ok := preferredApp(list, prefs.OpenInApp.Get(a.prefs))
	if workspace == "" || !ok || a.following() {
		return
	}
	open := func(app host.OpenInApp) {
		prefs.OpenInApp.Set(a.prefs, app.ID)
		go func() { _ = host.OpenIn(list, workspace, app.ID) }()
	}
	appIcon := func(app host.OpenInApp) {
		if bm := a.appIcon(app); bm != nil {
			ui.Image(c, bm).Size(k.Px(16), k.Px(16)).Shrink(0)
		} else {
			k.Icon(c, "folder-open", 16, t.MutedForeground)
		}
	}
	ui.Row(c).Height(k.Px(28)).Radius(k.Px(8)).Border(1, t.Border).Children(func() {
		main := ui.ButtonBase(c).Label(L("Open in %s", preferred.Label)).Tooltip(L("Open in %s", preferred.Label)).FillHeight().PaddingX(k.Px(6)).
			Radius(k.Px(8), 0, 0, k.Px(8)).Cursor(ui.CursorPointer)
		if main.Hovered() {
			main.Background(t.Accent)
		}
		main.Children(func() { appIcon(preferred) })
		if main.Clicked() {
			open(preferred)
		}
		ui.Box(c).Width(1).FillHeight().Background(t.Border)
		more := ui.ButtonBase(c).Label(L("Open in another app")).Tooltip(L("Open in another app")).FillHeight().Width(k.Px(18)).
			Radius(0, k.Px(8), k.Px(8), 0).Cursor(ui.CursorPointer).Expanded(v.openIn)
		if more.Hovered() || v.openIn {
			more.Background(t.Accent)
		}
		more.Children(func() { k.Icon(c, "chevron-down", 12, t.MutedForeground) })
		if more.Clicked() {
			v.openIn = !v.openIn
		}
		k.MenuPopup(c, more, &v.openIn, true, L("Open in"), func() {
			for _, app := range list {
				item := ui.ButtonBase(c.Key(app.ID)).Role(ui.RoleMenuItemRadio).Checked(app.ID == preferred.ID).Label(app.Label).
					Gap(k.Px(8)).Padding(k.Px(6), k.Px(8)).Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
				if item.Hovered() {
					item.Background(t.Accent)
				}
				item.Children(func() {
					appIcon(app)
					k.Text(c, app.Label, 14, 20).TextColor(t.PopoverForeground).SingleLine().Grow(1)
					ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Children(func() {
						if app.ID == preferred.ID {
							k.Icon(c, "check", 14, t.PopoverForeground)
						}
					})
				})
				if item.Clicked() {
					v.openIn = false
					open(app)
				}
			}
		})
	})
}
