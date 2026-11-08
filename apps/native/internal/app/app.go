// Package app is the native window: the sidebar of Sessions, the Session
// view with its transcript and composer, the New session page and
// Settings, drawn in MyGo to look as the web Client does (apps/desktop/src/
// renderer). It talks to the Daemon through the Go SDK's Controller; the
// Host (package host) starts the Daemon and supplies the credential.
package app

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/alerts"
	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/internal/titles"
	"github.com/kkkk2323/droi/apps/native/internal/updates"
	"github.com/kkkk2323/droi/apps/native/pkg/compaction"
	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/drafts"
	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
	"github.com/kkkk2323/droi/apps/native/pkg/settingsview"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// LoadedMessageLimit is how many of a Session's latest messages a load
// brings, as the web Client's LOADED_MESSAGE_LIMIT.
const LoadedMessageLimit = sessionview.LoadedMessageLimit

// sessionPage is how many Sessions one daemon.list_available_sessions reads.
const sessionPage = 100

// Route is the page the main panel shows.
type Route struct {
	Name      string // "home", "new", "session", "settings", "automations", "page"
	SessionID string
	// Page is the id of the extension's Page that "page" shows.
	Page      string
	Workspace string
	Scratch   bool
	Tab       string
	// Automation is the one the Automations page shows, "" for the list.
	Automation string
	// Factory is the Factory automation the Automations page shows.
	Factory string
}

// Config is what the window needs from main: a Controller to the Daemon,
// the preferences file and the System Prompt Addition for new Sessions.
type Config struct {
	Controller *controller.Controller
	Prefs      *prefs.Store
	// Invalidate asks the window for a frame after state changed on
	// another goroutine; Update runs fn on the main thread and draws.
	Update func(fn func())
	// Now is the clock, for the sidebar's relative times; time.Now when nil.
	Now func() time.Time
	// InsetTop: the traffic lights sit over the sidebar's top strip.
	InsetTop bool
	// StatsFile keeps each Session's speed and time across runs; "" keeps
	// them in memory, as in tests.
	StatsFile string
	// OpenPath, ShowInFolder and Beep reach the desktop; nil in tests.
	OpenPath     func(path string) error
	ShowInFolder func(path string)
	// OpenInApps lists the apps that open a folder; the installed ones when nil.
	OpenInApps func() []host.OpenInApp
	// FileIcon is an app's icon as a PNG of size px; nil in tests, which
	// show the apps without icons.
	FileIcon func(path string, size int) ([]byte, error)
	// Gateway is the Client's side of the Gateway's plain endpoints: the
	// Scratch Workspaces of Sessions started without a Workspace and where
	// a Session's transcript is. It is made from the address and token the
	// Daemon socket uses; nil without a Gateway.
	Gateway *gateway.Client
	// Computer is the other computer the window follows, from the Computer
	// preference; nil for this computer. The Controller and Gateway are then
	// those of that computer's Gateway.
	Computer *computer.Paired
	// Switch builds the window's insides again for the computer to follow,
	// nil for this computer, once the Computer preference names it: a new
	// Controller, Gateway Client and App, in place of this App, which it
	// closes. The window stays open; Droi does not relaunch.
	Switch func(to *computer.Paired)
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
	// PickFolder asks for a folder, "" for none; the system's dialog when nil.
	PickFolder func(title string) string
	// ReadImage is the clipboard's image as PNG, nil without one.
	ReadImage func() []byte
	// ReadFiles is the paths of the files on the clipboard, nil without any.
	ReadFiles func() []string
	// ModelCalls stands in for the Key Pool's measure of a Session's model
	// calls, in tests; nil reads the Host's Key Pool.
	ModelCalls func(id string) (diag.SessionCalls, *diag.LiveCall, bool)
	// Updater is the in-app update, nil where the app cannot update
	// itself (a development build); Relaunch starts the installed one.
	Updater  *updates.Updater
	Relaunch func()
	// KernelID resolves a Session the window names to the Kernel conversation that holds it; nil, or false, for
	// a Session the Daemon still holds. Pages of the Sessions it resolves draw from the Kernel the Host hands over
	// with UseKernel.
	KernelID func(id string) (kernel.ID, bool)
	// KernelSession is KernelID's inverse: the Session the window names a conversation by, for what Kernel says
	// of a conversation by its id alone (that it finished, say).
	KernelSession func(id kernel.ID) (string, bool)
	// Memory is the Host's Memory; nil without a Host.
	Memory Memory
	// SessionList, optional, hears the Workspace order the window shows for
	// this computer, for the Gateway to give paired phones.
	SessionList *SessionListShare
	// Remote is the Gateway for paired phones; nil without a Host.
	Remote Remote
	// AutomationsChanged, optional, calls fn when the Host's Automations or
	// their runs change, and returns a function that stops it; nil for
	// another computer, whose Automations the page reads again now and then.
	AutomationsChanged func(fn func()) (unsubscribe func())
	// WorkstreamsChanged, optional, calls fn when what the Host's Workstreams
	// list changes, and returns a function that stops it; nil for another
	// computer, whose Workstreams the window reads again now and then.
	WorkstreamsChanged func(fn func()) (unsubscribe func())
	// MenuBar is the shell's menu bar item of the Key Pool; nil where there
	// is none, which leaves its setting out.
	MenuBar *MenuBar
	// Pages are an app's own pages, in the sidebar under Automations.
	Pages []extension.Page
	// SettingsTabs are an app's own Settings sections.
	SettingsTabs []extension.SettingsTab
	// SignIn, optional, is an app's own sign-in page, drawn in place of
	// Droi's (extension.Extension.SignIn).
	SignIn func(c *ui.Context, env extension.Env)
	// HiddenSettings are the ids of Droi's Settings sections the app leaves
	// out; About stays.
	HiddenSettings []string
	// Presets, optional, are an app's defaults for a new install, given to
	// this computer's Daemon and this device once per Version.
	Presets *defaults.Presets
}

// App is the window's state, which lasts from frame to frame.
type App struct {
	cfg   Config
	ctl   *controller.Controller
	prefs *prefs.Store
	// ctx ends with Close, and with it every call the App still makes.
	ctx    context.Context
	cancel context.CancelFunc
	// unsubscribe are Start's subscriptions, which Close ends.
	unsubscribe []func()

	kit   *kit.Kit
	route Route
	// notesMD shows the release notes.
	notesMD sessionview.Markdown
	// locale is the system's (SetLocale); "" keeps the last one told.
	locale string
	// languageChanged hears a change of the language, for what the window
	// does not draw, such as the menu bar.
	languageChanged func()

	// Guarded by mu: written by the Controller's goroutines.
	mu       sync.Mutex
	status   controller.Status
	listed   []sessions.Summary
	listErr  string
	listDone bool
	// listGen counts the reads of the first page, so that a slower, older
	// read does not overwrite a newer one's list.
	listGen uint64
	// listRev counts the changes to listed, for what is built from it.
	listRev uint64
	// listCopy is listed at listCopyRev, which every frame gets until the
	// list changes: copying thousands of Sessions per frame was most of
	// what a frame allocated. Frames and searches only read it.
	listCopy    []sessions.Summary
	listCopyRev uint64
	// pageIDs are the Sessions of the first page last read.
	pageIDs map[string]bool
	// older is how far the pages after the first are read; olderGen counts
	// their reads, so that one made stale is dropped.
	older    olderState
	olderGen uint64
	busy     map[string]string // working states the Daemon reported
	dirty    bool
	// times are the Session Times the window follows.
	times timesState
	// seen are the "seen" times on their way to Kernel.
	seen seenWrites
	// unlisted are the Sessions that started working before the list had
	// them, a run of an Automation or a subagent, that readUntilListed reads
	// the list for.
	unlisted map[string]bool
	// asked are the Kernel conversations the list was read for because Kernel named one it did not have; each is
	// read for once until the list is read whole again.
	asked map[string]bool
	// reading are the reads of the list still out, by listGen, each with the indexSeq it started at; indexLog are
	// the index changes applied since the oldest of them began (list_kernel.go).
	reading  map[uint64]uint64
	indexSeq uint64
	indexLog []indexEvent

	sidebar sidebarState
	views   map[string]*sessionView
	// recent are the Sessions whose view is kept, opened last at the end.
	recent   []string
	settings settingsState
	newPage  newSessionState
	// defaults are the Session Defaults, which Settings and the New session
	// page share.
	defaults *settingsview.Defaults
	// presetting is held while applyPresets saves the Presets.
	presetting sync.Mutex
	// shownRev is the listRev of the list this frame shows; listMemo
	// keeps what is built from it.
	shownRev uint64
	// subagentKey, subagentRev, subagentFirstSeen and known are
	// knownSubagents'.
	subagentKey       string
	subagentRev       uint64
	subagentFirstSeen map[string]int64
	known             knownList
	listMemo          listMemo

	drafts      *drafts.Drafts
	pending     drafts.Pending
	compactions *compaction.Log
	// unread are the Sessions that finished or started waiting while
	// another one was open; opening one reads it. The Session Times say
	// instead, when the Gateway keeps them (unreadSessions). Main thread only.
	unread map[string]bool
	// everConnected: the Controller connected once; until then the Daemon
	// is starting up. Guarded by mu.
	everConnected bool
	// droidUpdateDismissed closes the "droid was updated" card until the
	// next update.
	droidUpdateDismissed bool
	// droiUpdateDismissed is the Droi's droid version whose update card
	// was closed.
	droiUpdateDismissed string
	signIn              signInState
	accounts            accountsState
	elsewhere           elsewhereState
	// updateDismissed is the update step whose card was closed.
	updateDismissed string
	memory          memoryState
	automations     automationsState
	loopAutos       loopAutomations
	workstreams     workstreamsState
	usage           usageState
	// narrow is a window under the web Client's md breakpoint, where the
	// sidebar is a drawer; drawerOpen whether it is out.
	narrow, drawerOpen bool
	remote             remoteState
	// revs counts each Session's changes in the Store, for its view to
	// build again.
	revs sessionview.Revisions
	// kernel is the Kernel connection UseKernel hands over; sessions are the conversations the pages have open on
	// it.
	kernel   kernelConn
	sessions *sessionview.Store
	// kernelAlerts follows the busy and idle of Kernel's conversations, as watchAlerts follows the Daemon's
	// working states.
	kernelAlerts *alerts.Tracker
	// approvals are the requests each Kernel Session waits on, by Session id, for its mark and alert. Guarded by mu.
	approvals map[string]map[string]bool
	// policy is the approval policy as the window gives it to Kernel.
	policy policyState
	// titles names the conversations a person started once their first round ends. It belongs with the Host,
	// and moves there when the Host holds the Kernel connection itself.
	titles *titles.Manager
	// loops are the Daemon's loops. Guarded by mu.
	loops loopsState
	// worktreeAsk asks before a worktree is deleted. Main thread only.
	worktreeAsk worktreeDialog

	// copyText is what the next frame puts on the clipboard: a copy that had
	// to ask the Gateway first. Main thread only.
	copyText string

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
	if cfg.Computer != nil {
		// This computer's Host, Memory, Gateway and Automations are not the
		// followed computer's, so the views that need one are left out.
		cfg.Host, cfg.Memory, cfg.Remote, cfg.AutomationsChanged, cfg.WorkstreamsChanged = nil, nil, nil, nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		cfg:    cfg,
		ctl:    cfg.Controller,
		prefs:  cfg.Prefs,
		ctx:    ctx,
		cancel: cancel,
		route:  Route{Name: "home"},
		busy:   map[string]string{},
		views:  map[string]*sessionView{},

		drafts:       drafts.New(cfg.Prefs),
		notesMD:      sessionview.Markdown{Own: true},
		compactions:  compaction.NewLog(),
		sessions:     sessionview.NewStore(keptViews),
		kernelAlerts: alerts.NewTracker(),
	}
	a.titles = titles.New(titles.Config{Source: &a.kernel, TitleSource: a.titleSource})
	a.unsubscribe = append(a.unsubscribe, a.sessions.Subscribe(func(id kernel.ID) {
		a.heardApprovals(id)
		a.redraw()
	}))
	a.defaults = &settingsview.Defaults{Controller: cfg.Controller, Ctx: ctx, Update: cfg.Update, Changed: a.defaultsChanged}
	pickFolder := a.pickFolder
	if cfg.Computer != nil {
		// Another computer's Daemon cannot use a folder picked on this one.
		pickFolder = nil
	}
	a.settings.Settings = settingsview.New(settingsview.Env{
		Kit:        func() *kit.Kit { return a.kit },
		Prefs:      cfg.Prefs,
		Update:     cfg.Update,
		Defaults:   a.defaults,
		Failed:     func(msg string) { a.settings.err = msg },
		PickFolder: pickFolder,
	})
	a.applyPrefs()
	if cfg.Updater != nil {
		cfg.Updater.Skip(prefs.SkippedUpdate.Get(cfg.Prefs))
		cfg.Updater.SetAutomatic(autoUpdate(cfg))
	}
	return a
}

// autoUpdate is About's "Install updates automatically": the user's own
// choice, else the app's Presets.
func autoUpdate(cfg Config) bool {
	p := prefs.AutoUpdate
	p.Fallback = cfg.Presets != nil && cfg.Presets.AutoUpdate
	return p.Get(cfg.Prefs)
}

func (a *App) applyPrefs() {
	a.kit = kit.New(theme.Parse(prefs.Theme.Get(a.prefs)), theme.FontChoice(prefs.Font.Get(a.prefs)), theme.TextSize(prefs.TextSize.Get(a.prefs)), prefs.Zoom.Get(a.prefs))
	a.kit.Compact = theme.Density(prefs.Density.Get(a.prefs)) == theme.DensityCompact
	if l10n.Set(prefs.Language.Get(a.prefs), a.locale) && a.languageChanged != nil {
		a.languageChanged()
	}
}

// SetLanguageChanged sets what hears a change of the app's language, on
// the main thread.
func (a *App) SetLanguageChanged(fn func()) { a.languageChanged = fn }

// SetLocale tells the system's locale, such as "zh-CN", which the app's
// language follows unless Settings picks one. Main thread only.
func (a *App) SetLocale(locale string) {
	a.locale = locale
	a.applyPrefs()
}

// Start subscribes to the Controller and reads the Session list once
// connected. Call it once the Controller is connecting.
func (a *App) Start() {
	if a.ctl == nil {
		return
	}
	a.unsubscribe = append(a.unsubscribe, a.ctl.Subscribe(a.onEvent), a.watchAlerts())
	stats := openSessionStats(a.cfg.StatsFile)
	a.ctl.Store().SetStatsSeed(stats.get)
	a.unsubscribe = append(a.unsubscribe, a.ctl.Store().Subscribe(func(e session.Event) {
		a.revs.Bump(e.SessionID)
		if e.Kind == session.EventStatsUpdated {
			if s := a.ctl.Store().Session(e.SessionID); s != nil {
				stats.put(e.SessionID, s.Stats())
			}
		}
		a.redraw()
	}))
	a.mu.Lock()
	a.status = a.ctl.Status()
	a.mu.Unlock()
	if a.status.Connected {
		go a.refreshList()
		go a.readLoops()
		go a.applyPresets()
	}
	if a.cfg.Gateway != nil {
		go a.followTimes()
	}
	if a.cfg.AutomationsChanged != nil {
		a.unsubscribe = append(a.unsubscribe, a.cfg.AutomationsChanged(func() {
			a.cfg.Update(func() {
				a.loopAutos.at = time.Time{}
				if a.route.Name == "automations" {
					a.automationsChanged()
				} else {
					a.automations.listAt = time.Time{}
					clear(a.automations.runsAt)
				}
			})
		}))
	}
	if a.cfg.WorkstreamsChanged != nil {
		a.unsubscribe = append(a.unsubscribe, a.cfg.WorkstreamsChanged(func() {
			a.cfg.Update(func() { a.loadWorkstreams(true) })
		}))
	}
}

// Close stops the App once the window shows another: it hears its
// Controller no more and the calls it makes end. The Controller is the
// caller's to close, after this. Main thread only.
func (a *App) Close() {
	a.flushSeen()
	a.cancel()
	for _, fn := range a.unsubscribe {
		fn()
	}
	a.unsubscribe = nil
}

// closed reports whether Close was called, for work that ends after it.
func (a *App) closed() bool { return a.ctx.Err() != nil }

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
			go a.reloadList()
			go a.readLoops()
			go a.applyPresets()
		}
	case controller.SpecHandoffStarted:
		// The plan goes on in a new Session: follow it there, unless the
		// user went elsewhere meanwhile.
		go a.refreshList()
		a.cfg.Update(func() {
			if a.route.Name == "session" && a.route.SessionID == ev.SessionID {
				a.Go(Route{Name: "session", SessionID: ev.NewSessionID})
			}
		})
	case controller.SpecHandoffFailed:
		a.cfg.Update(func() {
			if v := a.views[ev.SessionID]; v != nil && ev.Err != nil {
				v.SetErr(L("The new session did not start: %s", ev.Err.Error()))
			}
		})
	case controller.DaemonNotification:
		switch ev.Method {
		case protocol.NotificationWorktreeRemoved, protocol.NotificationWorktreeBranchChanged:
			go a.reloadList()
		case protocol.NotificationCronStateChanged:
			a.loopsChanged(ev.Params)
		}
	case controller.SessionNotification:
		if ev.Raw.Type == "droid_working_state_changed" {
			var n struct {
				NewState string `json:"newState"`
			}
			_ = jsonUnmarshal(ev.Raw.Raw, &n)
			a.mu.Lock()
			a.busy[ev.SessionID] = n.NewState
			fresh := n.NewState != "idle" && a.listDone && !a.unlisted[ev.SessionID] && findSummary(a.listed, ev.SessionID) == nil
			if fresh {
				if a.unlisted == nil {
					a.unlisted = map[string]bool{}
				}
				a.unlisted[ev.SessionID] = true
			}
			a.mu.Unlock()
			if fresh {
				go a.readUntilListed(ev.SessionID)
			} else if n.NewState == "idle" {
				go a.refreshList()
			}
		}
		if ev.Raw.Type == "child_session_available" {
			go a.refreshList()
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

// Go shows another page.
func (a *App) Go(r Route) {
	if r.Name == "settings" && a.route.Name != "settings" {
		a.settings.back = a.route
	}
	r = a.inWorkstream(r)
	a.workstreams.paneDrawer = false
	a.route = r
	a.usage.shown = false
	// Picking anything in the drawer is the end of the drawer's job.
	a.drawerOpen = false
	if r.Name == "session" {
		delete(a.unread, r.SessionID)
		a.viewed(r.SessionID)
		prefs.LastSessionID.Set(a.prefs, r.SessionID)
		a.elsewhere.confirm = ""
		if acc, ok := a.elsewhere.accounts[r.SessionID]; ok {
			// droid would refuse to load it; it loads once moved here.
			a.takeSession(r.SessionID, acc)
		} else {
			delete(a.elsewhere.unopened, r.SessionID)
			a.view(r.SessionID).Open()
			a.remember(r.SessionID)
		}
	}
	if r.Name == "new" {
		a.newPage.reset(r)
	} else {
		a.dropDraft()
	}
	if r.Name == "automations" {
		a.enterAutomations(r)
	}
}

// Route is the page shown.
func (a *App) Route() Route { return a.route }

// View builds the window.
func (a *App) View(c *ui.Context) {
	c.SetTheme(a.kit.UITheme(c.Theme()))
	if a.copyText != "" {
		c.WriteClipboard(a.copyText)
		a.copyText = ""
	}
	status, listed, busy, listErr, listDone, rev := a.snapshot()
	listed, rev = a.knownSubagents(listed, rev)
	listed, rev = a.withElsewhere(listed, rev)
	a.shownRev = rev
	a.restoreLast(listed, listDone)
	doing := a.activity(busy)
	a.noteLiveRuns(listed, doing)
	a.noteWorkstreams(c, listed, doing)
	a.narrow = a.isNarrow(c)
	// A drawer only exists on narrow screens; widening the window closes it.
	if !a.narrow {
		a.drawerOpen = false
	}
	a.sidebar.numbered = a.sidebar.numbered[:0]
	a.worktreeDialogView(c)
	if a.route.Name == "settings" {
		a.settingsPage(c, status)
		return
	}
	if a.needsSignIn() {
		a.signInPage(c)
		return
	}
	t := a.kit.T
	shown := a.sidebarShown()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Sidebar).TextColor(t.Foreground).Children(func() {
		// edge is where the sidebar ends once it has slid all the way open.
		edge := float32(0)
		if !a.narrow {
			target := float32(0)
			if shown {
				target = 1
			}
			// Animated here, not with a Transition: that moves the box after
			// the layout, so the main panel would jump to its new width. The
			// share shown animates, not the width, so that the width follows
			// a drag of the edge at once.
			side := ui.Column(c).FillHeight().ClipX().Shrink(0)
			open := side.Animate("open", target, 200*time.Millisecond)
			width := a.sidebarWidth()
			side.Width(open * width)
			// As the web Client's visibility flip: once the slide is done.
			if open == 0 {
				side.Invisible()
			}
			if open == 1 {
				edge = width
			}
			side.Children(func() {
				ui.Column(c).Width(width).FillHeight().Children(func() {
					a.sidebarView(c, listed, busy, listErr, listDone)
				})
			})
		} else {
			a.drawer(c, func() { a.sidebarView(c, listed, busy, listErr, listDone) })
		}
		// The web Client keeps the main panel's left border while the sidebar is hidden.
		main := ui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(t.Background).Clip().BorderWidth(0, 0, 0, 1).BorderColor(t.Border)
		a.mu.Lock()
		starting := startingUp(status, a.everConnected)
		a.mu.Unlock()
		main.Children(func() {
			if !starting {
				a.banner(c, status)
			}
			switch {
			case a.route.Name == "session":
				id := a.route.SessionID
				if acc, sel, ok := a.elsewhereOf(id, listed); ok {
					a.elsewhereView(c, id, sel, acc, len(a.activity(busy)))
					break
				}
				a.sessionPage(c, id, listed)
			case a.route.Name == "automations":
				a.automationsPage(c)
			case a.route.Name == "page" && a.extensionPage(a.route.Page) != nil:
				a.extensionPageView(c, a.extensionPage(a.route.Page))
			case starting && a.following():
				a.followingStartView(c, status)
			case starting && a.cfg.Host != nil && a.cfg.Host.HasCredential():
				// Home with nothing to show yet; the New session page would flash.
				a.startingUpView(c, status)
			default:
				a.newSessionPage(c, listed, status, listDone)
			}
		})
		a.cornerCards(c)
		if edge > 0 {
			a.sidebarEdge(c, edge)
		}
	})
	a.newWorkstreamDialog(c)
	a.workstreamSettingsDialog(c)
	a.archiveWorkstreamDialog(c)
	// After the sidebar, which numbers this frame's Sessions.
	for n, key := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		if c.Shortcut(ui.Cmd, key) {
			a.Run(sessionCommand(n + 1))
		}
	}
	if a.narrow {
		return
	}
	// The toggle stays put at the window's top left; the sidebar slides under it.
	ui.Overlay(c, func() {
		left := float32(8)
		if a.cfg.InsetTop {
			left = 76
		}
		label := L("Hide sidebar")
		if !shown {
			label = L("Show sidebar")
		}
		b := a.kit.IconButton(c, "panel-left", label, 32).Tooltip(withShortcut(label, CmdToggleSidebar)).Absolute().Left(left).Top(6).Expanded(shown)
		if b.Clicked() {
			a.Run(CmdToggleSidebar)
		}
	})
}

// The sidebar's edge resizes it between these widths, in px of the default
// text size, as prefs.SidebarWidth keeps it.
const (
	sidebarMinWidth = 180
	sidebarMaxWidth = 480
)

// sidebarWidth is the width of the sidebar and of Settings' list of
// sections, so going to Settings and back moves nothing: the one its edge
// is being dragged to, else the one saved.
func (a *App) sidebarWidth() float32 {
	w := a.sidebar.width
	if w == 0 {
		w = float32(prefs.SidebarWidth.Get(a.prefs))
	}
	return a.kit.Px(min(max(w, sidebarMinWidth), sidebarMaxWidth))
}

// sidebarEdge is the line between the sidebar, w DIPs wide, and the page.
// Dragged, or moved with the arrows once it has the focus, it resizes the
// sidebar; a double click puts back the default width. A drag is saved as it
// ends, not at each move: the preferences are one file, written whole.
func (a *App) sidebarEdge(c *ui.Context, w float32) {
	k, t := a.kit, a.kit.T
	grip := k.Px(6)
	px := w * 16 / k.Rem
	edge := ui.Box(c).Absolute().Left(w-grip/2).Top(0).Bottom(0).Width(grip).Cursor(ui.CursorResizeEW).
		Role(ui.RoleSplitter).Label(L("Resize sidebar")).Focusable().FocusRing(false).
		Range(sidebarMinWidth, sidebarMaxWidth, math.Round(float64(px))).Step(16)
	// The drag adds up unclamped, so the edge comes back only with the
	// pointer once it was dragged past either end.
	dx, _, held := edge.Dragged()
	switch {
	case held && dx != 0:
		if a.sidebar.width == 0 {
			a.sidebar.width = px
		}
		a.sidebar.width += dx * 16 / k.Rem
	case !held && a.sidebar.width != 0:
		a.saveSidebarWidth(a.sidebar.width)
		a.sidebar.width = 0
	}
	if edge.Shortcut(0, ui.KeyLeft) {
		a.saveSidebarWidth(px - 16)
	}
	if edge.Shortcut(0, ui.KeyRight) {
		a.saveSidebarWidth(px + 16)
	}
	if edge.DoubleClicked() {
		a.sidebar.width = 0
		a.prefs.Remove(prefs.SidebarWidth.Key)
	}
	if held || edge.Hovered() || edge.FocusVisible() {
		// Over the main panel's left border, which starts at w.
		edge.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + grip/2 - 0.5, Y: r.Y, W: 2, H: r.H}, t.Tint, 0)
		})
	}
}

// saveSidebarWidth keeps px, in px of the default text size, as the
// sidebar's width.
func (a *App) saveSidebarWidth(px float32) {
	px = min(max(px, sidebarMinWidth), sidebarMaxWidth)
	prefs.SidebarWidth.Set(a.prefs, math.Round(float64(px)))
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
	text := L("Reconnecting to Droi…")
	if !status.Reconnecting && status.Failure != nil {
		text = L("Not connected to Droi: %s", failureText(status.Failure))
	}
	ui.Row(c).Role(ui.RoleStatus).Gap(k.Px(8)).Padding(k.Px(6), k.Px(16)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).
		Background(t.Attention.Alpha(0.1)).Children(func() {
		if status.Reconnecting {
			k.Spinner(c, 14, t.AttentionForeground)
		}
		k.Text(c, text, 12, 16).TextColor(t.AttentionForeground)
	})
}
