package app

import (
	"sync"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
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
	h := newHarness(t, sc, "")
	var mu sync.Mutex
	var sounds []string
	var notes []Alert
	h.a.cfg.PlaySound = func(s string) { mu.Lock(); sounds = append(sounds, s); mu.Unlock() }
	h.a.cfg.Notify = func(n Alert) { mu.Lock(); notes = append(notes, n); mu.Unlock() }
	h.a.cfg.Focused = func() bool { return true }

	h.openSession("Deploy")
	h.send("Go")
	deploy := h.d.Sessions[0].SessionID
	h.click("Notes")
	h.until("the completion alert", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(notes) > 0
	})
	mu.Lock()
	if len(sounds) != 1 || sounds[0] != "fx-ok01" {
		t.Errorf("sounds %q", sounds)
	}
	if n := notes[0]; n.SessionID != deploy || n.Title != "Droid finished" || n.Body != "Deploy" {
		t.Errorf("notification %+v", n)
	}
	mu.Unlock()
	if !h.a.unread[deploy] {
		t.Fatal("Deploy is not unread")
	}
	if _, ok := h.tt.Find("Unread"); !ok {
		t.Error("no unread mark in the sidebar")
	}
	// The list reorders as the Sessions' times change; a click before it
	// settles can land on another row.
	h.settle()
	h.click("Deploy")
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
