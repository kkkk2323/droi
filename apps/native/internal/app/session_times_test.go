package app

import (
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// timesScenario is two Sessions of one Workspace and one of another, the
// Daemon dating "Fix the login race" the newest.
func timesScenario() fakedaemon.Scenario {
	now := time.Now().Unix()
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Fix it"}},
				Extra: map[string]any{"sessionId": "s-fix", "updatedAt": now - 60}},
			{Title: "Add dark mode toggle", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Add dark mode"}},
				Extra: map[string]any{"sessionId": "s-dark", "updatedAt": now - 2*60*60}},
			{Title: "Refactor invoice generator", Cwd: "/Users/dev/billing-service", Messages: []fakedaemon.Message{{Role: "user", Text: "Refactor"}},
				Extra: map[string]any{"sessionId": "s-bill", "updatedAt": now - 3*24*60*60 - 60}},
		},
		Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"On it."}},
	}
}

// A message sent to a Session, from this window or a phone, moves it up:
// the list dates a Session by the last message sent to it, which the
// Gateway notes, not by the Daemon's date, which opening it can move.
func TestAMessageSentMovesTheSessionUp(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.settle()
	if !h.above("Fix the login race", "Add dark mode toggle") {
		t.Fatal("the Daemon's newest is not first before anything was sent")
	}
	h.openRow("Add dark mode toggle", "s-dark")
	h.send("Make it blue")
	h.until("the reply", func() bool { return h.hasText("On it.") })
	if h.times.Snapshot().Sessions["s-dark"].SentAt == 0 {
		t.Fatal("the Gateway did not note the message")
	}
	h.settle()
	if !h.above("Add dark mode toggle", "Fix the login race") {
		t.Fatal("the Session sent a message did not move up")
	}
	h.openRow("Fix the login race", "s-fix")
	h.settle()
	if !h.above("Add dark mode toggle", "Fix the login race") {
		t.Fatal("opening a Session moved it up")
	}

	// A phone's message, which only the Gateway sees.
	h.times.Sent("s-fix")
	h.settle()
	h.until("the phone's message", func() bool { return h.above("Fix the login race", "Add dark mode toggle") })
}

// A result nobody has seen floats up and is marked on every Client, and
// reads once any Client shows it.
func TestAnUnseenResultIsSharedByEveryClient(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	h.until("the list", func() bool { return h.hasText("billing-service") })
	h.openRow("Add dark mode toggle", "s-dark")
	// A Loop's turn in another Session ends while this one is open.
	h.times.Working("s-fix")
	h.times.Ended("s-fix")
	h.settle()
	h.until("the unread mark", func() bool {
		_, ok := h.tt.Find("Unread")
		return ok && h.a.sidebar.unread["s-fix"]
	})
	// A phone shows it.
	h.times.Viewed("s-fix")
	h.until("the mark gone", func() bool { _, ok := h.tt.Find("Unread"); return !ok })

	// A result in a Session older than the rows shown leaves "Show 1 older".
	if h.hasText("Refactor invoice generator") {
		t.Fatal("an old Session shows before it has news")
	}
	h.times.Working("s-bill")
	h.times.Ended("s-bill")
	h.settle()
	h.until("the old Session's result shown", func() bool { return h.hasText("Refactor invoice generator") })
	h.openRow("Refactor invoice generator", "s-bill")
	h.until("read on every Client", func() bool { return !h.times.Snapshot().Sessions["s-bill"].Unread() })
}

// The Session on screen is never marked unread, though the end of its turn
// can reach the Session Times before this window notes it seen.
func TestTheSessionOnScreenIsNeverUnread(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.openRow("Add dark mode toggle", "s-dark")
	h.times.Working("s-dark")
	h.times.Ended("s-dark")
	h.until("the end in the window's times", func() bool {
		h.a.mu.Lock()
		defer h.a.mu.Unlock()
		return h.a.times.times["s-dark"].Unread()
	})
	h.settle()
	if _, ok := h.tt.Find("Unread"); ok || h.a.sidebar.unread["s-dark"] {
		t.Fatal("the Session on screen is marked unread")
	}
	// This window heard nothing of the end, so the mark shows once the
	// Session is no longer on screen.
	h.openRow("Fix the login race", "s-fix")
	h.until("the mark off screen", func() bool { return h.a.sidebar.unread["s-dark"] })
}

// While the pointer is over the list, no row moves under it; it settles
// once the pointer leaves.
func TestTheRowsHoldStillUnderThePointer(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.settle()
	r, ok := h.tt.Find("Fix the login race")
	if !ok {
		t.Fatal("no row")
	}
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	h.frame()
	h.frame()
	h.times.Sent("s-dark")
	for range 20 {
		h.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if !h.above("Fix the login race", "Add dark mode toggle") {
		t.Fatal("a row moved under the pointer")
	}
	h.tt.Move(refW-1, refH-1)
	h.until("the list settled", func() bool { return h.above("Add dark mode toggle", "Fix the login race") })
}

// By time, the sidebar is one list by the day each Session was last used,
// each row naming its Workspace; the Sort menu switches the view.
func TestTheTimeViewListsSessionsByDay(t *testing.T) {
	h := newHarness(t, timesScenario(), "")
	h.until("the list", func() bool { return h.hasText("Add dark mode toggle") })
	h.click("Sort")
	if err := h.tt.ChooseMenuItem("By time"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	if prefs.SessionView.Get(h.a.prefs) != "time" {
		t.Fatal("the view was not kept")
	}
	h.settle()
	h.until("the sections", func() bool { return h.hasText("Today") && h.hasText("Earlier") })
	if !h.above("Today", "Fix the login race") || !h.above("Add dark mode toggle", "Earlier") || !h.above("Earlier", "Refactor invoice generator") {
		t.Fatalf("sections out of order: %q", h.tt.Texts())
	}
	if _, ok := h.tt.Find("In billing-service"); !ok {
		t.Fatalf("a row does not name its Workspace: %q", h.tt.Texts())
	}
	h.times.Sent("s-bill")
	h.until("the old Session in Today", func() bool {
		return !h.hasText("Earlier") && h.above("Refactor invoice generator", "Fix the login race")
	})
	h.click("Sort")
	if err := h.tt.ChooseMenuItem("By workspace"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if h.hasText("Earlier") || prefs.SessionView.Get(h.a.prefs) != "" {
		t.Fatal("the Workspace view did not come back")
	}
}
