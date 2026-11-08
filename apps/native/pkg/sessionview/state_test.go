package sessionview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// The window stops keeping a session's state: Kernel pushes a view, and this file's two types hold it.
// These tests take the pushes the contract defines and check what a View would draw from them. No Kernel
// runs, and none is needed: a push is JSON, and the whole job here is what that JSON means.

// viewPush is a whole view of one conversation, as conversation.view sends it to a "view" subscriber.
func viewPush(t *testing.T, id kernel.ID, entries ...kernel.Entry) kernel.ViewNotification {
	t.Helper()
	view := kernel.View{Conversation: kernel.Conversation{ID: id}, Entries: entries}
	return kernel.ViewNotification{ID: id, Kind: kernel.ViewKindValue, Value: &view}
}

func entryOf(t *testing.T, id kernel.ID, kind, text string, at float64) kernel.Entry {
	t.Helper()
	model, err := json.Marshal([]map[string]any{{
		"role": roleOf(kind), "content": []map[string]any{{"type": "text", "text": text}}, "timestamp": at,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return kernel.Entry{ID: id, ConversationID: 1, Kind: kind, Model: model}
}

func roleOf(kind string) string {
	if kind == kernel.EntryUser {
		return "user"
	}
	return "assistant"
}

// TestStateTakesAWholeView: the first push a subscriber gets is the state it draws. It replaces whatever the
// state held, and the revision moves so the transcript is built again.
func TestStateTakesAWholeView(t *testing.T) {
	s := NewState(7)
	if s.Revision() != 0 || s.Opened() {
		t.Fatalf("a new state is %+v, want an empty unopened one", s)
	}

	first := viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "hello", 1000))
	if !s.Apply(first) {
		t.Fatal("the first whole view changed nothing")
	}
	if !s.Opened() || len(s.Entries()) != 1 || s.Entries()[0].ID != 1 {
		t.Fatalf("the state holds %+v", s.Entries())
	}
	if s.Revision() != 1 {
		t.Errorf("revision %d, want 1", s.Revision())
	}

	second := viewPush(t, 7,
		entryOf(t, 1, kernel.EntryUser, "hello", 1000),
		entryOf(t, 2, kernel.EntryAssistant, "hi", 2000))
	if !s.Apply(second) {
		t.Fatal("the second whole view changed nothing")
	}
	if len(s.Entries()) != 2 || s.Revision() != 2 {
		t.Fatalf("the state holds %d entries at revision %d", len(s.Entries()), s.Revision())
	}

	// A push is a snapshot, not a delta: the entries it does not carry are gone. Kernel drops the window's front
	// when a compaction moves the head, and a state that merged the pushes would keep showing them.
	third := viewPush(t, 7, entryOf(t, 3, kernel.EntryAssistant, "after the compaction", 3000))
	if !s.Apply(third) {
		t.Fatal("the third whole view changed nothing")
	}
	if len(s.Entries()) != 1 || s.Entries()[0].ID != 3 {
		t.Fatalf("the state merged the pushes: %+v", s.Entries())
	}
}

// TestStateReplacesOnFull: a "full" push is the other whole shape — Kernel sends it to a delta subscriber when
// it cannot prove the ops are continuous. It replaces the state the same way, because the answer to "I cannot
// prove this is continuous" is "here is everything".
func TestStateReplacesOnFull(t *testing.T) {
	s := NewState(7)
	s.Apply(viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "one", 1000)))

	whole := kernel.View{Conversation: kernel.Conversation{ID: 7}, Entries: []kernel.Entry{
		entryOf(t, 1, kernel.EntryUser, "one", 1000),
		entryOf(t, 2, kernel.EntryAssistant, "two", 2000),
	}}
	full := kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindFull, Value: &whole}
	if !s.Apply(full) {
		t.Fatal("the full push changed nothing")
	}
	if len(s.Entries()) != 2 || s.Revision() != 2 {
		t.Fatalf("%d entries at revision %d, want the whole view", len(s.Entries()), s.Revision())
	}
}

// TestStateIgnoresADeltaPush: the window subscribes to whole views. A push with no value and only ops is a
// mistake in the wiring, and the state says so by changing nothing rather than by guessing what the ops mean.
func TestStateIgnoresADeltaPush(t *testing.T) {
	s := NewState(7)
	s.Apply(viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "one", 1000)))
	before := s.Revision()

	delta := kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Ops: json.RawMessage(`[["a",["entries"],{}]]`)}
	if s.Apply(delta) {
		t.Error("a delta push changed a window subscriber's state")
	}
	if s.Revision() != before || len(s.Entries()) != 1 {
		t.Fatalf("the state moved on a delta push: revision %d, %d entries", s.Revision(), len(s.Entries()))
	}
}

// TestStateKeepsTheViewWhenReadingFails: a failed view says Kernel could not read the conversation. What the
// window already shows stays — a transient read failure must not blank a transcript — and the error is kept to
// be shown.
func TestStateKeepsTheViewWhenReadingFails(t *testing.T) {
	s := NewState(7)
	s.Apply(viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "one", 1000)))

	failed := kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindFailed,
		Error: json.RawMessage(`{"message":"the database is locked","code":-32603}`)}
	if !s.Apply(failed) {
		t.Fatal("the failure changed nothing")
	}
	if s.LoadErr() != "the database is locked" {
		t.Errorf("the error reads %q, want Kernel's sentence", s.LoadErr())
	}
	if len(s.Entries()) != 1 {
		t.Errorf("the failure dropped the entries the window was showing: %+v", s.Entries())
	}

	// A view that arrives after it clears the error.
	s.Apply(viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "one", 1000), entryOf(t, 2, kernel.EntryAssistant, "two", 2000)))
	if s.LoadErr() != "" {
		t.Errorf("the error survived a good view: %q", s.LoadErr())
	}
}

// TestStateReadsTheDocumentsOfAView: a view carries the conversation's built-in documents, and the ones the
// composer and the sidebar ask about are pi.live (a run, its failure), pi.inbox (queued input) and pi.usage.
// A fresh conversation.open must answer all of them at once, without a second call and without a run having
// started since.
//
// The documents are the protocol's own shapes (apps/kernel/protocol/README.md): an inbox is {"items":[...]}, and
// a run is {taskId, inputs, turns}. A fixture of a shape the Kernel never sends would pass here and read nothing
// in the window — TestARealKernelAnswersTheInbox checks these against a Kernel that produced them.
func TestStateReadsTheDocumentsOfAView(t *testing.T) {
	view := kernel.View{
		Conversation: kernel.Conversation{ID: 7},
		Entries:      []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "one", 1000)},
		Docs: map[string]json.RawMessage{
			"pi.live":  json.RawMessage(`{"run":{"taskId":3,"inputs":[2],"turns":1},"runFailure":null}`),
			"pi.inbox": json.RawMessage(`{"items":[{"id":17,"mode":"followUp","content":"queued one"}]}`),
			"pi.usage": json.RawMessage(`{"models":{"factory/claude-sonnet-4-5":{"input":1200,"output":180,"totalTokens":1380}},"tools":{}}`),
		},
	}
	s := NewState(7)
	s.Apply(kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: &view})

	if !s.Busy() {
		t.Error("a view whose pi.live names a run reads as idle")
	}
	if len(s.Run()) == 0 || string(s.Run()) == "null" {
		t.Errorf("the run is %s, want the document's", s.Run())
	}
	if len(s.LiveDocument()) == 0 {
		t.Error("the live document is empty although the view carried it")
	}
	items := s.InboxItems()
	if len(items) != 1 {
		t.Fatalf("the inbox reads %d items, want the one the view carried: %s", len(items), s.UsageDocument())
	}
	if items[0].ID != 17 || items[0].Mode != "followUp" || string(items[0].Content) != `"queued one"` {
		t.Errorf("the queued item reads %+v", items[0])
	}
	if len(s.UsageDocument()) == 0 {
		t.Error("the usage document is empty although the view carried it")
	}
	if !json.Valid(s.UsageDocument()) {
		t.Errorf("the usage document is not JSON: %s", s.UsageDocument())
	}

	// A view that carries no documents at all — the conversation was read while nothing had happened to it —
	// leaves nothing behind. A state that kept the last inbox would keep answering for a document the view no
	// longer has, and the composer would draw a queued message nobody queued.
	bare := kernel.View{Conversation: kernel.Conversation{ID: 7}, Entries: []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "one", 1000)}}
	s.Apply(kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: &bare})
	if len(s.InboxItems()) != 0 || len(s.Run()) != 0 || len(s.Failure()) != 0 || len(s.LiveDocument()) != 0 || len(s.UsageDocument()) != 0 {
		t.Errorf("a view with no documents kept the previous one's: inbox %+v run %s failure %s live %s usage %s",
			s.InboxItems(), s.Run(), s.Failure(), s.LiveDocument(), s.UsageDocument())
	}

	// A conversation that is idle, holds nothing queued and has spent nothing: the documents are there and they
	// say so, which is different from not being there.
	idle := kernel.View{Conversation: kernel.Conversation{ID: 7}, Docs: map[string]json.RawMessage{
		"pi.live": json.RawMessage(`{}`), "pi.inbox": json.RawMessage(`{"items":[]}`),
		"pi.usage": json.RawMessage(`{"models":{},"tools":{}}`),
	}}
	s.Apply(kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: &idle})
	if s.Busy() {
		t.Error("the state still reads as busy after a view that names no run")
	}
	if len(s.InboxItems()) != 0 {
		t.Errorf("an empty inbox reads as %+v", s.InboxItems())
	}
	if len(s.UsageDocument()) == 0 || len(s.LiveDocument()) == 0 {
		t.Error("the documents a view carries are not read when they say nothing")
	}
}

// TestStateTakesBusy: conversation.busy is the run that started or ended. It reaches every connection, so the
// state hears about a run on a conversation another client started.
func TestStateTakesBusy(t *testing.T) {
	s := NewState(7)
	started := kernel.BusyNotification{ID: 7, Run: json.RawMessage(`{"taskId":3,"inputs":[2],"turns":0}`)}
	if !s.ApplyBusy(started) {
		t.Fatal("a started run changed nothing")
	}
	if !s.Busy() {
		t.Error("the state reads idle after a run started")
	}

	// The run ended badly: the sentence stays with the state, because a failed turn is what the page explains.
	end := kernel.BusyNotification{ID: 7, Run: json.RawMessage(`null`),
		RunFailure: json.RawMessage(`{"taskId":3,"reason":"model_error"}`)}
	if !s.ApplyBusy(end) {
		t.Fatal("the end of a run changed nothing")
	}
	if s.Busy() {
		t.Error("the state reads busy after the run ended")
	}
	if len(s.Failure()) == 0 {
		t.Error("the run's failure was dropped")
	}
	if string(s.Run()) != "null" && len(s.Run()) != 0 {
		t.Errorf("the ended run is kept as %s", s.Run())
	}

	// A run starting again clears the previous failure: it is about the turn that ended, not this one.
	s.ApplyBusy(started)
	if len(s.Failure()) != 0 {
		t.Errorf("the old failure survived a new run: %s", s.Failure())
	}
}

// TestStateTakesUsage: conversation.usage answers what the conversation spent by model and by tool, and how full
// its context is. It is a cheap call, so the state holds the last answer and the composer reads it.
func TestStateTakesUsage(t *testing.T) {
	s := NewState(7)
	u := kernel.Usage{
		Models:  json.RawMessage(`{"factory/claude-sonnet-4-5":{"input":1200}}`),
		Context: json.RawMessage(`{"used":1200,"limit":200000,"compactionAt":900}`),
	}
	if !s.ApplyUsage(u) {
		t.Fatal("usage changed nothing")
	}
	if string(s.UsageContext()) != `{"used":1200,"limit":200000,"compactionAt":900}` {
		t.Errorf("the context reads %s", s.UsageContext())
	}
	if s.ApplyUsage(u) {
		t.Error("the same usage twice counts as a change")
	}
}

// TestStoreOpensAndDraws: a conversation is opened once, and its first answer is the state.
func TestStoreOpensAndDraws(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{
		7: {Conversation: kernel.Conversation{ID: 7}, Entries: []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "one", 1000)}},
	}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := st.Open(ctx, k, 7, kernel.Window{Min: 100, Max: 200}); err != nil {
		t.Fatalf("open: %v", err)
	}
	s := st.State(7)
	if s == nil || len(s.Entries()) != 1 || !s.Opened() {
		t.Fatalf("the store holds an unopened state for conversation 7")
	}
	if got := k.opened(); len(got) != 1 || got[0] != 7 {
		t.Errorf("the store opened %v, want conversation 7", got)
	}

	// Opening again is a no-op: Kernel would replace the window, and the window has not changed.
	if err := st.Open(ctx, k, 7, kernel.Window{Min: 100, Max: 200}); err != nil {
		t.Fatalf("second open: %v", err)
	}
	if got := k.opened(); len(got) != 1 {
		t.Errorf("the second open reached Kernel as well: %v", got)
	}
}

// TestStoreAsksForWholeViews: the state holds whole views and ignores ops, which is right only while the
// subscription is "view". The protocol's default is that value, so leaving it out works and nothing would notice
// the day someone wrote "delta": every push would carry value: null, Apply would ignore all of them, and every
// page would freeze on its first snapshot with no error anywhere.
func TestStoreAsksForWholeViews(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}
	params := k.openParams()
	if len(params) != 1 {
		t.Fatalf("%d opens reached Kernel, want one", len(params))
	}
	if params[0].Subscribe != kernel.SubscribeView {
		t.Fatalf("the open subscribed to %q, want %q", params[0].Subscribe, kernel.SubscribeView)
	}
	if params[0].Window == nil || params[0].Window.Min != 10 || params[0].Window.Max != 20 {
		t.Errorf("the open asked for window %+v", params[0].Window)
	}
}

// TestStoreKeepsTheLastFewViews: a window keeps a few sessions' views and closes the oldest, as the app's
// keptViews does — but now closing means unwatching, so Kernel stops pushing to a connection nobody draws.
func TestStoreKeepsTheLastFewViews(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{}}
	for _, id := range []kernel.ID{1, 2, 3, 4} {
		k.views[id] = kernel.View{Conversation: kernel.Conversation{ID: id}}
	}
	st := NewStore(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, id := range []kernel.ID{1, 2, 3} {
		if err := st.Open(ctx, k, id, kernel.Window{Min: 100, Max: 200}); err != nil {
			t.Fatalf("open %d: %v", id, err)
		}
	}
	if st.State(1) != nil {
		t.Error("the oldest view is still held")
	}
	if st.State(2) == nil || st.State(3) == nil {
		t.Error("the two newest views are not both held")
	}
	if got := k.unwatched(); len(got) != 1 || got[0] != 1 {
		t.Errorf("unwatched %v, want conversation 1 once", got)
	}
}

// TestStoreKeepsTheConversationsUsedLast: the limit drops the conversation used longest ago, not the one opened
// longest ago. Going back to a session is the commonest thing a window does, and evicting the page the user just
// returned to would blank the transcript and stop Kernel's pushes for it.
func TestStoreKeepsTheConversationsUsedLast(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{}}
	for _, id := range []kernel.ID{1, 2, 3, 4} {
		k.views[id] = kernel.View{Conversation: kernel.Conversation{ID: id}}
	}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	open := func(id kernel.ID) {
		t.Helper()
		if err := st.Open(ctx, k, id, kernel.Window{Min: 10, Max: 20}); err != nil {
			t.Fatalf("open %d: %v", id, err)
		}
	}
	open(1)
	open(2)
	open(3)

	// Back to 1, which is the conversation on screen again. It is held, so Kernel is not asked, but the use is
	// recorded: 2 is now the one used longest ago.
	open(1)
	if got := k.opened(); len(got) != 3 {
		t.Fatalf("going back to a held conversation opened it again: %v", got)
	}

	open(4)
	if st.State(1) == nil {
		t.Error("the conversation the window is on was evicted")
	}
	if st.State(2) != nil {
		t.Error("the conversation used longest ago is still held")
	}
	if st.State(3) == nil || st.State(4) == nil {
		t.Error("the two most recent conversations are not both held")
	}
	if got := k.unwatched(); len(got) != 1 || got[0] != 2 {
		t.Errorf("unwatched %v, want conversation 2 only", got)
	}
}

// TestStoreKeepsARunningConversation: a conversation with a run in progress is never evicted. The app has always
// kept a busy session's view, and the Workstreams panel tells "who is working" from the open conversations' runs,
// so evicting one would make a working agent disappear mid-turn.
func TestStoreKeepsARunningConversation(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{}}
	for _, id := range []kernel.ID{1, 2, 3, 4} {
		k.views[id] = kernel.View{Conversation: kernel.Conversation{ID: id}}
	}
	st := NewStore(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, id := range []kernel.ID{1, 2} {
		if err := st.Open(ctx, k, id, kernel.Window{Min: 10, Max: 20}); err != nil {
			t.Fatalf("open %d: %v", id, err)
		}
	}
	st.ApplyBusy(0, kernel.BusyNotification{ID: 2, Run: json.RawMessage(`{"taskId":9,"inputs":[8],"turns":0}`)})

	// The limit is one and 2 is running: 1 goes, 2 stays and is the conversation on screen.
	if err := st.Open(ctx, k, 3, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}
	if st.State(2) == nil || !st.State(2).Busy() {
		t.Error("the running conversation was evicted")
	}
	if st.State(3) == nil {
		t.Error("the conversation just opened is not held")
	}
	if st.State(1) != nil {
		t.Error("the idle conversation was kept over the running one")
	}

	// Both remaining conversations are running: the limit gives way rather than the run.
	st.ApplyBusy(0, kernel.BusyNotification{ID: 3, Run: json.RawMessage(`{"taskId":10,"inputs":[9],"turns":0}`)})
	if err := st.Open(ctx, k, 4, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}
	if st.State(2) == nil || st.State(3) == nil || st.State(4) == nil {
		t.Fatalf("a limit of one dropped a running conversation instead of giving way")
	}
}

func TestStoreKeepsRecentViewsInAdditionToBusyOnes(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}, 2: {}, 3: {}, 4: {}}}
	st := NewStore(2)
	ctx := context.Background()
	if err := st.Open(ctx, k, 1, kernel.Window{}); err != nil {
		t.Fatal(err)
	}
	st.ApplyBusy(0, kernel.BusyNotification{ID: 1, Run: json.RawMessage(`{"taskId":9}`)})
	for _, id := range []kernel.ID{2, 3} {
		if err := st.Open(ctx, k, id, kernel.Window{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []kernel.ID{1, 2, 3} {
		if st.State(id) == nil {
			t.Fatalf("conversation %d was dropped: keep the recent two plus the busy one", id)
		}
	}
	if err := st.Open(ctx, k, 4, kernel.Window{}); err != nil {
		t.Fatal(err)
	}
	if st.State(1) == nil || st.State(2) != nil || st.State(3) == nil || st.State(4) == nil {
		t.Fatal("want the busy conversation and the two most recently used views")
	}
}

func TestStoreTrimsAnIdleExemptionWhenReturningToAHeldView(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}, 2: {}}}
	st := NewStore(1)
	ctx := context.Background()
	if err := st.Open(ctx, k, 1, kernel.Window{}); err != nil {
		t.Fatal(err)
	}
	st.ApplyBusy(0, kernel.BusyNotification{ID: 1, Run: json.RawMessage(`{"taskId":9}`)})
	if err := st.Open(ctx, k, 2, kernel.Window{}); err != nil {
		t.Fatal(err)
	}
	st.ApplyBusy(0, kernel.BusyNotification{ID: 1})
	if err := st.Open(ctx, k, 2, kernel.Window{}); err != nil {
		t.Fatal(err)
	}
	if st.State(1) != nil || st.State(2) == nil {
		t.Fatal("returning to a held view did not trim the now-idle exemption")
	}
	if got := k.unwatched(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("unwatched %v, want only the idle exemption", got)
	}
}

func TestStoreKeepsAReopenedStateWhenTheOldOpenFails(t *testing.T) {
	st := NewStore(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old := &fakeKernel{entered: make(chan kernel.ID, 1), release: make(chan struct{}),
		answerErr: errors.New("old connection closed")}
	done := make(chan error, 1)
	go func() { done <- st.Open(ctx, old, 1, kernel.Window{}) }()
	select {
	case <-old.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	current := &fakeKernel{views: map[kernel.ID]kernel.View{
		1: {Entries: []kernel.Entry{entryOf(t, 1, kernel.EntryAssistant, "recovered", 1000)}},
	}}
	err := st.Reopen(ctx, current, 1)
	close(old.release)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if s := st.State(1); s == nil || len(s.Entries()) != 1 {
		t.Fatal("the failed old open deleted the new generation's state")
	}
}

func TestZeroStoreOpensRoutesReopensAndCloses(t *testing.T) {
	var st Store
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}, 2: {}}}
	ctx := context.Background()
	// Open must work as the very first operation, without Max or Subscribe warming up the maps.
	if err := st.Open(ctx, k, 1, kernel.Window{}); err != nil {
		t.Fatal(err)
	}
	var changed []kernel.ID
	stop := st.Subscribe(func(id kernel.ID) {
		// A subscriber may read the store: callbacks must not hold either lock.
		if st.State(id) == nil {
			t.Errorf("notification for missing state %d", id)
		}
		changed = append(changed, id)
	})
	if st.Max() != 1 || st.Generation() != 0 {
		t.Fatal("a zero store must keep one conversation on generation zero")
	}
	if !st.ApplyView(0, viewPush(t, 1, entryOf(t, 1, kernel.EntryUser, "one", 1000))) {
		t.Fatal("the zero store dropped a push for its open conversation")
	}
	if err := st.Reopen(ctx, k, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.Open(ctx, k, 2, kernel.Window{}); err != nil {
		t.Fatal(err)
	}
	if st.State(1) != nil || st.State(2) == nil || len(changed) != 3 {
		t.Fatalf("the zero store did not evict or notify: changes %v", changed)
	}
	stop()
	st.ApplyBusy(1, kernel.BusyNotification{ID: 2, Run: json.RawMessage(`{"taskId":9}`)})
	if len(changed) != 3 {
		t.Fatal("unsubscribe did not stop notifications")
	}
	st.Close(ctx, k, 2)
	if st.State(2) != nil {
		t.Fatal("the zero store kept a closed conversation")
	}
}

// TestStoreKeepsAPushThatArrivesDuringTheOpen: Kernel subscribes before it answers conversation.open, so a push
// can arrive while the call is in flight. What it holds is at least as new as the answer, and dropping it would
// lose the difference — which, for the last push of a turn, is the answer the page is waiting for and nothing
// else would make it rebuild.
func TestStoreKeepsAPushThatArrivesDuringTheOpen(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{
		7: {Conversation: kernel.Conversation{ID: 7}, Entries: []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "one", 1000)}},
	}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	k.onOpen = func(p kernel.OpenParams) {
		st.ApplyView(0, viewPush(t, p.ID,
			entryOf(t, 1, kernel.EntryUser, "one", 1000),
			entryOf(t, 2, kernel.EntryAssistant, "the answer that landed while opening", 2000)))
	}

	if err := st.Open(ctx, k, 7, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}
	if got := len(st.State(7).Entries()); got != 2 {
		t.Fatalf("the state holds %d entries, want the push that arrived during the open", got)
	}
}

// TestStoreAsksOnceForTwoOpensOfOneConversation: two goroutines can open one conversation at once — the app
// reloads the list while the page opens — and both reaching Kernel would replace the window twice and record the
// use twice, which the next eviction reads as one conversation too many.
func TestStoreAsksOnceForTwoOpensOfOneConversation(t *testing.T) {
	k := &fakeKernel{
		views:   map[kernel.ID]kernel.View{1: {}},
		entered: make(chan kernel.ID, 1),
		release: make(chan struct{}),
	}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first := make(chan error, 1)
	go func() { first <- st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}) }()
	<-k.entered // the first open is in flight, and its state is held

	second := make(chan error, 1)
	go func() { second <- st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}) }()
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("the second open answered %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second open waited for the first instead of finding the conversation held")
	}

	close(k.release)
	if err := <-first; err != nil {
		t.Fatalf("the first open answered %v", err)
	}
	if got := k.opened(); len(got) != 1 {
		t.Errorf("the two opens reached Kernel %d times, want once: %v", len(got), got)
	}
	if st.State(1) == nil {
		t.Error("the conversation is not held after both opens")
	}
}

// TestStorePassesThePushesToTheRightState: one connection hears every conversation it opened, so the store
// routes each push by its id — and a push for a conversation nobody opened is not an error to the others.
func TestStorePassesThePushesToTheRightState(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}, 2: {}}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range []kernel.ID{1, 2} {
		if err := st.Open(ctx, k, id, kernel.Window{Min: 10, Max: 20}); err != nil {
			t.Fatal(err)
		}
	}

	var seen []kernel.ID
	var mu sync.Mutex
	stop := st.Subscribe(func(id kernel.ID) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, id)
	})
	defer stop()

	st.ApplyView(0, viewPush(t, 2, entryOf(t, 5, kernel.EntryUser, "two", 1000)))
	st.ApplyBusy(0, kernel.BusyNotification{ID: 1, Run: json.RawMessage(`{"taskId":9}`)})
	st.ApplyView(0, viewPush(t, 99, entryOf(t, 6, kernel.EntryUser, "nobody opened this", 1000)))

	mu.Lock()
	got := append([]kernel.ID(nil), seen...)
	mu.Unlock()
	if len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Errorf("the subscriber heard %v, want [2 1]", got)
	}
	if st.State(2).Entries()[0].ID != 5 {
		t.Error("the push for 2 did not land on 2")
	}
	if !st.State(1).Busy() {
		t.Error("the busy push for 1 did not land on 1")
	}
}

// TestStoreStopsPushingAfterUnsubscribe: the app watches the store to redraw; a view that went away must not
// keep its watcher awake.
func TestStoreStopsPushingAfterUnsubscribe(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}
	n := 0
	stop := st.Subscribe(func(kernel.ID) { n++ })
	stop()
	st.ApplyView(0, viewPush(t, 1, entryOf(t, 1, kernel.EntryUser, "x", 1000)))
	if n != 0 {
		t.Errorf("the stopped watcher was called %d times", n)
	}
}

// TestStoreCloses: closing a conversation unwatches it and drops the state; a later push for it is ignored.
func TestStoreCloses(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}
	st.Close(ctx, k, 1)
	if st.State(1) != nil {
		t.Error("the closed conversation is still held")
	}
	if got := k.unwatched(); len(got) != 1 || got[0] != 1 {
		t.Errorf("unwatched %v", got)
	}
	st.ApplyView(0, viewPush(t, 1, entryOf(t, 1, kernel.EntryUser, "x", 1000)))
	if st.State(1) != nil {
		t.Error("a push revived a closed conversation")
	}
}

// TestStoreReportsAFailedOpen: an open that Kernel refuses leaves nothing held and answers the reason, so the
// page can say why instead of waiting for a view that will not come.
func TestStoreReportsAFailedOpen(t *testing.T) {
	k := &fakeKernel{openErr: errors.New("conversation not found")}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := st.Open(ctx, k, 42, kernel.Window{Min: 10, Max: 20})
	if err == nil || !strings.Contains(err.Error(), "conversation not found") {
		t.Fatalf("open answered %v, want Kernel's reason", err)
	}
	if st.State(42) != nil {
		t.Error("a failed open left a state behind")
	}
	if err := st.Open(ctx, k, 42, kernel.Window{Min: 10, Max: 20}); err == nil {
		t.Error("the failed conversation cannot be opened again")
	}
}

// TestStoreReopensEveryConversationOnANewConnection: a Kernel restart ends the connection, and every
// subscription goes with it while the states stay. Without a second open on the new connection nothing ever
// pushes again: the page keeps the transcript it had, a sent message is accepted and stored, and its answer never
// arrives — the only cure being to quit the app.
func TestStoreReopensEveryConversationOnANewConnection(t *testing.T) {
	first := &fakeKernel{views: map[kernel.ID]kernel.View{
		1: {Conversation: kernel.Conversation{ID: 1}, Entries: []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "before", 1000)}},
		2: {Conversation: kernel.Conversation{ID: 2}, Entries: []kernel.Entry{entryOf(t, 2, kernel.EntryUser, "before", 1000)}},
	}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range []kernel.ID{1, 2} {
		if err := st.Open(ctx, first, id, kernel.Window{Min: 10, Max: 20}); err != nil {
			t.Fatal(err)
		}
	}

	// The new Kernel has both conversations and one more entry in each.
	second := &fakeKernel{views: map[kernel.ID]kernel.View{
		1: {Conversation: kernel.Conversation{ID: 1}, Entries: []kernel.Entry{
			entryOf(t, 1, kernel.EntryUser, "before", 1000), entryOf(t, 3, kernel.EntryAssistant, "after", 2000)}},
		2: {Conversation: kernel.Conversation{ID: 2}, Entries: []kernel.Entry{
			entryOf(t, 2, kernel.EntryUser, "before", 1000), entryOf(t, 4, kernel.EntryAssistant, "after", 2000)}},
	}}
	if err := st.Reopen(ctx, second, 1); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := second.opened(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("the new connection opened %v, want both conversations in use order", got)
	}
	for _, id := range []kernel.ID{1, 2} {
		if got := len(st.State(id).Entries()); got != 2 {
			t.Errorf("conversation %d holds %d entries, want the whole view the new Kernel answered", id, got)
		}
	}
	if got := st.Generation(); got != 1 {
		t.Errorf("the store is on generation %d, want 1", got)
	}
	// The window asked for the same window it had: reopening is not a reason to show a different page.
	if p := second.openParams()[0]; p.Window == nil || p.Window.Min != 10 || p.Window.Max != 20 {
		t.Errorf("the re-open asked for window %+v", p.Window)
	}
}

// TestStoreDropsAPushFromAnEarlierGeneration: a push already on its way when the connection ended names
// yesterday's Kernel. Applying it would put an older view over the one the new Kernel answered.
func TestStoreDropsAPushFromAnEarlierGeneration(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{
		1: {Conversation: kernel.Conversation{ID: 1}, Entries: []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "one", 1000)}},
	}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}
	if err := st.Reopen(ctx, k, 1); err != nil {
		t.Fatal(err)
	}

	before := st.State(1).Revision()
	if st.ApplyView(0, viewPush(t, 1, entryOf(t, 9, kernel.EntryUser, "yesterday's Kernel", 1000))) {
		t.Error("a push from the generation that ended was applied")
	}
	if st.State(1).Revision() != before || len(st.State(1).Entries()) != 1 {
		t.Error("the old generation's push moved the state")
	}
	if st.ApplyBusy(0, kernel.BusyNotification{ID: 1, Run: json.RawMessage(`{"taskId":9}`)}) {
		t.Error("an old generation's busy notification was applied")
	}
	if st.ApplyUsage(0, 1, kernel.Usage{Context: json.RawMessage(`{"used":9}`)}) {
		t.Error("an old generation's usage answer was applied")
	}
	if st.State(1).Busy() || len(st.State(1).UsageContext()) != 0 {
		t.Error("the old generation changed the run or usage")
	}

	if !st.ApplyView(1, viewPush(t, 1,
		entryOf(t, 1, kernel.EntryUser, "one", 1000),
		entryOf(t, 2, kernel.EntryAssistant, "today's", 2000))) {
		t.Fatal("a push from the current generation was dropped")
	}
	if got := len(st.State(1).Entries()); got != 2 {
		t.Errorf("the current generation's push left %d entries", got)
	}
}

// TestStoreKeepsTheEntriesOfAConversationItCannotReopen: a conversation the new Kernel refuses to answer keeps
// what the window was showing and records the sentence, the same as a failed read. Blanking it would turn a
// restart into an empty page.
func TestStoreKeepsTheEntriesOfAConversationItCannotReopen(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{
		1: {Conversation: kernel.Conversation{ID: 1}, Entries: []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "one", 1000)}},
	}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}

	refusing := &fakeKernel{openErr: errors.New("conversation not found")}
	err := st.Reopen(ctx, refusing, 1)
	if err == nil || !strings.Contains(err.Error(), "conversation not found") {
		t.Fatalf("reopen answered %v, want the Kernel's reason", err)
	}
	if got := len(st.State(1).Entries()); got != 1 {
		t.Errorf("the conversation the Kernel refused holds %d entries, want the one it had", got)
	}
	if st.State(1).LoadErr() == "" {
		t.Error("the refusal was not recorded for the page to show")
	}
}

// TestStoreTakesPushesFromTwoGoroutines: a view push arrives on the connection's read loop and a usage answer
// comes back on whichever goroutine asked for it, so the two land on one state at once. Every field is behind the
// state's lock, and this test is what says so: it is the race detector, not an assertion, that fails it.
func TestStoreTakesPushesFromTwoGoroutines(t *testing.T) {
	k := &fakeKernel{views: map[kernel.ID]kernel.View{1: {}}}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.Open(ctx, k, 1, kernel.Window{Min: 10, Max: 20}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			st.ApplyView(0, viewPush(t, 1, entryOf(t, kernel.ID(i+1), kernel.EntryUser, "one", float64(i))))
			st.ApplyBusy(0, kernel.BusyNotification{ID: 1, Run: json.RawMessage(`{"taskId":9}`)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			st.ApplyUsage(0, 1, kernel.Usage{Context: json.RawMessage("{}")})
			_ = st.State(1).InboxItems()
			_ = st.State(1).Revision()
		}
	}()
	wg.Wait()
	if got := st.State(1).Revision(); got == 0 {
		t.Error("the state never moved through 400 pushes")
	}
}

// fakeKernel is the client a store needs, with the opens and unwatches it was asked for, and the hooks a test of
// the round trip itself needs.
type fakeKernel struct {
	mu        sync.Mutex
	views     map[kernel.ID]kernel.View
	openErr   error
	answerErr error
	params    []kernel.OpenParams
	unwatch_  []kernel.ID
	// onOpen runs before the call answers, which is where a test puts a push that arrives during it.
	onOpen func(kernel.OpenParams)
	// entered is signalled with the id when an open starts, and release holds it there: together they keep one
	// call in flight while the test makes another.
	entered chan kernel.ID
	release chan struct{}
	// unwatchEntered and unwatchRelease do the same for an unwatch.
	unwatchEntered chan kernel.ID
	unwatchRelease chan struct{}
	// calls is every open and unwatch in the order they reached the fake, as "open 1" and "unwatch 1".
	calls []string
	// entries answers conversation.entries; entriesParams are the calls it got.
	entries       func(kernel.EntriesParams) (kernel.EntriesResult, error)
	entriesParams []kernel.EntriesParams
	// abort answers conversation.abort; submissions answer submission.get.
	abort       func(kernel.AbortParams) error
	submissions map[kernel.ID]kernel.SubmissionGet
}

func (f *fakeKernel) Open(ctx context.Context, p kernel.OpenParams) (kernel.OpenResult, error) {
	f.mu.Lock()
	err := f.openErr
	onOpen, entered, release := f.onOpen, f.entered, f.release
	if err == nil {
		f.params = append(f.params, p)
	}
	f.calls = append(f.calls, fmt.Sprintf("open %d", p.ID))
	f.mu.Unlock()
	if err != nil {
		return kernel.OpenResult{}, err
	}
	if onOpen != nil {
		onOpen(p)
	}
	if entered != nil {
		entered <- p.ID
	}
	if release != nil {
		<-release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.answerErr != nil {
		return kernel.OpenResult{}, f.answerErr
	}
	return kernel.OpenResult{View: f.views[p.ID]}, nil
}

func (f *fakeKernel) Unwatch(ctx context.Context, id kernel.ID) error {
	f.mu.Lock()
	entered, release := f.unwatchEntered, f.unwatchRelease
	f.calls = append(f.calls, fmt.Sprintf("unwatch %d", id))
	f.mu.Unlock()
	if entered != nil {
		entered <- id
	}
	if release != nil {
		<-release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unwatch_ = append(f.unwatch_, id)
	return nil
}

func (f *fakeKernel) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeKernel) opened() []kernel.ID {
	ids := []kernel.ID{}
	for _, p := range f.openParams() {
		ids = append(ids, p.ID)
	}
	return ids
}

func (f *fakeKernel) openParams() []kernel.OpenParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]kernel.OpenParams(nil), f.params...)
}

func (f *fakeKernel) unwatched() []kernel.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]kernel.ID(nil), f.unwatch_...)
}

// The rest of the client's conversation half, so the fake satisfies the interface a View will use. A store test
// never gets here.
func (f *fakeKernel) Submit(context.Context, kernel.SubmitParams) (kernel.SubmitResult, error) {
	return kernel.SubmitResult{}, errors.New("not in this test")
}

func (f *fakeKernel) Abort(_ context.Context, p kernel.AbortParams) error {
	f.mu.Lock()
	answer := f.abort
	f.mu.Unlock()
	if answer == nil {
		return errors.New("not in this test")
	}
	return answer(p)
}

func (f *fakeKernel) AbortSubmission(context.Context, kernel.AbortSubmissionParams) (kernel.Status, error) {
	return kernel.Status{}, errors.New("not in this test")
}

func (f *fakeKernel) SubmissionGet(_ context.Context, p kernel.AbortSubmissionParams) (kernel.SubmissionGet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.submissions[p.SubmissionID]
	if !ok {
		return kernel.SubmissionGet{}, errors.New("not in this test")
	}
	return rec, nil
}

func (f *fakeKernel) Configure(context.Context, kernel.ConfigureParams) (kernel.ConfigureResult, error) {
	return kernel.ConfigureResult{}, errors.New("not in this test")
}

func (f *fakeKernel) Entries(_ context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error) {
	f.mu.Lock()
	f.entriesParams = append(f.entriesParams, p)
	answer := f.entries
	f.mu.Unlock()
	if answer == nil {
		return kernel.EntriesResult{}, errors.New("not in this test")
	}
	return answer(p)
}

func (f *fakeKernel) Usage(context.Context, kernel.ID) (kernel.Usage, error) {
	return kernel.Usage{}, errors.New("not in this test")
}

func (f *fakeKernel) Processes(context.Context, kernel.ID) (kernel.ProcessesResult, error) {
	return kernel.ProcessesResult{}, errors.New("not in this test")
}

func (f *fakeKernel) StopProcess(context.Context, kernel.StopProcessParams) (kernel.Status, error) {
	return kernel.Status{}, errors.New("not in this test")
}

// TestAViewResolvesItsConversationID: the window names a Session by the string its routes and preferences use,
// and Kernel names it by number. The default reads the number out of the string; an app that keeps a mapping
// hands one over instead. A string that is not a number is not a conversation, and the View says so rather than
// calling Kernel with a zero.
func TestAViewResolvesItsConversationID(t *testing.T) {
	v := New("17", Env{})
	if id, ok := v.convID(); !ok || id != 17 {
		t.Fatalf("17 resolved to %v, %v", id, ok)
	}
	if _, ok := New("chat-1", Env{}).convID(); ok {
		t.Error("a string that is not a number resolved to a conversation")
	}

	custom := New("chat-1", Env{ConvID: func(id string) (kernel.ID, bool) { return 42, id == "chat-1" }})
	if id, ok := custom.convID(); !ok || id != 42 {
		t.Fatalf("the app's resolver answered %v, %v", id, ok)
	}
	if _, ok := New("other", Env{ConvID: func(string) (kernel.ID, bool) { return 0, false }}).convID(); ok {
		t.Error("a resolver that refused the id still resolved it")
	}
}
