package app

import (
	"encoding/base64"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/md"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/subagents"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

// olderPage is how many messages one daemon.get_session_messages brings.
const olderPage = 100

// columnWidth is the reading column's max-w-3xl, which the transcript,
// the Prompts and the composer share.
const columnWidth = 768

// sessionView is the state of one Session's page, kept while the window
// lives so that going back finds it where it was.
type sessionView struct {
	a  *App
	id string

	list    ui.ListState
	entries []*transcript.Entry
	built   int64 // the Store revision entries were built at

	// Guarded by mu: written by the goroutines loading the Session.
	mu           sync.Mutex
	loading      bool
	loadErr      string
	olderLoading bool
	olderErr     string

	// flags are the disclosures' open states, by the block or call they open.
	flags map[string]*bool
	// docs caches parsed Markdown by its text.
	docs   map[string][]*md.Node
	usedAt map[string]int64
	frame  int64
	// calls caches what a tool call's result reads as, decoded from its
	// JSON once: the transcript keeps a call's ToolCall while it stays the
	// same, and builds a new one when it changes.
	calls map[*transcript.ToolCall]*callView
	rail  railState
	// pendingJump is a rail jump to a message whose page just loaded.
	pendingJump string
	// links and streamingID are the frame's, for the transcript's rows.
	links       *subagents.Links
	streamingID string
	// While a fold the user opened holds the transcript in place: whether
	// the list has left its end since, and the rows it had.
	leftEnd  bool
	heldRows int
	images   map[string]*ui.Bitmap

	renaming bool
	rename   string
	copyOpen bool
	copied   time.Time
	openIn   bool

	compacting bool
	composer   composerState
	tools      toolsState
	git        gitState
	// subOpen and trailOpen: the header's subagent menus.
	subOpen, trailOpen bool

	// focused is whether the composer took the focus once already.
	focused   bool
	promptErr string
	asks      map[string]*askState
}

func (a *App) view(id string) *sessionView {
	v := a.views[id]
	if v == nil {
		v = &sessionView{a: a, id: id, built: -1, flags: map[string]*bool{}, docs: map[string][]*md.Node{}, usedAt: map[string]int64{}, images: map[string]*ui.Bitmap{},
			calls: map[*transcript.ToolCall]*callView{}, rail: railState{preview: -1}}
		v.list.FollowEnd = true
		d := a.drafts.Load(id)
		v.composer.text, v.composer.images = d.Text, d.Images
		a.views[id] = v
	}
	return v
}

// open loads the Session from the Daemon unless it is loaded or loading.
func (v *sessionView) open() {
	ctl := v.a.ctl
	if ctl == nil {
		return
	}
	if s := ctl.Store().Session(v.id); s != nil && s.LoadState() == session.Loaded {
		return
	}
	v.mu.Lock()
	if v.loading {
		v.mu.Unlock()
		return
	}
	v.loading, v.loadErr = true, ""
	v.mu.Unlock()
	go func() {
		limit := int64(LoadedMessageLimit)
		_, err := ctl.LoadSession(v.a.ctx, protocol.LoadSessionParams{SessionID: v.id, MessageLimit: &limit})
		v.mu.Lock()
		v.loading = false
		if err != nil {
			v.loadErr = err.Error()
		}
		v.mu.Unlock()
		v.a.redraw()
	}()
}

// loadOlder brings the page of messages before the first one shown.
func (v *sessionView) loadOlder() {
	ctl := v.a.ctl
	s := ctl.Store().Session(v.id)
	if s == nil || !s.HasOlderMessages() {
		return
	}
	cursor, ok := s.OlderMessagesCursor()
	v.mu.Lock()
	if !ok || v.olderLoading {
		v.mu.Unlock()
		return
	}
	v.olderLoading, v.olderErr = true, ""
	v.mu.Unlock()
	go func() {
		defer v.a.redraw()
		cl, err := ctl.Client()
		var res *protocol.GetSessionMessagesResult
		if err == nil {
			limit := float64(olderPage)
			res, err = cl.GetSessionMessages(v.a.ctx, protocol.GetSessionMessagesParams{SessionID: v.id, Cursor: cursor, Limit: &limit})
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		v.olderLoading = false
		if err != nil {
			v.olderErr = err.Error()
			return
		}
		page := make([]protocol.FactoryDroidMessage, len(res.Messages))
		for i, m := range res.Messages {
			page[len(page)-1-i] = m
		}
		ctl.Store().PrependOlderMessages(v.id, page, res.HasMore)
	}()
}

// hold stops the transcript following its end, as the web Client's list
// follows only rows added: a fold opened near the end grows below the row
// clicked instead of pushing it up. Scrolling back to the end follows
// again (resume).
func (v *sessionView) hold() {
	v.list.FollowEnd = false
	v.leftEnd = false
	v.heldRows = len(v.entries)
}

func (v *sessionView) holdOnToggle(trigger *ui.Element) {
	if trigger.Changed() {
		v.hold()
	}
}

// resume follows the end again when the user scrolls back to it, or when
// rows are added while it shows, as the web Client follows new output.
func (v *sessionView) resume() {
	if v.list.FollowEnd {
		return
	}
	atEnd := v.list.AtEnd()
	if !atEnd {
		v.leftEnd = true
		return
	}
	if v.leftEnd || len(v.entries) != v.heldRows {
		v.list.FollowEnd = true
	}
}

func (v *sessionView) flag(key string, def bool) *bool {
	p := v.flags[key]
	if p == nil {
		p = new(bool)
		*p = def
		v.flags[key] = p
	}
	return p
}

// doc is the parsed Markdown of text, parsed once while it stays the same.
func (v *sessionView) doc(text string) []*md.Node {
	v.usedAt[text] = v.frame
	if d, ok := v.docs[text]; ok {
		return d
	}
	d := md.ParseTree(text)
	v.docs[text] = d
	return d
}

// sweep drops the Markdown no frame used lately, as a reply streams in
// and each of its lengths is parsed once.
func (v *sessionView) sweep() {
	v.frame++
	if v.frame%120 != 0 {
		return
	}
	for text, at := range v.usedAt {
		if v.frame-at > 60 {
			delete(v.docs, text)
			delete(v.usedAt, text)
		}
	}
	for call, cv := range v.calls {
		if v.frame-cv.usedAt > 60 {
			delete(v.calls, call)
		}
	}
}

type callView struct {
	usedAt  int64
	result  *transcript.ToolResultView
	run     *transcript.ScriptRun
	summary *string
}

func (v *sessionView) callView(call *transcript.ToolCall) *callView {
	cv := v.calls[call]
	if cv == nil {
		cv = &callView{}
		v.calls[call] = cv
	}
	cv.usedAt = v.frame
	return cv
}

func (v *sessionView) result(call *transcript.ToolCall) transcript.ToolResultView {
	cv := v.callView(call)
	if cv.result == nil {
		r := transcript.ReadResult(call)
		cv.result = &r
	}
	return *cv.result
}

func (v *sessionView) scriptRun(call *transcript.ToolCall) transcript.ScriptRun {
	cv := v.callView(call)
	if cv.run == nil {
		r := transcript.ReadScriptRun(call)
		cv.run = &r
	}
	return *cv.run
}

func (v *sessionView) scriptSummary(call *transcript.ToolCall) string {
	cv := v.callView(call)
	if cv.summary == nil {
		s := transcript.ScriptSummary(call)
		cv.summary = &s
	}
	return *cv.summary
}

func (v *sessionView) image(id string, img transcript.Image) *ui.Bitmap {
	if b, ok := v.images[id]; ok {
		return b
	}
	data, err := base64.StdEncoding.DecodeString(img.Data)
	var b *ui.Bitmap
	if err == nil {
		b, _ = ui.DecodeBitmap(data)
	}
	v.images[id] = b
	return b
}

// refresh builds the transcript again when the Store changed, keeping the
// entries that did not change so that their rows keep their state.
func (v *sessionView) refresh(s *session.Session) {
	rev := v.a.storeRev.Load()
	if rev == v.built || s == nil {
		return
	}
	v.built = rev
	next := transcript.Build(s.DisplayMessages())
	v.entries, _ = transcript.ReuseUnchanged(v.entries, next)
}

// build is the web Client's SessionView: the header, the transcript and
// the composer.
func (v *sessionView) build(c *ui.Context, a *App, sel *sessions.Summary, listed []sessions.Summary) {
	k, t := a.kit, a.kit.T
	v.sweep()
	v.resume()
	s := a.ctl.Store().Session(v.id)
	v.refresh(s)
	title := "Untitled session"
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
	v.mu.Lock()
	loadErr, loading := v.loadErr, v.loading
	v.mu.Unlock()
	loaded := s != nil && s.LoadState() == session.Loaded

	ui.Column(c).Role(ui.RoleGroup).Label(title).Fill().Children(func() {
		v.header(c, s, loaded, title, workspace, a.subagentNavOf(listed, sel, v.id))
		ui.Box(c).Grow(1).MinHeight(0).Children(func() {
			switch {
			case loadErr != "":
				ui.Column(c).FillWidth().MaxWidth(k.Px(columnWidth)).AlignSelf(ui.Center).Padding(k.Px(24)).Children(func() {
					k.Text(c, loadErr, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				})
			case !loaded && len(v.entries) == 0:
				if loading || s == nil || s.LoadState() != session.Loaded {
					ui.Row(c).Fill().Center().Gap(k.Px(8)).Children(func() {
						k.Spinner(c, 16, t.MutedForeground)
						k.Text(c, "Loading session…", 14, 20).TextColor(t.MutedForeground)
					})
				}
			default:
				v.transcriptView(c, s, listed)
			}
		})
		v.composerArea(c, s, workspace, loaded)
	})
}

// header is the PageHeader: the title, which a pencil renames, and the
// copy and open-in controls. It drags the window.
func (v *sessionView) header(c *ui.Context, s *session.Session, loaded bool, title, workspace string, nav subagentNav) {
	a := v.a
	k, t := a.kit, a.kit.T
	// The web Client's leading spacer, always there, so the header's gap
	// stands before the title too: room for the traffic lights and the
	// sidebar toggle while the sidebar is hidden.
	leading := float32(0)
	if !prefs.SidebarVisible.Get(a.prefs) {
		leading = 100
		if !a.cfg.InsetTop {
			leading = 32
		}
	}
	ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(12)).Gap(k.Px(4)).DragWindow().Children(func() {
		ui.Box(c).Width(leading).Shrink(0)
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
				k.Text(c, title, 14, 20).Role(ui.RoleHeading).FontWeight(500).TextColor(t.Foreground).SingleLine().Shrink(1)
				b := ui.ButtonBase(c).Label("Rename session").Size(k.Px(24), k.Px(24)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
				if !hover && !b.Focused() {
					b.Opacity(0)
				} else if b.Hovered() {
					b.Background(t.Muted)
				}
				b.Children(func() { k.Icon(c, "pencil", 12, t.MutedForeground) })
				if b.Clicked() {
					v.renaming, v.rename = true, title
				}
			})
		})
		ui.Row(c).Gap(k.Px(2)).Shrink(0).Children(func() {
			v.subagentMenu(c, nav)
			v.gitButton(c, s, loaded)
			v.copyMenu(c, title, workspace)
			v.openInButton(c, workspace)
		})
	})
}

func (v *sessionView) renameField(c *ui.Context, title string) {
	a := v.a
	k, t := a.kit, a.kit.T
	in := ui.TextInputBase(c, &v.rename).Label("Session title").AutoFocus().Grow(1).MaxWidth(k.Px(448)).Height(k.Px(28)).
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
	if k.IconButton(c, "check", "Save title", 24).Clicked() {
		save()
	}
	if k.IconButton(c, "x", "Cancel rename", 24).Clicked() {
		v.renaming = false
	}
}

// copyMenu is CopySessionMenu: the Session's id, or its details.
func (v *sessionView) copyMenu(c *ui.Context, title, workspace string) {
	a := v.a
	k, t := a.kit, a.kit.T
	copied := time.Since(v.copied) < 1500*time.Millisecond
	label, icon := "Copy session info", "copy"
	if copied {
		label, icon = "Copied", "check"
		c.After(1500*time.Millisecond - time.Since(v.copied))
	}
	b := ui.ButtonBase(c).Label(label).Tooltip("Copy session info").Size(k.Px(28), k.Px(28)).Radius(k.Px(8)).Cursor(ui.CursorPointer).Expanded(v.copyOpen)
	color := t.MutedForeground
	if b.Hovered() || v.copyOpen {
		b.Background(t.Accent)
		color = t.Foreground
	}
	b.Children(func() { k.Icon(c, icon, 14, color) })
	if b.Clicked() {
		v.copyOpen = !v.copyOpen
	}
	k.MenuPopup(c, b, &v.copyOpen, true, "Copy session info", func() {
		if k.MenuItem(c, &v.copyOpen, "fingerprint-pattern", "Copy session ID").Clicked() {
			c.WriteClipboard(v.id)
			v.copied = time.Now()
		}
		if k.MenuItem(c, &v.copyOpen, "copy", "Copy session details").Clicked() {
			c.WriteClipboard(a.sessionDetails(sessions.Summary{SessionID: v.id, Title: title, Cwd: workspace}))
			v.copied = time.Now()
		}
	})
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
	go func() {
		if png := host.AppIconPNG(app.AppPath, 64); png != nil {
			if bm, err := ui.DecodeBitmap(png); err == nil {
				appIcons.Store(app.ID, bm)
				a.redraw()
			}
		}
	}()
	return nil
}

// preferredApp is the remembered app while it is installed, else Finder,
// else the first.
func preferredApp(apps []host.OpenInApp, remembered string) (host.OpenInApp, bool) {
	for _, id := range []string{remembered, "finder"} {
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
	if workspace == "" || !ok {
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
		main := ui.ButtonBase(c).Label("Open in "+preferred.Label).Tooltip("Open in "+preferred.Label).FillHeight().PaddingX(k.Px(6)).
			Radius(k.Px(8), 0, 0, k.Px(8)).Cursor(ui.CursorPointer)
		if main.Hovered() {
			main.Background(t.Accent)
		}
		main.Children(func() { appIcon(preferred) })
		if main.Clicked() {
			open(preferred)
		}
		ui.Box(c).Width(1).FillHeight().Background(t.Border)
		more := ui.ButtonBase(c).Label("Open in another app").Tooltip("Open in another app").FillHeight().Width(k.Px(18)).
			Radius(0, k.Px(8), k.Px(8), 0).Cursor(ui.CursorPointer).Expanded(v.openIn)
		if more.Hovered() || v.openIn {
			more.Background(t.Accent)
		}
		more.Children(func() { k.Icon(c, "chevron-down", 12, t.MutedForeground) })
		if more.Clicked() {
			v.openIn = !v.openIn
		}
		k.MenuPopup(c, more, &v.openIn, true, "Open in", func() {
			for _, app := range list {
				item := ui.ButtonBase(c).Key(app.ID).Role(ui.RoleMenuItemRadio).Checked(app.ID == preferred.ID).Label(app.Label).
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
