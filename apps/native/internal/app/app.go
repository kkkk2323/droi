// Package app is the native window: the sidebar of Sessions, the Session
// view with its transcript and composer, the New session page and
// Settings, drawn in MyGo to look as the web Client does (apps/desktop/src/
// renderer). It talks to the Daemon through the Go SDK's Controller; the
// Host (package host) starts the Daemon and supplies the credential.
package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/compaction"
	"github.com/kkkk2323/droi/apps/native/internal/drafts"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/theme"
)

// LoadedMessageLimit is how many of a Session's latest messages a load
// brings, as the web Client's LOADED_MESSAGE_LIMIT.
const LoadedMessageLimit = 400

// sessionPage is how many Sessions one daemon.list_available_sessions reads.
const sessionPage = 100

// Route is the page the main panel shows.
type Route struct {
	Name      string // "home", "new", "session", "settings"
	SessionID string
	Workspace string
	Scratch   bool
	Tab       string
}

// Config is what the window needs from main: a Controller to the Daemon,
// the preferences file and the System Prompt Addition for new Sessions.
type Config struct {
	Controller   *controller.Controller
	Prefs        *prefs.Store
	SystemPrompt func() []byte
	// Invalidate asks the window for a frame after state changed on
	// another goroutine; Update runs fn on the main thread and draws.
	Update func(fn func())
	// Now is the clock, for the sidebar's relative times; time.Now when nil.
	Now func() time.Time
	// InsetTop: the traffic lights sit over the sidebar's top strip.
	InsetTop bool
	// FactoryHome is ~/.factory, where the Daemon writes transcripts.
	FactoryHome string
	// OpenPath, ShowInFolder and Beep reach the desktop; nil in tests.
	OpenPath     func(path string) error
	ShowInFolder func(path string)
	// OpenInApps lists the apps that open a folder; the installed ones when nil.
	OpenInApps func() []host.OpenInApp
	// Scratch makes the folders of Sessions started without a Workspace.
	Scratch *host.Scratch
	// Host is the Daemon's supervisor and the login, for Settings; nil in
	// tests, which leaves out the sections that need it.
	Host *host.Host
	// Version is the app's version, for About.
	Version string
	// Env reads the environment, for settings it can override.
	Env func(string) string
	// PlaySound plays an alert sound: a built-in name, "bell", or a file.
	PlaySound func(sound string)
	// Notify shows a desktop notification; nil shows none.
	Notify func(Alert)
	// Focused reports whether the window has the focus.
	Focused func() bool
	// ReadImage is the clipboard's image as PNG, nil without one.
	ReadImage func() []byte
}

// App is the window's state, which lasts from frame to frame.
type App struct {
	cfg   Config
	ctl   *controller.Controller
	prefs *prefs.Store
	ctx   context.Context

	kit   *kit.Kit
	route Route

	// Guarded by mu: written by the Controller's goroutines.
	mu       sync.Mutex
	status   controller.Status
	listed   []sessions.Summary
	listErr  string
	listDone bool
	more     *float64
	busy     map[string]string // working states the Daemon reported
	dirty    bool

	sidebar  sidebarState
	views    map[string]*sessionView
	settings settingsState
	newPage  newSessionState

	drafts      *drafts.Drafts
	pending     drafts.Pending
	compactions *compaction.Log
	// unread are the Sessions that finished or started waiting while
	// another one was open; opening one reads it. Main thread only.
	unread map[string]bool
	// everConnected: the Controller connected once; until then the Daemon
	// is starting up. Guarded by mu.
	everConnected bool
	// droidUpdateDismissed closes the "droid was updated" card until the
	// next update.
	droidUpdateDismissed bool
	// storeRev counts the Store's changes, for the views to build again.
	storeRev atomic.Int64

	listOnce sync.Once
}

// New makes the window's state; Start connects it.
func New(cfg Config) *App {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Update == nil {
		cfg.Update = func(fn func()) { fn() }
	}
	if cfg.Prefs == nil {
		cfg.Prefs = prefs.Memory()
	}
	a := &App{
		cfg:   cfg,
		ctl:   cfg.Controller,
		prefs: cfg.Prefs,
		ctx:   context.Background(),
		route: Route{Name: "home"},
		busy:  map[string]string{},
		views: map[string]*sessionView{},

		drafts:      drafts.New(cfg.Prefs),
		compactions: compaction.NewLog(),
	}
	a.applyPrefs()
	return a
}

func (a *App) applyPrefs() {
	a.kit = kit.New(theme.Parse(prefs.Theme.Get(a.prefs)), theme.FontChoice(prefs.Font.Get(a.prefs)), theme.TextSize(prefs.TextSize.Get(a.prefs)), prefs.Zoom.Get(a.prefs))
}

// Start subscribes to the Controller and reads the Session list once
// connected. Call it once the Controller is connecting.
func (a *App) Start() {
	if a.ctl == nil {
		return
	}
	a.ctl.Subscribe(a.onEvent)
	a.watchAlerts()
	a.ctl.Store().Subscribe(func(e session.Event) {
		a.storeRev.Add(1)
		if e.Kind == session.EventWorkingStateChanged || e.Kind == session.EventSettingsUpdated || e.Kind == session.EventMetadataUpdated {
			a.touch(e.SessionID)
		}
		a.redraw()
	})
	a.mu.Lock()
	a.status = a.ctl.Status()
	a.mu.Unlock()
	if a.status.Connected {
		go a.refreshList()
	}
}

// redraw asks for a frame from any goroutine.
func (a *App) redraw() { a.cfg.Update(func() {}) }

func (a *App) onEvent(e controller.Event) {
	switch ev := e.(type) {
	case controller.StatusChanged:
		a.mu.Lock()
		a.status = ev.Status
		a.everConnected = a.everConnected || ev.Status.Connected
		a.mu.Unlock()
		if ev.Status.Connected {
			go a.refreshList()
		}
	case controller.SessionNotification:
		if ev.Raw.Type == "droid_working_state_changed" {
			var n struct {
				NewState string `json:"newState"`
			}
			_ = jsonUnmarshal(ev.Raw.Raw, &n)
			a.mu.Lock()
			a.busy[ev.SessionID] = n.NewState
			a.mu.Unlock()
			if n.NewState == "idle" {
				go a.refreshList()
			}
		}
		if ev.Raw.Type == "session_title_updated" {
			go a.refreshList()
		}
		switch ev.Raw.Type {
		case "tool_result":
			// The Daemon edits files while a turn runs.
			id := ev.SessionID
			a.cfg.Update(func() {
				if v := a.views[id]; v != nil {
					v.gitStale()
				}
			})
		case "mcp_status_changed", "mcp_auth_required", "mcp_auth_completed":
			id := ev.SessionID
			a.cfg.Update(func() {
				if v := a.views[id]; v != nil {
					v.toolsStale()
				}
			})
		}
	}
	a.redraw()
}

// touch moves a Session that just did something to the top of its group,
// as the web Client's activeAt does until the list is read again.
func (a *App) touch(id string) {
	now := a.cfg.Now().Unix()
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.listed {
		if a.listed[i].SessionID == id && a.listed[i].UpdatedAt < now {
			if s := a.ctl.Store().Session(id); s != nil && s.WorkingState() != protocol.DroidWorkingStateIdle {
				a.listed[i].UpdatedAt = now
			}
		}
	}
}

// refreshList reads the first page of Sessions again.
func (a *App) refreshList() {
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	limit := float64(sessionPage)
	show := prefs.ShowArchived.Get(a.prefs)
	res, err := cl.ListAvailableSessions(a.ctx, protocol.ListAvailableSessionsParams{Limit: &limit, IncludeArchived: &show})
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listDone = true
	if err != nil {
		a.listErr = err.Error()
		return
	}
	a.listErr = ""
	a.listed = summariesOf(res.Sessions)
	a.more = nil
	if res.HasMore {
		a.more = res.NextCursor
	}
	go a.redraw()
}

// loadOlder reads the next page of Sessions.
func (a *App) loadOlder() {
	a.mu.Lock()
	cursor := a.more
	a.mu.Unlock()
	if cursor == nil {
		return
	}
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	limit := float64(sessionPage)
	show := prefs.ShowArchived.Get(a.prefs)
	res, err := cl.ListAvailableSessions(a.ctx, protocol.ListAvailableSessionsParams{Limit: &limit, IncludeArchived: &show, EndBefore: cursor})
	if err != nil {
		return
	}
	a.mu.Lock()
	a.listed = append(a.listed, summariesOf(res.Sessions)...)
	a.more = nil
	if res.HasMore {
		a.more = res.NextCursor
	}
	a.mu.Unlock()
	a.redraw()
}

func summariesOf(list []protocol.DaemonAvailableSessionInfo) []sessions.Summary {
	out := make([]sessions.Summary, 0, len(list))
	for _, s := range list {
		tags := make([]sessions.Tag, len(s.Tags))
		for i, t := range s.Tags {
			tags[i] = sessions.Tag{Name: t.Name, Metadata: t.Metadata}
		}
		if sessions.IsDraft(tags) || sessions.IsMemory(tags) {
			continue
		}
		sum := sessions.Summary{
			SessionID:        s.SessionID,
			Title:            trimOr(s.Title, "Untitled session"),
			Cwd:              s.Cwd,
			RepoRoot:         s.RepoRoot,
			UpdatedAt:        int64(s.UpdatedAt),
			ArchivedAt:       s.ArchivedAt,
			Tags:             tags,
			ParentID:         sessions.ContinuationParent(tags),
			CallingSessionID: s.CallingSessionID,
			CallingToolUseID: s.CallingToolUseID,
		}
		if s.MessagesCount != nil {
			n := int(*s.MessagesCount)
			sum.MessagesCount = &n
		}
		out = append(out, sum)
	}
	return out
}

// snapshot copies what other goroutines write, for one frame.
func (a *App) snapshot() (controller.Status, []sessions.Summary, map[string]string, string, bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	busy := make(map[string]string, len(a.busy))
	for k, v := range a.busy {
		busy[k] = v
	}
	return a.status, append([]sessions.Summary(nil), a.listed...), busy, a.listErr, a.listDone, a.more != nil
}

// Go shows another page.
func (a *App) Go(r Route) {
	if r.Name == "settings" && a.route.Name != "settings" {
		a.settings.back = a.route
	}
	a.route = r
	if r.Name == "session" {
		delete(a.unread, r.SessionID)
		prefs.LastSessionID.Set(a.prefs, r.SessionID)
		a.view(r.SessionID).open()
	}
	if r.Name == "new" {
		a.newPage.reset(r)
	}
}

// Route is the page shown.
func (a *App) Route() Route { return a.route }

// View builds the window.
func (a *App) View(c *ui.Context) {
	c.SetTheme(a.kit.UITheme(c.Theme()))
	status, listed, busy, listErr, listDone, more := a.snapshot()
	a.restoreLast(listed, listDone)
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		prefs.SidebarVisible.Set(a.prefs, !prefs.SidebarVisible.Get(a.prefs))
	}
	if a.route.Name == "settings" {
		a.settingsPage(c, status)
		return
	}
	t := a.kit.T
	shown := prefs.SidebarVisible.Get(a.prefs)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Sidebar).TextColor(t.Foreground).Children(func() {
		w := float32(0)
		if shown {
			w = 240
		}
		side := ui.Column(c).Width(w).FillHeight().ClipX().Shrink(0).Transition(ui.ElementTransition{Size: true, Duration: 200 * time.Millisecond})
		if !shown {
			side.Invisible()
		}
		side.Children(func() {
			ui.Column(c).Width(240).FillHeight().Children(func() {
				a.sidebarView(c, listed, busy, listErr, listDone, more)
			})
		})
		// The web Client keeps the main panel's left border while the sidebar is hidden.
		main := ui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(t.Background).Clip().BorderWidth(0, 0, 0, 1).BorderColor(t.Border)
		a.mu.Lock()
		starting := startingUp(status, a.everConnected)
		a.mu.Unlock()
		main.Children(func() {
			a.setupBanner(c)
			if !starting {
				a.banner(c, status)
			}
			switch {
			case a.route.Name == "session":
				sel := findSummary(listed, a.route.SessionID)
				a.view(a.route.SessionID).build(c, a, sel, listed)
			case starting && a.cfg.Host != nil && a.cfg.Host.HasCredential():
				// Home with nothing to show yet; the New session page would flash.
				a.startingUpView(c)
			default:
				a.newSessionPage(c, listed, status)
			}
		})
		a.droidUpdatedCard(c)
	})
	// The toggle stays put at the window's top left; the sidebar slides under it.
	ui.Overlay(c, func() {
		left := float32(8)
		if a.cfg.InsetTop {
			left = 76
		}
		label := "Hide sidebar"
		if !shown {
			label = "Show sidebar"
		}
		b := a.kit.IconButton(c, "panel-left", label, 32).Absolute().Left(left).Top(6).Expanded(shown)
		if b.Clicked() {
			prefs.SidebarVisible.Set(a.prefs, !shown)
		}
	})
}

// restoreLast reopens the Session that was open last, once the first list
// confirms it still exists; going home on purpose afterwards sticks.
func (a *App) restoreLast(listed []sessions.Summary, listDone bool) {
	if !listDone {
		return
	}
	a.listOnce.Do(func() {
		if a.route.Name != "home" {
			return
		}
		if last := prefs.LastSessionID.Get(a.prefs); last != "" && findSummary(listed, last) != nil {
			a.Go(Route{Name: "session", SessionID: last})
		}
	})
}

func findSummary(list []sessions.Summary, id string) *sessions.Summary {
	for i := range list {
		if list[i].SessionID == id {
			return &list[i]
		}
	}
	return nil
}

// banner says the connection dropped, as the web Client's ReconnectingBanner.
func (a *App) banner(c *ui.Context, status controller.Status) {
	if status.Connected || (!status.Reconnecting && status.Failure == nil) {
		return
	}
	t, k := a.kit.T, a.kit
	text := "Reconnecting to Droid…"
	if !status.Reconnecting && status.Failure != nil {
		text = "Not connected to Droid: " + status.Failure.Error()
	}
	ui.Row(c).Role(ui.RoleStatus).Gap(k.Px(8)).Padding(k.Px(6), k.Px(16)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).
		Background(t.Attention.Alpha(0.1)).Children(func() {
		if status.Reconnecting {
			k.Spinner(c, 14, t.AttentionForeground)
		}
		k.Text(c, text, 12, 16).TextColor(t.AttentionForeground)
	})
}
