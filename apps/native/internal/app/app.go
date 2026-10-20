// Package app is the native window: the sidebar of Sessions, the Session
// view with its transcript and composer, the New session page and
// Settings, drawn in MyGo to look as the web Client does (apps/desktop/src/
// renderer). It talks to the Daemon through the Go SDK's Controller; the
// Host (package host) starts the Daemon and supplies the credential.
package app

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/internal/updates"
	"github.com/kkkk2323/droi/apps/native/pkg/compaction"
	"github.com/kkkk2323/droi/apps/native/pkg/drafts"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
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
	Name      string // "home", "new", "session", "settings", "automations"
	SessionID string
	Workspace string
	Scratch   bool
	Tab       string
	// Automation is the one the Automations page shows, "" for the list.
	Automation string
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
	// Updater is the in-app update, nil where the app cannot update
	// itself (a development build); Relaunch starts the installed one.
	Updater  *updates.Updater
	Relaunch func()
	// Memory is the Host's Memory; nil without a Host.
	Memory Memory
	// Remote is the Gateway for paired phones; nil without a Host.
	Remote Remote
	// FactoryAppRunning reports whether the Factory App's Daemon runs too,
	// for the Automations page's notice; nil never tells.
	FactoryAppRunning func() bool
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
	// turnAt is when each working Session's turn began, in Unix seconds:
	// it keeps the Session's place in the list until the turn ends.
	turnAt map[string]int64
	// unlisted are the Sessions that started working before the list had
	// them, a run of an Automation or a subagent, that readUntilListed reads
	// the list for.
	unlisted map[string]bool

	sidebar sidebarState
	views   map[string]*sessionView
	// recent are the Sessions whose view is kept, opened last at the end.
	recent   []string
	settings settingsState
	newPage  newSessionState
	// defaults are the Session Defaults, which Settings and the New session
	// page share.
	defaults *settingsview.Defaults
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
	// another one was open; opening one reads it. Main thread only.
	unread map[string]bool
	// everConnected: the Controller connected once; until then the Daemon
	// is starting up. Guarded by mu.
	everConnected bool
	// droidUpdateDismissed closes the "droid was updated" card until the
	// next update.
	droidUpdateDismissed bool
	signIn               signInState
	accounts             accountsState
	elsewhere            elsewhereState
	// updateDismissed is the update step whose card was closed.
	updateDismissed string
	memory          memoryState
	automations     automationsState
	// narrow is a window under the web Client's md breakpoint, where the
	// sidebar is a drawer; drawerOpen whether it is out.
	narrow, drawerOpen bool
	remote             remoteState
	// revs counts each Session's changes in the Store, for its view to
	// build again.
	revs sessionview.Revisions
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
		// This computer's Host, Memory, Gateway and Factory App are not the
		// followed computer's, so the views that need one are left out.
		cfg.Host, cfg.Memory, cfg.Remote, cfg.FactoryAppRunning = nil, nil, nil, nil
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
		turnAt: map[string]int64{},
		views:  map[string]*sessionView{},

		drafts:      drafts.New(cfg.Prefs),
		notesMD:     sessionview.Markdown{Own: true},
		compactions: compaction.NewLog(),
	}
	a.defaults = &settingsview.Defaults{Controller: cfg.Controller, Ctx: ctx, Update: cfg.Update}
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
	}
	return a
}

func (a *App) applyPrefs() {
	a.kit = kit.New(theme.Parse(prefs.Theme.Get(a.prefs)), theme.FontChoice(prefs.Font.Get(a.prefs)), theme.TextSize(prefs.TextSize.Get(a.prefs)), prefs.Zoom.Get(a.prefs))
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
		if e.Kind == session.EventWorkingStateChanged {
			s := a.ctl.Store().Session(e.SessionID)
			a.touch(e.SessionID, s != nil && s.WorkingState() != protocol.DroidWorkingStateIdle)
		}
		a.redraw()
	}))
	a.mu.Lock()
	a.status = a.ctl.Status()
	a.mu.Unlock()
	if a.status.Connected {
		go a.refreshList()
		go a.readLoops()
	}
}

// Close stops the App once the window shows another: it hears its
// Controller no more and the calls it makes end. The Controller is the
// caller's to close, after this. Main thread only.
func (a *App) Close() {
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

// touch moves a Session whose turn begins to the top of its group, as the
// web Client's activeAt does. The working states inside one turn leave it
// where it is: two working Sessions would otherwise swap places on every
// step either takes.
func (a *App) touch(id string, working bool) {
	now := a.cfg.Now().Unix()
	a.mu.Lock()
	defer a.mu.Unlock()
	if !working {
		delete(a.turnAt, id)
		return
	}
	if _, ok := a.turnAt[id]; ok {
		return
	}
	a.turnAt[id] = now
	a.listRev++
	for i := range a.listed {
		if a.listed[i].SessionID == id && a.listed[i].UpdatedAt < now {
			a.listed[i].UpdatedAt = now
		}
	}
}

// holdTurns keeps working Sessions where their turn put them when the
// list is read again: the Daemon's date for one moves with every message
// its turn writes. Call with mu held.
func (a *App) holdTurns(list []sessions.Summary) {
	for i := range list {
		id := list[i].SessionID
		at, ok := a.turnAt[id]
		if !ok {
			continue
		}
		// The list may be read for the end of the turn before touch hears of it.
		if a.ctl != nil {
			if s := a.ctl.Store().Session(id); s == nil || s.WorkingState() == protocol.DroidWorkingStateIdle {
				delete(a.turnAt, id)
				continue
			}
		}
		list[i].UpdatedAt = at
	}
}

type olderState int

const (
	olderUnread olderState = iota
	olderReading
	olderRead
)

// olderPages are the pages a read of the first page leaves: the cursor
// after it, and edge, its oldest Session's date in Unix seconds.
type olderPages struct {
	gen    uint64
	cursor float64
	edge   int64
}

// refreshList reads the first page of Sessions again and keeps the older
// ones read before. The first time, or once reloadList made them stale,
// it reads every page after it as well, which can take a while.
func (a *App) refreshList() {
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	gen := a.startList()
	limit := float64(sessionPage)
	show := prefs.ShowArchived.Get(a.prefs)
	res, err := cl.ListAvailableSessions(a.ctx, protocol.ListAvailableSessionsParams{Limit: &limit, IncludeArchived: &show})
	if p := a.setList(gen, res, err); p != nil {
		a.readOlder(*p, show)
	}
}

// unlistedReads are the waits between reads of the list for a Session
// that works before the Daemon lists it, the last one repeated: the Daemon
// announces a run of an Automation before it is on disk.
var unlistedReads = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}

// readUntilListed reads the list until it has the Session id, the Session
// stops working, or the connection is gone.
func (a *App) readUntilListed(id string) {
	defer func() {
		a.mu.Lock()
		delete(a.unlisted, id)
		a.mu.Unlock()
	}()
	for i := 0; ; i++ {
		if _, err := a.ctl.Client(); err != nil {
			return
		}
		a.refreshList()
		a.mu.Lock()
		done := a.busy[id] == "idle" || findSummary(a.listed, id) != nil
		a.mu.Unlock()
		if done {
			return
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(unlistedReads[min(i, len(unlistedReads)-1)]):
		}
	}
}

// reloadList reads every page again, for a change the first page may not
// show: an older Session archived or renamed, another connection.
func (a *App) reloadList() {
	a.mu.Lock()
	a.older = olderUnread
	a.olderGen++
	a.mu.Unlock()
	a.refreshList()
}

// startList counts a read of the first page, which setList then takes.
func (a *App) startList() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listGen++
	return a.listGen
}

// setList takes the first page of read gen, unless a newer read started,
// with the older Sessions kept. It returns the pages left to read, if
// they are to be read.
func (a *App) setList(gen uint64, res *protocol.ListAvailableSessionsResult, err error) *olderPages {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.listGen {
		return nil
	}
	a.listDone = true
	if err != nil {
		a.listErr = err.Error()
		return nil
	}
	a.listErr = ""
	go a.redraw()
	page := summariesOf(res.Sessions)
	a.holdTurns(page)
	a.pageIDs = make(map[string]bool, len(page))
	for _, s := range page {
		a.pageIDs[s.SessionID] = true
	}
	a.listRev++
	if !res.HasMore || res.NextCursor == nil || len(res.Sessions) == 0 {
		a.listed = page
		a.older = olderRead
		a.olderGen++
		a.keepList()
		return nil
	}
	edge := int64(res.Sessions[0].UpdatedAt)
	for _, s := range res.Sessions {
		edge = min(edge, int64(s.UpdatedAt))
	}
	list := page
	for _, s := range a.listed {
		if !a.pageIDs[s.SessionID] && s.UpdatedAt <= edge {
			list = append(list, s)
		}
	}
	a.listed = list
	if a.older != olderUnread {
		return nil
	}
	a.older = olderReading
	a.olderGen++
	return &olderPages{gen: a.olderGen, cursor: *res.NextCursor, edge: edge}
}

// readOlder reads the pages p leaves, then puts them in place of the
// older Sessions kept until then.
func (a *App) readOlder(p olderPages, show bool) {
	failed := func() {
		a.mu.Lock()
		if a.olderGen == p.gen {
			a.older = olderUnread
		}
		a.mu.Unlock()
	}
	cl, err := a.ctl.Client()
	if err != nil {
		failed()
		return
	}
	var read []protocol.DaemonAvailableSessionInfo
	limit, cursor := float64(sessionPage), p.cursor
	for {
		res, err := cl.ListAvailableSessions(a.ctx, protocol.ListAvailableSessionsParams{Limit: &limit, IncludeArchived: &show, EndBefore: &cursor})
		if err != nil {
			failed()
			return
		}
		a.mu.Lock()
		stale := a.olderGen != p.gen
		a.mu.Unlock()
		if stale {
			return
		}
		read = append(read, res.Sessions...)
		if !res.HasMore || res.NextCursor == nil || *res.NextCursor == cursor {
			break
		}
		cursor = *res.NextCursor
	}
	older := summariesOf(read)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.olderGen != p.gen {
		return
	}
	a.holdTurns(older)
	var list []sessions.Summary
	kept := map[string]bool{}
	for _, s := range a.listed {
		if a.pageIDs[s.SessionID] || s.UpdatedAt >= p.edge {
			list = append(list, s)
			kept[s.SessionID] = true
		}
	}
	for _, s := range older {
		if !kept[s.SessionID] {
			list = append(list, s)
		}
	}
	a.listed = list
	a.listRev++
	a.older = olderRead
	a.keepList()
	go a.redraw()
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
			Title:            trimOr(s.Title, L("Untitled session")),
			Cwd:              s.Cwd,
			RepoRoot:         s.RepoRoot,
			UpdatedAt:        int64(s.UpdatedAt),
			ArchivedAt:       s.ArchivedAt,
			Tags:             tags,
			ParentID:         sessions.ContinuationParent(tags),
			CallingSessionID: s.CallingSessionID,
			CallingToolUseID: s.CallingToolUseID,
			OrganizationID:   s.OrganizationID,
		}
		if s.MessagesCount != nil {
			n := int(*s.MessagesCount)
			sum.MessagesCount = &n
		}
		if w := s.Worktree; w != nil && w.RemovedAt == "" && w.Path != "" {
			sum.Worktree = &sessions.Worktree{Path: w.Path, Branch: w.Branch, RepoRoot: w.RepoRoot, Lifecycle: string(w.Lifecycle)}
		}
		out = append(out, sum)
	}
	return out
}

// snapshot copies what other goroutines write, for one frame.
func (a *App) snapshot() (controller.Status, []sessions.Summary, map[string]string, string, bool, uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	busy := make(map[string]string, len(a.busy))
	for k, v := range a.busy {
		busy[k] = v
	}
	if a.listCopy == nil || a.listCopyRev != a.listRev {
		// Clipped, so that a reader's append makes a new array.
		a.listCopy, a.listCopyRev = slices.Clip(slices.Clone(a.listed)), a.listRev
	}
	return a.status, a.listCopy, busy, a.listErr, a.listDone, a.listRev
}

// Go shows another page.
func (a *App) Go(r Route) {
	if r.Name == "settings" && a.route.Name != "settings" {
		a.settings.back = a.route
	}
	a.route = r
	// Picking anything in the drawer is the end of the drawer's job.
	a.drawerOpen = false
	if r.Name == "session" {
		delete(a.unread, r.SessionID)
		prefs.LastSessionID.Set(a.prefs, r.SessionID)
		a.elsewhere.confirm = ""
		if acc, ok := a.elsewhere.accounts[r.SessionID]; ok {
			// droid would refuse to load it; it loads after the switch.
			a.noteUnopened(r.SessionID, acc)
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
	a.noteLiveRuns(listed, a.activity(busy))
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
		if !a.narrow {
			target := float32(0)
			if shown {
				target = 240
			}
			// Animated here, not with a Transition: that moves the box after
			// the layout, so the main panel would jump to its new width.
			side := ui.Column(c).FillHeight().ClipX().Shrink(0)
			w := side.Animate("width", target, 200*time.Millisecond)
			side.Width(w)
			// As the web Client's visibility flip: once the slide is done.
			if w == 0 {
				side.Invisible()
			}
			side.Children(func() {
				ui.Column(c).Width(240).FillHeight().Children(func() {
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
				a.view(id).build(c, a, findSummary(listed, id), listed)
			case a.route.Name == "automations":
				a.automationsPage(c)
			case starting && a.following():
				a.followingStartView(c, status)
			case starting && a.cfg.Host != nil && a.cfg.Host.HasCredential():
				// Home with nothing to show yet; the New session page would flash.
				a.startingUpView(c, status)
			default:
				a.newSessionPage(c, listed, status)
			}
		})
		a.cornerCards(c)
	})
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
		if shown {
			a.computerMenu(c, left+36)
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
	text := L("Reconnecting to Droid…")
	if !status.Reconnecting && status.Failure != nil {
		text = L("Not connected to Droid: %s", failureText(status.Failure))
	}
	ui.Row(c).Role(ui.RoleStatus).Gap(k.Px(8)).Padding(k.Px(6), k.Px(16)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).
		Background(t.Attention.Alpha(0.1)).Children(func() {
		if status.Reconnecting {
			k.Spinner(c, 14, t.AttentionForeground)
		}
		k.Text(c, text, 12, 16).TextColor(t.AttentionForeground)
	})
}
