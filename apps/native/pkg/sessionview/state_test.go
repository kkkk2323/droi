package sessionview

import (
	"context"
	"encoding/json"
	"errors"
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
	if s.Revision != 0 || s.Opened() {
		t.Fatalf("a new state is %+v, want an empty unopened one", s)
	}

	first := viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "hello", 1000))
	if !s.Apply(first) {
		t.Fatal("the first whole view changed nothing")
	}
	if !s.Opened() || len(s.Entries()) != 1 || s.Entries()[0].ID != 1 {
		t.Fatalf("the state holds %+v", s)
	}
	if s.Revision != 1 {
		t.Errorf("revision %d, want 1", s.Revision)
	}

	second := viewPush(t, 7,
		entryOf(t, 1, kernel.EntryUser, "hello", 1000),
		entryOf(t, 2, kernel.EntryAssistant, "hi", 2000))
	if !s.Apply(second) {
		t.Fatal("the second whole view changed nothing")
	}
	if len(s.Entries()) != 2 || s.Revision != 2 {
		t.Fatalf("the state holds %d entries at revision %d", len(s.Entries()), s.Revision)
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
	if len(s.Entries()) != 2 || s.Revision != 2 {
		t.Fatalf("%d entries at revision %d, want the whole view", len(s.Entries()), s.Revision)
	}
}

// TestStateIgnoresADeltaPush: the window subscribes to whole views. A push with no value and only ops is a
// mistake in the wiring, and the state says so by changing nothing rather than by guessing what the ops mean.
func TestStateIgnoresADeltaPush(t *testing.T) {
	s := NewState(7)
	s.Apply(viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "one", 1000)))
	before := s.Revision

	delta := kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Ops: json.RawMessage(`[["a",["entries"],{}]]`)}
	if s.Apply(delta) {
		t.Error("a delta push changed a window subscriber's state")
	}
	if s.Revision != before || len(s.Entries()) != 1 {
		t.Fatalf("the state moved on a delta push: revision %d, %d entries", s.Revision, len(s.Entries()))
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
	if s.LoadErr != "the database is locked" {
		t.Errorf("the error reads %q, want Kernel's sentence", s.LoadErr)
	}
	if len(s.Entries()) != 1 {
		t.Errorf("the failure dropped the entries the window was showing: %+v", s.Entries())
	}

	// A view that arrives after it clears the error.
	s.Apply(viewPush(t, 7, entryOf(t, 1, kernel.EntryUser, "one", 1000), entryOf(t, 2, kernel.EntryAssistant, "two", 2000)))
	if s.LoadErr != "" {
		t.Errorf("the error survived a good view: %q", s.LoadErr)
	}
}

// TestStateReadsTheDocumentsOfAView: a view carries the conversation's built-in documents, and three of them are
// what the composer and the sidebar ask about: pi.live (a run, its failure), pi.inbox (queued input), pi.usage.
// A fresh conversation.open must answer all of them at once, without a second call and without a run having
// started since.
func TestStateReadsTheDocumentsOfAView(t *testing.T) {
	view := kernel.View{
		Conversation: kernel.Conversation{ID: 7},
		Entries:      []kernel.Entry{entryOf(t, 1, kernel.EntryUser, "one", 1000)},
		Docs: map[string]json.RawMessage{
			"pi.live":  json.RawMessage(`{"run":{"taskId":3,"turns":1,"phase":"executing_tool"},"runFailure":null}`),
			"pi.inbox": json.RawMessage(`{"queued":[{"id":1,"text":"later"}]}`),
			"pi.usage": json.RawMessage(`{"models":{"factory/claude-sonnet-4-5":{"input":1200,"output":180,"totalTokens":1380}},"tools":{}}`),
		},
	}
	s := NewState(7)
	s.Apply(kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: &view})

	if !s.Busy() {
		t.Error("a view whose pi.live names a run reads as idle")
	}
	if len(s.Run) == 0 || string(s.Run) == "null" {
		t.Errorf("the run is %s, want the document's", s.Run)
	}
	if len(s.Inbox) == 0 {
		t.Error("the inbox is empty although the view carried it")
	}
	if len(s.UsageDocument()) == 0 {
		t.Error("the usage document is empty although the view carried it")
	}
	if !json.Valid(s.UsageDocument()) {
		t.Errorf("the usage document is not JSON: %s", s.UsageDocument())
	}

	// A conversation that is idle, holds nothing queued and has spent nothing: the documents say so, and the
	// state must not keep the previous view's answers.
	empty := kernel.View{Conversation: kernel.Conversation{ID: 7}, Docs: map[string]json.RawMessage{
		"pi.live": json.RawMessage(`{}`), "pi.inbox": json.RawMessage(`{}`), "pi.usage": json.RawMessage(`{"models":{},"tools":{}}`),
	}}
	s.Apply(kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: &empty})
	if s.Busy() {
		t.Error("the state still reads as busy after a view that names no run")
	}
	if len(s.Run) != 0 {
		t.Errorf("the old run survived: %s", s.Run)
	}
}

// TestStateTakesBusy: conversation.busy is the run that started or ended. It reaches every connection, so the
// state hears about a run on a conversation another client started.
func TestStateTakesBusy(t *testing.T) {
	s := NewState(7)
	started := kernel.BusyNotification{ID: 7, Run: json.RawMessage(`{"taskId":3,"turns":0,"phase":"streaming"}`)}
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
	if len(s.Failure) == 0 {
		t.Error("the run's failure was dropped")
	}
	if string(s.Run) != "null" && len(s.Run) != 0 {
		t.Errorf("the ended run is kept as %s", s.Run)
	}

	// A run starting again clears the previous failure: it is about the turn that ended, not this one.
	s.ApplyBusy(started)
	if len(s.Failure) != 0 {
		t.Errorf("the old failure survived a new run: %s", s.Failure)
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
	if s == nil || len(s.Entries()) != 1 {
		t.Fatalf("the store holds %+v", s)
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
	stop := st.Subscribe(func(id kernel.ID) { seen = append(seen, id) })
	defer stop()

	st.ApplyView(viewPush(t, 2, entryOf(t, 5, kernel.EntryUser, "two", 1000)))
	st.ApplyBusy(kernel.BusyNotification{ID: 1, Run: json.RawMessage(`{"taskId":9}`)})
	st.ApplyView(viewPush(t, 99, entryOf(t, 6, kernel.EntryUser, "nobody opened this", 1000)))

	if len(seen) != 2 || seen[0] != 2 || seen[1] != 1 {
		t.Errorf("the subscriber heard %v, want [2 1]", seen)
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
	st.ApplyView(viewPush(t, 1, entryOf(t, 1, kernel.EntryUser, "x", 1000)))
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
	st.ApplyView(viewPush(t, 1, entryOf(t, 1, kernel.EntryUser, "x", 1000)))
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
}

// fakeKernel is the client a store needs, with the opens and unwatches it was asked for.
type fakeKernel struct {
	mu       sync.Mutex
	views    map[kernel.ID]kernel.View
	openErr  error
	openIDs  []kernel.ID
	unwatch_ []kernel.ID
}

func (f *fakeKernel) Open(ctx context.Context, p kernel.OpenParams) (kernel.OpenResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.openErr != nil {
		return kernel.OpenResult{}, f.openErr
	}
	f.openIDs = append(f.openIDs, p.ID)
	return kernel.OpenResult{View: f.views[p.ID]}, nil
}

func (f *fakeKernel) Unwatch(ctx context.Context, id kernel.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unwatch_ = append(f.unwatch_, id)
	return nil
}

func (f *fakeKernel) opened() []kernel.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]kernel.ID(nil), f.openIDs...)
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

func (f *fakeKernel) Abort(context.Context, kernel.AbortParams) error {
	return errors.New("not in this test")
}

func (f *fakeKernel) AbortSubmission(context.Context, kernel.AbortSubmissionParams) (kernel.Status, error) {
	return kernel.Status{}, errors.New("not in this test")
}

func (f *fakeKernel) Configure(context.Context, kernel.ConfigureParams) (kernel.ConfigureResult, error) {
	return kernel.ConfigureResult{}, errors.New("not in this test")
}

func (f *fakeKernel) Entries(context.Context, kernel.EntriesParams) (kernel.EntriesResult, error) {
	return kernel.EntriesResult{}, errors.New("not in this test")
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
