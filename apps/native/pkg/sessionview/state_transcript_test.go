package sessionview

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// A view's entries are a window: Pi keeps the newest ones, appends what comes next and, once the window is too
// long, drops its oldest in one cut. The window draws more than that: what the window dropped and the pages
// loaded above it. These tests hold the State to Pi's ConversationTranscript rules — a window only adds to what
// is held; one that does not continue it replaces it — and the page above to conversation.entries by entry ID.

// windowOf is a view of conversation 7 whose window holds ids, the entry just above it being before (0 when the
// window starts the conversation). Odd ids are the user's, even ones the answers.
func windowOf(t *testing.T, before kernel.ID, ids ...kernel.ID) kernel.View {
	t.Helper()
	view := kernel.View{Conversation: kernel.Conversation{ID: 7}}
	if before > 0 {
		view.Before = json.RawMessage(fmt.Sprint(before))
	}
	for _, id := range ids {
		view.Entries = append(view.Entries, numbered(t, id))
	}
	return view
}

func numbered(t *testing.T, id kernel.ID) kernel.Entry {
	t.Helper()
	kind := kernel.EntryAssistant
	if id%2 == 1 {
		kind = kernel.EntryUser
	}
	return entryOf(t, id, kind, fmt.Sprintf("m%d", id), float64(id)*1000)
}

func windowPush(t *testing.T, before kernel.ID, ids ...kernel.ID) kernel.ViewNotification {
	t.Helper()
	view := windowOf(t, before, ids...)
	return kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: &view}
}

func span(from, to kernel.ID) []kernel.ID {
	var ids []kernel.ID
	for id := from; id <= to; id++ {
		ids = append(ids, id)
	}
	return ids
}

func idsOf(entries []kernel.Entry) []kernel.ID {
	ids := make([]kernel.ID, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

// descending is a conversation.entries page read backwards from the newest entry at or below maxEntryId, as the
// Kernel answers one: a cursor while entries remain below the page.
func descending(t *testing.T, p kernel.EntriesParams) kernel.EntriesResult {
	t.Helper()
	var q struct {
		Order      string    `json:"order"`
		MaxEntryID kernel.ID `json:"maxEntryId"`
	}
	if err := json.Unmarshal(p.Query, &q); err != nil || q.Order != "descending" || q.MaxEntryID < 1 {
		t.Errorf("conversation.entries was asked %s, want {order: descending, maxEntryId}", p.Query)
		return kernel.EntriesResult{}
	}
	var page kernel.EntriesResult
	id := q.MaxEntryID
	for ; id >= 1 && len(page.Items) < p.Limit; id-- {
		page.Items = append(page.Items, numbered(t, id))
	}
	if id >= 1 {
		page.Cursor = json.RawMessage(`{"opaque":true}`)
	}
	return page
}

// TestStateKeepsTheEntriesTheWindowDrops: when the window cuts its oldest entries, the transcript keeps them. A
// window that dropped rows the user was reading would move the page under them.
func TestStateKeepsTheEntriesTheWindowDrops(t *testing.T) {
	s := NewState(7)
	s.Apply(windowPush(t, 0, span(1, 4)...))
	if got := idsOf(s.Transcript().Entries); !slices.Equal(got, span(1, 4)) || s.HasOlder() {
		t.Fatalf("the first window holds %v (older: %v), want 1-4 and nothing above", got, s.HasOlder())
	}

	s.Apply(windowPush(t, 2, span(3, 6)...))
	if got := idsOf(s.Transcript().Entries); !slices.Equal(got, span(1, 6)) {
		t.Fatalf("after the window cut its front the transcript holds %v, want 1-6", got)
	}
	if got := idsOf(s.Entries()); !slices.Equal(got, span(3, 6)) {
		t.Errorf("Entries is %v, want the window itself, 3-6", got)
	}
	if s.HasOlder() || s.Transcript().Before != nil {
		t.Error("a transcript that holds the first entry says there is more above it")
	}

	s.Apply(windowPush(t, 2, span(3, 7)...))
	if got := idsOf(s.Transcript().Entries); !slices.Equal(got, span(1, 7)) {
		t.Fatalf("an append after the cut holds %v, want 1-7", got)
	}
}

// TestStateReplacesWhatItHoldsWithAWindowThatDoesNotContinueIt: a window whose before lies past the newest entry
// held left entries out between the two. They are not known, so the window replaces what is held and the page
// above it reaches them, rather than drawing the two parts as if they were adjacent.
func TestStateReplacesWhatItHoldsWithAWindowThatDoesNotContinueIt(t *testing.T) {
	s := NewState(7)
	s.Apply(windowPush(t, 0, span(1, 4)...))
	s.Apply(windowPush(t, 9, span(10, 12)...))
	if got := idsOf(s.Transcript().Entries); !slices.Equal(got, span(10, 12)) {
		t.Fatalf("the transcript holds %v, want only the window 10-12", got)
	}
	if !s.HasOlder() || s.Low() != 9 {
		t.Errorf("older: %v, low %d; want the page above to start at 9", s.HasOlder(), s.Low())
	}
	if string(s.Transcript().Before) != "9" {
		t.Errorf("the transcript's before is %s, want 9", s.Transcript().Before)
	}
}

// TestStatePrependsTheOlderPages: a page is read newest first and drawn oldest first above what is held; the
// next page starts below its oldest entry, and one without a cursor reaches the conversation's start. A window
// that continues the transcript afterwards keeps the pages.
func TestStatePrependsTheOlderPages(t *testing.T) {
	s := NewState(7)
	s.Apply(windowPush(t, 50, span(51, 53)...))
	rev := s.Revision()

	page := kernel.EntriesResult{Items: []kernel.Entry{numbered(t, 50), numbered(t, 49), numbered(t, 48)}, Cursor: json.RawMessage(`{"x":1}`)}
	if !s.PrependOlder(50, page) {
		t.Fatal("the page above the window was not taken")
	}
	if got := idsOf(s.Transcript().Entries); !slices.Equal(got, span(48, 53)) {
		t.Fatalf("the transcript holds %v, want 48-53", got)
	}
	if s.Low() != 47 || s.Revision() == rev {
		t.Errorf("low %d at revision %d (was %d); want 47 and a new revision", s.Low(), s.Revision(), rev)
	}

	last := kernel.EntriesResult{Items: []kernel.Entry{numbered(t, 47), numbered(t, 46)}}
	if !s.PrependOlder(47, last) {
		t.Fatal("the last page was not taken")
	}
	if s.HasOlder() {
		t.Error("a page without a cursor did not reach the conversation's start")
	}
	if s.PrependOlder(47, last) {
		t.Error("the same page was taken twice")
	}

	s.Apply(windowPush(t, 50, span(51, 54)...))
	if got := idsOf(s.Transcript().Entries); !slices.Equal(got, span(46, 54)) {
		t.Fatalf("a push after the pages holds %v, want 46-54", got)
	}
}

// TestStateDropsAPageAskedBeforeTheWindowMoved: a page asked for the transcript the state held is not above the
// one it holds after a window replaced it, and would join two parts with a hole between them.
func TestStateDropsAPageAskedBeforeTheWindowMoved(t *testing.T) {
	s := NewState(7)
	s.Apply(windowPush(t, 50, span(51, 53)...))
	s.Apply(windowPush(t, 90, span(91, 93)...))
	page := kernel.EntriesResult{Items: []kernel.Entry{numbered(t, 50), numbered(t, 49)}, Cursor: json.RawMessage(`{}`)}
	if s.PrependOlder(50, page) {
		t.Error("a page asked before the window moved was taken")
	}
	if got := idsOf(s.Transcript().Entries); !slices.Equal(got, span(91, 93)) {
		t.Fatalf("the transcript holds %v, want 91-93", got)
	}
}

// TestStoreLoadsEarlierByEntryID: the page above is conversation.entries backwards from the entry above what is
// held — the view's before is an entry ID, not a cursor — and each page lands above the last one until the
// conversation's start.
func TestStoreLoadsEarlierByEntryID(t *testing.T) {
	f := &fakeKernel{views: map[kernel.ID]kernel.View{7: windowOf(t, 50, span(51, 53)...)}}
	f.entries = func(p kernel.EntriesParams) (kernel.EntriesResult, error) { return descending(t, p), nil }
	st := NewStore(3)
	ctx := context.Background()
	if err := st.Open(ctx, f, 7, kernelWindow); err != nil {
		t.Fatal(err)
	}
	notified := 0
	stop := st.Subscribe(func(kernel.ID) { notified++ })
	defer stop()

	if err := st.LoadEarlier(ctx, f, 7, 20); err != nil {
		t.Fatal(err)
	}
	p := f.entriesParams[0]
	if p.ID != 7 || p.Limit != 20 || len(p.Cursor) != 0 || string(p.Query) != `{"order":"descending","maxEntryId":50}` {
		t.Errorf("conversation.entries was asked %+v (query %s), want id 7, limit 20, maxEntryId 50, no cursor", p, p.Query)
	}
	if got := idsOf(st.State(7).Transcript().Entries); !slices.Equal(got, span(31, 53)) {
		t.Fatalf("after one page the transcript holds %v, want 31-53", got)
	}
	if notified != 1 {
		t.Errorf("the store told its watchers %d times, want once", notified)
	}

	for range 5 {
		if err := st.LoadEarlier(ctx, f, 7, 20); err != nil {
			t.Fatal(err)
		}
	}
	if got := idsOf(st.State(7).Transcript().Entries); !slices.Equal(got, span(1, 53)) || st.State(7).HasOlder() {
		t.Fatalf("after every page the transcript holds %v (older: %v), want 1-53 and nothing above", got, st.State(7).HasOlder())
	}
	if len(f.entriesParams) != 3 {
		t.Errorf("conversation.entries was asked %d times, want 3: a transcript at its start asks nothing", len(f.entriesParams))
	}
}

// TestStoreDoesNotLetALateUnwatchEndANewerOpen: opening a conversation evicts the one used least recently, and
// its unwatch is a round trip outside the lock. Opening the evicted one again while that unwatch is in flight
// must not reach Kernel first: Kernel would subscribe, then take the unwatch, and the page would stop moving.
func TestStoreDoesNotLetALateUnwatchEndANewerOpen(t *testing.T) {
	f := &fakeKernel{views: map[kernel.ID]kernel.View{1: windowOf(t, 0, 1), 2: windowOf(t, 0, 1)}}
	st := NewStore(1)
	ctx := context.Background()
	if err := st.Open(ctx, f, 1, kernelWindow); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan kernel.ID, 4), make(chan struct{})
	f.mu.Lock()
	f.unwatchEntered, f.unwatchRelease = entered, release
	f.mu.Unlock()

	done := make(chan error, 2)
	go func() { done <- st.Open(ctx, f, 2, kernelWindow) }()
	if id := <-entered; id != 1 {
		t.Fatalf("opening 2 unwatched %d, want 1", id)
	}
	go func() { done <- st.Open(ctx, f, 1, kernelWindow) }()

	time.Sleep(100 * time.Millisecond)
	if log := f.callLog(); slices.Index(log, "open 1") != lastIndex(log, "open 1") {
		t.Fatalf("1 was opened again while its unwatch was in flight: %v", log)
	}

	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	log := f.callLog()
	if slices.Index(log, "unwatch 1") > lastIndex(log, "open 1") {
		t.Fatalf("the unwatch of 1 reached Kernel after it was opened again: %v", log)
	}
	if s := st.State(1); s == nil || !s.Opened() {
		t.Error("the conversation opened again has no view")
	}
}

// TestAViewDrawsAKernelConversation: a View whose conversation Kernel drives opens it in the store, draws its
// rows, draws them again when a push lands, and brings the page above when asked — all without a Daemon.
func TestAViewDrawsAKernelConversation(t *testing.T) {
	f := &fakeKernel{views: map[kernel.ID]kernel.View{7: windowOf(t, 4, span(5, 8)...)}}
	f.entries = func(p kernel.EntriesParams) (kernel.EntriesResult, error) { return descending(t, p), nil }
	st := NewStore(3)
	v := New("7", Env{Kernel: f, Sessions: st, Revisions: &Revisions{}})

	if v.Frame() != nil {
		t.Fatal("a conversation nobody opened has a snapshot")
	}
	v.Open()
	var snap *Snapshot
	await(t, "the first view", func() bool {
		snap = v.Frame()
		return snap != nil && snap.Loaded
	})
	if !snap.HasOlder || snap.Session != nil || snap.State != st.State(7) {
		t.Errorf("the snapshot is %+v, want a Kernel one with entries above it", snap)
	}
	if got := texts(v); !slices.Equal(got, []string{"m5", "m6", "m7", "m8"}) {
		t.Fatalf("the rows read %v, want m5-m8", got)
	}

	st.ApplyView(0, kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: ptr(windowOf(t, 4, span(5, 9)...))})
	v.Frame()
	if got := texts(v); !slices.Equal(got, []string{"m5", "m6", "m7", "m8", "m9"}) {
		t.Fatalf("after a push the rows read %v, want m5-m9", got)
	}

	v.loadOlder()
	await(t, "the page above", func() bool {
		v.Frame()
		return len(texts(v)) == 9
	})
	if got := texts(v); got[0] != "m1" {
		t.Errorf("the rows read %v, want the conversation from m1", got)
	}
	if v.Frame().HasOlder {
		t.Error("a transcript at its start still offers the page above")
	}
}

func ptr[T any](v T) *T { return &v }

func lastIndex(s []string, v string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == v {
			return i
		}
	}
	return -1
}

// texts are the first block's text of each row the View built.
func texts(v *View) []string {
	var out []string
	for _, e := range v.Entries() {
		if len(e.Blocks) > 0 {
			out = append(out, e.Blocks[0].Text)
		}
	}
	return out
}

// TestARealKernelPagesTheTranscriptAboveTheWindow: what the window means, and how the page above is asked, are the
// Kernel's and Pi's, so they are checked against a Kernel that produced them. A window of two to four entries over
// four turns cuts its front more than once, and the Kernel coalesces pushes, so one push can pass over a whole cut:
// the transcript then starts again at that window. Either way it is never missing an entry between its first and
// its last, and paging up reaches every one. A second connection that opens the same conversation sees only the
// window and pages up the same way.
func TestARealKernelPagesTheTranscriptAboveTheWindow(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "one", Text: "answer one"}, {Match: "two", Text: "answer two"},
		{Match: "three", Text: "answer three"}, {Match: "four", Text: "answer four"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	small := kernel.Window{Min: 2, Max: 4}
	st := NewStore(3)
	if err := st.Open(ctx, c, id, small); err != nil {
		t.Fatalf("conversation.open: %v\n%s", err, k.Logs())
	}
	pumpNotifications(st, c)
	for _, word := range []string{"one", "two", "three", "four"} {
		sub := k.Submit(t, c, id, word)
		k.WaitForReply(t, c, sub, "answer "+word, 60*time.Second)
	}

	all, err := c.Entries(ctx, kernel.EntriesParams{ID: id, Query: json.RawMessage(`{"order":"ascending"}`), Limit: 500})
	if err != nil {
		t.Fatalf("conversation.entries: %v", err)
	}
	want := idsOf(all.Items)
	if len(want) < 8 {
		t.Fatalf("four turns wrote %d entries, want at least 8: %v", len(want), want)
	}
	await(t, "the window to hold the last entry", func() bool {
		held := st.State(id).Transcript().Entries
		return len(held) > 0 && held[len(held)-1].ID == want[len(want)-1]
	})
	got := idsOf(st.State(id).Transcript().Entries)
	if !slices.Equal(got, want[len(want)-len(got):]) {
		t.Fatalf("the transcript holds %v, which is not an unbroken end of %v", got, want)
	}
	if !st.State(id).HasOlder() && !slices.Equal(got, want) {
		t.Fatalf("the transcript holds %v and says nothing is above it, want %v", got, want)
	}
	if window := st.State(id).Entries(); len(window) > small.Max {
		t.Errorf("the window holds %d entries, more than its max %d", len(window), small.Max)
	}
	pageUp(t, st, c, id, len(want), k)
	if got := idsOf(st.State(id).Transcript().Entries); !slices.Equal(got, want) {
		t.Fatalf("paged up, the transcript holds %v, want %v", got, want)
	}

	c2 := k.Dial(t)
	fresh := NewStore(3)
	if err := fresh.Open(ctx, c2, id, small); err != nil {
		t.Fatalf("conversation.open on a second connection: %v", err)
	}
	if !fresh.State(id).HasOlder() {
		t.Fatalf("a window of %v over %d entries says nothing is above it", small, len(want))
	}
	pageUp(t, fresh, c2, id, len(want), k)
	if got := idsOf(fresh.State(id).Transcript().Entries); !slices.Equal(got, want) {
		t.Fatalf("paged up on a second connection, the transcript holds %v, want %v", got, want)
	}
}

// pageUp loads pages of three until the transcript reaches the conversation's start.
func pageUp(t *testing.T, st *Store, c *kernel.Client, id kernel.ID, entries int, k *fakekernel.Kernel) {
	t.Helper()
	for i := 0; st.State(id).HasOlder(); i++ {
		if i > entries {
			t.Fatal("the pages never reached the conversation's start")
		}
		if err := st.LoadEarlier(context.Background(), c, id, 3); err != nil {
			t.Fatalf("the page above: %v\n%s", err, k.Logs())
		}
	}
}

// TestAKernelConversationThatCannotBeReadKeepsItsRows: Kernel says when it could not read a conversation again, and
// the state keeps the entries it had. The page must keep drawing them — a passing read failure that blanked the
// transcript would hide everything the user was reading — and stands the sentence in only when there is nothing
// else to draw.
func TestAKernelConversationThatCannotBeReadKeepsItsRows(t *testing.T) {
	theme.RegisterFonts()
	f := &fakeKernel{views: map[kernel.ID]kernel.View{7: windowOf(t, 0, span(1, 2)...), 8: {Conversation: kernel.Conversation{ID: 8}}}}
	st := NewStore(3)
	k := kit.New(theme.Parse("light"), theme.FontChoice(""), theme.TextSize(""), 1)
	var mu sync.Mutex
	var queue []func()
	update := func(fn func()) { mu.Lock(); queue = append(queue, fn); mu.Unlock() }
	draw := func(id string) (*View, *ui.Tester, func(string, func() bool)) {
		v := New(id, Env{Kernel: f, Sessions: st, Kit: func() *kit.Kit { return k }, Update: update, Revisions: &Revisions{}})
		v.Open()
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(k.UITheme(c.Theme()))
			v.Build(c, nil)
		}, 900, 640)
		until := func(what string, ok func() bool) {
			t.Helper()
			deadline := time.Now().Add(8 * time.Second)
			for {
				mu.Lock()
				q := queue
				queue = nil
				mu.Unlock()
				for _, fn := range q {
					fn()
				}
				keepTime(tt)
				tt.Frame()
				if ok() {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("never: %s; texts %q", what, tt.Texts())
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		return v, tt, until
	}
	shows := func(tt *ui.Tester, sub string) bool {
		for _, s := range tt.Texts() {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	failed := func(id kernel.ID) kernel.ViewNotification {
		return kernel.ViewNotification{ID: id, Kind: kernel.ViewKindFailed, Error: json.RawMessage(`{"message":"the database is locked"}`)}
	}

	_, tt, until := draw("7")
	until("the rows", func() bool { return shows(tt, "m1") && shows(tt, "m2") })
	st.ApplyView(0, failed(7))
	until("a frame after the failure", func() bool { return st.State(7).LoadErr() != "" })
	until("the rows still drawn", func() bool { return shows(tt, "m1") && shows(tt, "m2") })

	_, empty, untilEmpty := draw("8")
	untilEmpty("the empty conversation opened", func() bool { return st.State(8) != nil && st.State(8).Opened() })
	st.ApplyView(0, failed(8))
	untilEmpty("the sentence", func() bool { return shows(empty, "the database is locked") })
}
