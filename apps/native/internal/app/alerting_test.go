package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// A Session that finishes while another is open turns unread, plays the
// completion sound and notifies; opening it reads it.
func TestCompletionAlertsWhileAnotherSessionIsOpen(t *testing.T) {
	sc := fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Deploy", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}},
			{Title: "Notes", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "hello"}}},
		},
		Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"one ", "two ", "three"}, DelayMs: 300},
	}
	keepOnDaemon(t, untilUnread)
	h := newHarness(t, sc, "")
	var mu sync.Mutex
	var sounds []string
	var notes []Alert
	h.a.cfg.PlaySound = func(s string) { mu.Lock(); sounds = append(sounds, s); mu.Unlock() }
	h.a.cfg.Notify = func(n Alert) { mu.Lock(); notes = append(notes, n); mu.Unlock() }
	h.a.cfg.Focused = func() bool { return true }

	h.openSession("Deploy")
	h.send("Go")
	deploy, notesID := h.d.Sessions[0].SessionID, h.d.Sessions[1].SessionID
	h.openRow("Notes", notesID)
	h.until("the completion alert", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(notes) > 0
	})
	mu.Lock()
	if len(sounds) != 1 || sounds[0] != "fx-ok01" {
		t.Errorf("sounds %q", sounds)
	}
	if n := notes[0]; n.SessionID != deploy || n.Title != "Droi finished" || n.Body != "Deploy" {
		t.Errorf("notification %+v", n)
	}
	mu.Unlock()
	if !h.a.unread[deploy] {
		t.Fatal("Deploy is not unread")
	}
	if _, ok := h.tt.Find("Unread"); !ok {
		t.Error("no unread mark in the sidebar")
	}
	h.openRow("Deploy", deploy)
	if h.a.unread[deploy] {
		t.Error("opening Deploy did not read it")
	}
}

// The open Session finishing in a focused window plays the sound but
// neither notifies nor turns unread.
func TestNoNotificationForTheSessionInView(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"done"}}), "")
	var mu sync.Mutex
	var sounds []string
	var notes []Alert
	h.a.cfg.PlaySound = func(s string) { mu.Lock(); sounds = append(sounds, s); mu.Unlock() }
	h.a.cfg.Notify = func(n Alert) { mu.Lock(); notes = append(notes, n); mu.Unlock() }
	h.a.cfg.Focused = func() bool { return true }
	h.openSession("Deploy")
	h.send("Go")
	h.until("the sound", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sounds) > 0
	})
	mu.Lock()
	defer mu.Unlock()
	if len(notes) != 0 {
		t.Errorf("notified %+v", notes)
	}
	if len(h.a.unread) != 0 {
		t.Errorf("unread %v", h.a.unread)
	}
}

func TestSoundGate(t *testing.T) {
	for _, c := range []struct {
		mode    string
		focused bool
		want    bool
	}{{"always", false, true}, {"focused", true, true}, {"focused", false, false}, {"unfocused", true, false}, {"unfocused", false, true}} {
		if got := soundGateAllows(c.mode, c.focused); got != c.want {
			t.Errorf("%s focused=%v: %v", c.mode, c.focused, got)
		}
	}
}

// A Session drawn from Kernel alerts as the Daemon's did when its run ends while another is open: Kernel says so with
// conversation.busy, which reaches every connection, also for a conversation no page has open.
func TestAKernelSessionThatFinishesElsewhereAlerts(t *testing.T) {
	sc := fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Deploy", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}},
			{Title: "Notes", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "hello"}}},
		},
		Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"one ", "two ", "three"}, DelayMs: 300},
	}
	h := newHarness(t, sc, "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	var mu sync.Mutex
	var notes []Alert
	h.a.cfg.Notify = func(n Alert) { mu.Lock(); notes = append(notes, n); mu.Unlock() }
	h.a.cfg.Focused = func() bool { return true }

	h.openSession("Deploy")
	h.send("Go")
	deploy, notesID := h.d.Sessions[0].SessionID, h.d.Sessions[1].SessionID
	h.openRow("Notes", notesID)
	h.until("the completion alert", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(notes) > 0
	})
	mu.Lock()
	if n := notes[0]; n.SessionID != deploy || n.Title != "Droi finished" || n.Body != "Deploy" {
		t.Errorf("notification %+v", n)
	}
	mu.Unlock()
	if !h.a.unread[deploy] {
		t.Fatal("Deploy is not unread")
	}
}

// A window that connects while a conversation's run is under way, and opens it, has not heard the run start: Kernel
// says busy once, when it starts. The window still takes the run as working, and its end raises the alert.
func TestAWindowThatJoinsARunningConversationAlertsWhenItEnds(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{{
		Chunks: []string{"one ", "two ", "three ", "four"}, Delay: 300 * time.Millisecond,
	}}})
	sender := k.Dial(t)
	id := k.CreateConversation(t, sender)
	k.Submit(t, sender, id, "hello")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for {
		opened, err := sender.Open(ctx, kernel.OpenParams{ID: id, Window: &fakekernel.DefaultWindow})
		if err != nil {
			t.Fatal(err)
		}
		s := sessionview.NewState(id)
		s.Apply(kernel.ViewNotification{ID: id, Kind: kernel.ViewKindValue, Value: &opened.View})
		if s.Busy() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var notifications atomic.Int64
	a := New(Config{
		Prefs:         prefs.Memory(),
		KernelSession: func(i kernel.ID) (string, bool) { return "conv", i == id },
		Update:        func(fn func()) { fn() },
		Notify:        func(Alert) { notifications.Add(1) },
	})
	defer a.Close()
	a.UseKernel(k.Dial(t), 1)
	for a.sessions.Generation() != 1 {
		time.Sleep(time.Millisecond)
	}
	if err := a.sessions.Open(ctx, &a.kernel, id, fakekernel.DefaultWindow); err != nil {
		t.Fatal(err)
	}
	for notifications.Load() == 0 {
		if ctx.Err() != nil {
			t.Fatalf("the run ended without an alert (busy %v)", a.sessions.State(id).Busy())
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	state := a.busy["conv"]
	a.mu.Unlock()
	if state != "idle" {
		t.Errorf("the sidebar holds the finished conversation as %q", state)
	}
}
