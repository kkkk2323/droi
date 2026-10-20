// Package sessionview draws one Session as Droi's window does: its
// transcript with tool calls, Markdown, pictures and subagent cards, the
// Prompts the Daemon asks (permission requests and AskUser questions), and
// a plain-text composer. It reaches the Daemon through the Go SDK's
// Controller and the rest of its app through an Env, so another app draws
// it too.
package sessionview

import (
	"context"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/subagents"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// LoadedMessageLimit is how many of a Session's latest messages a load
// brings, as the web Client's LOADED_MESSAGE_LIMIT.
const LoadedMessageLimit = 400

// olderPage is how many messages one daemon.get_session_messages brings.
const olderPage = 100

// ColumnWidth is the reading column's max-w-3xl, which the transcript,
// the Prompts and the composer share.
const ColumnWidth = 768

// imageIdleFrames is how many builds of its view a picture stays decoded
// without being drawn.
const imageIdleFrames = 600

// Env is what a View needs from its app.
type Env struct {
	// Controller is the Daemon's.
	Controller *controller.Controller
	// Ctx ends the calls the View makes; context.Background when nil.
	Ctx context.Context
	// Kit is what to draw with, read as the View is built: it changes with
	// the theme, the font, the text size and the zoom.
	Kit func() *kit.Kit
	// Update runs fn on the UI thread and draws a frame; it is called from
	// any goroutine.
	Update func(fn func())
	// Now is the clock, for the turns' times; time.Now when nil.
	Now func() time.Time
	// Revisions counts the Store's changes to each Session, so that the
	// transcript is built again only when its own Session changed.
	Revisions *Revisions

	// OpenSession opens a subagent's Session from its card; nil leaves the
	// card without its Open button.
	OpenSession func(id string)
	// Quote takes a reply the transcript's menu quotes; nil quotes it in the
	// plain composer.
	Quote func(text string)
	// ScrollShortcut is how the shortcut that scrolls to the latest reads,
	// such as "⌘J", for the button's tooltip; "" tells none.
	ScrollShortcut string
	// Overlay draws over the transcript's list once it is built, in a box
	// the list fills; width is the list's.
	Overlay func(c *ui.Context, s *session.Session, width float32)
	// Local tells whether the Daemon runs on this computer, where a
	// background command's output file and process can be looked at; nil
	// when it never does.
	Local func() bool
}

// Revisions counts each Session's changes in the Store, for its View to
// build its transcript again.
type Revisions struct {
	m sync.Map // Session id → *atomic.Int64
}

func (r *Revisions) of(id string) *atomic.Int64 {
	n, _ := r.m.LoadOrStore(id, new(atomic.Int64))
	return n.(*atomic.Int64)
}

// Bump counts a change to Session id. Any goroutine.
func (r *Revisions) Bump(id string) { r.of(id).Add(1) }

// Load is the count of Session id's changes.
func (r *Revisions) Load(id string) int64 { return r.of(id).Load() }

// Watch counts every change the Store reports, then calls changed, until
// the returned function is called.
func (r *Revisions) Watch(store *session.Store, changed func()) (unsubscribe func()) {
	return store.Subscribe(func(e session.Event) {
		r.Bump(e.SessionID)
		if changed != nil {
			changed()
		}
	})
}

// View is the state of one Session's page, kept while its app shows it so
// that going back finds it where it was.
type View struct {
	env Env
	id  string

	list    ui.ListState
	entries []*transcript.Entry
	builder transcript.Builder
	built   int64 // the Session's revision entries were built at
	// frameRows are the list's rows of the frame being built.
	frameRows []transcriptRow

	// Guarded by mu: written by the goroutines loading the Session.
	mu           sync.Mutex
	loading      bool
	loadErr      string
	olderLoading bool
	olderErr     string

	// flags are the disclosures' open states, by the block or call they open.
	flags map[string]*bool
	// md are the transcript's Markdown options.
	md    Markdown
	frame int64
	// calls caches what a tool call's result reads as, decoded from its
	// JSON once: the transcript keeps a call's ToolCall while it stays the
	// same, and builds a new one when it changes.
	calls map[*transcript.ToolCall]*callView
	// pendingJump is a jump to a message whose page just loaded.
	pendingJump string
	// links and streamingID are the frame's, for the transcript's rows;
	// linksAt is what links were built from.
	links       *subagents.Links
	linksAt     linksKey
	streamingID string
	// progress is what each subagent's Session shows of its run, read
	// again when that Session changes.
	progress map[string]*subagentProgress
	// While a fold the user opened holds the transcript in place: whether
	// the list has left its end since, and the rows it had.
	leftEnd  bool
	heldRows int
	images   map[string]*ui.Bitmap
	imageAt  map[string]int64
	// zoom is the picture shown enlarged while zoomOpen.
	zoom      *ui.Bitmap
	zoomLabel string
	zoomOpen  bool

	jobs jobsState

	compacting bool
	// hasLoaded: the Session was loaded once. The Daemon cleans up an idle
	// one, and the Controller loads it again before the next call, so the
	// composer stays usable.
	hasLoaded bool
	// err is the composer's error line.
	err       string
	promptErr string
	asks      map[string]*askState
	specs     map[string]*specAnswer
	plain     plainComposer
}

// New makes the view of Session id; Open loads it.
func New(id string, env Env) *View {
	if env.Ctx == nil {
		env.Ctx = context.Background()
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Update == nil {
		env.Update = func(fn func()) { fn() }
	}
	v := &View{env: env, id: id, built: -1, flags: map[string]*bool{}, images: map[string]*ui.Bitmap{}, imageAt: map[string]int64{},
		calls: map[*transcript.ToolCall]*callView{}, progress: map[string]*subagentProgress{}}
	v.list.FollowEnd = true
	v.md.Load = v.loadImage
	v.md.Enlarge = func(bm *ui.Bitmap, label string) { v.zoom, v.zoomLabel, v.zoomOpen = bm, label, true }
	return v
}

// ID is the Session's id.
func (v *View) ID() string { return v.id }

func (v *View) kit() *kit.Kit { return v.env.Kit() }

func (v *View) look() (*kit.Kit, *theme.Tokens) {
	k := v.env.Kit()
	return k, k.T
}

func (v *View) redraw() { v.env.Update(func() {}) }

// Open loads the Session from the Daemon unless it is loaded or loading.
func (v *View) Open() {
	ctl := v.env.Controller
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
		_, err := ctl.LoadSession(v.env.Ctx, protocol.LoadSessionParams{SessionID: v.id, MessageLimit: &limit})
		v.mu.Lock()
		v.loading = false
		if err != nil {
			v.loadErr = err.Error()
		}
		v.mu.Unlock()
		v.redraw()
	}()
}

// loadOlder brings the page of messages before the first one shown.
func (v *View) loadOlder() {
	ctl := v.env.Controller
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
		defer v.redraw()
		cl, err := ctl.Client()
		var res *protocol.GetSessionMessagesResult
		if err == nil {
			limit := float64(olderPage)
			res, err = cl.GetSessionMessages(v.env.Ctx, protocol.GetSessionMessagesParams{SessionID: v.id, Cursor: cursor, Limit: &limit})
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

// Hold stops the transcript following its end, as the web Client's list
// follows only rows added: a fold opened near the end grows below the row
// clicked instead of pushing it up. Scrolling back to the end follows
// again (resume).
func (v *View) Hold() {
	v.list.FollowEnd = false
	v.leftEnd = false
	v.heldRows = len(v.entries)
}

func (v *View) hold() { v.Hold() }

func (v *View) holdOnToggle(trigger ui.Element) {
	if trigger.Changed() {
		v.hold()
	}
}

// resume follows the end again when the user scrolls back to it, or when
// rows are added while it shows, as the web Client follows new output.
func (v *View) resume() {
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

// ScrollToLatest scrolls the transcript to its end and follows it again.
func (v *View) ScrollToLatest() {
	v.list.FollowEnd = true
	v.list.ScrollToEnd()
}

// Flag is the open state kept under key, def until it is set: the
// transcript's folds keep theirs here, and so can the page's own.
func (v *View) Flag(key string, def bool) *bool {
	p := v.flags[key]
	if p == nil {
		p = new(bool)
		*p = def
		v.flags[key] = p
	}
	return p
}

func (v *View) flag(key string, def bool) *bool { return v.Flag(key, def) }

// sweep drops the decoded results and pictures no frame used lately.
func (v *View) sweep() {
	v.frame++
	if v.frame%120 != 0 {
		return
	}
	for call, cv := range v.calls {
		if v.frame-cv.usedAt > 60 {
			delete(v.calls, call)
		}
	}
	// A decoded picture is width × height × 4 bytes; one the list scrolled
	// away from long ago is decoded again if it comes back.
	for id, at := range v.imageAt {
		if v.frame-at > imageIdleFrames {
			delete(v.images, id)
			delete(v.imageAt, id)
		}
	}
}

// Busy: the view holds what would be lost with it, a load or a compaction
// under way, or the answers to a question still open.
func (v *View) Busy() bool {
	v.mu.Lock()
	loading := v.loading || v.olderLoading
	v.mu.Unlock()
	ctl := v.env.Controller
	asking := len(v.asks) > 0 && ctl != nil && len(ctl.PendingAskUsers(v.id)) > 0
	return loading || v.compacting || asking
}

// SetCompacting tells the view that its app compacts the Session, which the
// transcript's activity row and the composer then show as a running turn.
func (v *View) SetCompacting(on bool) { v.compacting = on }

// Compacting reports what SetCompacting set.
func (v *View) Compacting() bool { return v.compacting }

// Ready reports whether the Session was loaded once, which keeps the
// composer usable while the Controller loads it again.
func (v *View) Ready() bool { return v.hasLoaded }

// Built is the Session's revision the transcript was last built at.
func (v *View) Built() int64 { return v.built }

// Entries are the transcript's entries, as last built.
func (v *View) Entries() []*transcript.Entry { return v.entries }

// ReadingEntry is the entry of the first row in view, nil while the list
// shows its end or its lead.
func (v *View) ReadingEntry() *transcript.Entry {
	rows := v.frameRows
	if first, _ := v.list.Visible(); !v.list.AtEnd() && first >= 1 && first-1 < len(rows) {
		return rows[first-1].e
	}
	return nil
}

// ScrollToEntry scrolls the transcript to where entry e starts, among this
// frame's rows, and holds it there.
func (v *View) ScrollToEntry(e *transcript.Entry) {
	if row := rowOf(v.frameRows, e); row >= 0 {
		v.hold()
		v.list.ScrollTo(row, ui.Start)
	}
}

// JumpTo scrolls to the message with id at the transcript's next build,
// for a message whose page is loading.
func (v *View) JumpTo(id string) { v.pendingJump = id }

// Zoomed reports whether a picture is shown enlarged.
func (v *View) Zoomed() bool { return v.zoomOpen }

type callView struct {
	usedAt  int64
	result  *transcript.ToolResultView
	run     *transcript.ScriptRun
	summary *string
}

func (v *View) callView(call *transcript.ToolCall) *callView {
	cv := v.calls[call]
	if cv == nil {
		cv = &callView{}
		v.calls[call] = cv
	}
	cv.usedAt = v.frame
	return cv
}

func (v *View) result(call *transcript.ToolCall) transcript.ToolResultView {
	cv := v.callView(call)
	if cv.result == nil {
		r := transcript.ReadResult(call)
		cv.result = &r
	}
	return *cv.result
}

func (v *View) scriptRun(call *transcript.ToolCall) transcript.ScriptRun {
	cv := v.callView(call)
	if cv.run == nil {
		r := transcript.ReadScriptRun(call)
		cv.run = &r
	}
	return *cv.run
}

func (v *View) scriptSummary(call *transcript.ToolCall) string {
	cv := v.callView(call)
	if cv.summary == nil {
		s := transcript.ScriptSummary(call)
		cv.summary = &s
	}
	return *cv.summary
}

func (v *View) image(id string, img transcript.Image) *ui.Bitmap {
	v.imageAt[id] = v.frame
	return CachedBitmap(v.images, id, img)
}

// CachedBitmap decodes img once per id; a failed decode is cached as nil.
func CachedBitmap(cache map[string]*ui.Bitmap, id string, img transcript.Image) *ui.Bitmap {
	if b, ok := cache[id]; ok {
		return b
	}
	data, err := base64.StdEncoding.DecodeString(img.Data)
	var b *ui.Bitmap
	if err == nil {
		b, _ = ui.DecodeBitmap(data)
	}
	cache[id] = b
	return b
}

// refresh builds the transcript again when the Session changed, keeping the
// entries that did not change so that their rows keep their state.
func (v *View) refresh(s *session.Session) {
	rev := v.env.Revisions.Load(v.id)
	if rev == v.built || s == nil {
		return
	}
	v.built = rev
	next := v.builder.Build(s.DisplayMessages())
	v.entries, _ = transcript.ReuseUnchanged(v.entries, next)
}

// Frame starts a frame of the view: it drops what no frame used lately,
// follows the transcript's end again when it should, and builds the
// transcript again if the Session changed. It returns the Session, nil
// until the Store has it. Call it once a frame, before the view's parts.
func (v *View) Frame() *session.Session {
	v.sweep()
	v.resume()
	var s *session.Session
	if ctl := v.env.Controller; ctl != nil {
		s = ctl.Store().Session(v.id)
	}
	v.refresh(s)
	v.Jobs()
	if s != nil && s.LoadState() == session.Loaded {
		v.hasLoaded = true
	}
	return s
}

// Transcript is the room under a page's header: the transcript, a spinner
// while the Session loads, or why it did not load. It grows to fill its
// column. listed are the Sessions the app lists, for the subagents' cards;
// nil reads the subagents from the Store alone.
func (v *View) Transcript(c *ui.Context, s *session.Session, listed []sessions.Summary) {
	k, t := v.look()
	v.mu.Lock()
	loadErr, loading := v.loadErr, v.loading
	v.mu.Unlock()
	loaded := s != nil && s.LoadState() == session.Loaded
	ui.Box(c).Grow(1).MinHeight(0).Children(func() {
		switch {
		case loadErr != "":
			ui.Column(c).FillWidth().MaxWidth(k.Px(ColumnWidth)).AlignSelf(ui.Center).Padding(k.Px(24)).Children(func() {
				k.Text(c, loadErr, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
			})
		case !loaded && len(v.entries) == 0:
			if loading || s == nil || s.LoadState() != session.Loaded {
				ui.Row(c).Fill().Center().Gap(k.Px(8)).Children(func() {
					k.Spinner(c, 16, t.MutedForeground)
					k.Text(c, l10n.L("Loading session…"), 14, 20).TextColor(t.MutedForeground)
				})
			}
		default:
			v.transcriptView(c, s, listed)
		}
	})
}

// Zoom is the picture clicked in the transcript, enlarged over the window
// while it is open. Build it after the rest of the page.
func (v *View) Zoom(c *ui.Context) {
	if v.zoomOpen {
		v.zoomView(c)
	}
}

// Build is the whole view: the transcript over the plain composer, filling
// the room it is given, and the enlarged picture.
func (v *View) Build(c *ui.Context, listed []sessions.Summary) {
	s := v.Frame()
	ui.Column(c).Fill().Children(func() {
		v.Transcript(c, s, listed)
		v.Composer(c, s)
	})
	v.Zoom(c)
}
