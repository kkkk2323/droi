package app

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
)

// newPhoneHarness is the window as the Phone App draws it, following a
// Paired Computer, on an iPhone's screen.
func newPhoneHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarnessWith(t, sessionsScenario(), "", func(c *Config) {
		c.Phone = true
		c.InsetTop = false
		c.Computer = &computer.Paired{Gateway: "http://127.0.0.1:1", Token: "pairing-token", Name: "Mac", ID: "mac"}
	})
	h.tt.SetSize(402, 874)
	h.frame()
	return h
}

func TestAPhoneOpensOnTheSessionListAndPushesASession(t *testing.T) {
	h := newPhoneHarness(t)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if r := h.a.Route(); r.Name != "home" {
		t.Fatalf("the phone opened on %+v, not the Session list", r)
	}
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("regression test") })
	if h.hasText("Add dark mode toggle") {
		t.Error("the Session list still shows beside an open Session")
	}
	if h.tt.Focused("Message") {
		t.Error("the composer took the focus, which brings up a phone's keyboard over the transcript")
	}
	h.click("Sessions")
	h.until("the Session list again", func() bool { return h.a.Route().Name == "home" && h.hasText("Add dark mode toggle") })
}

func TestAFingerFromThePhonesLeftEdgeGoesBack(t *testing.T) {
	h := newPhoneHarness(t)
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("regression test") })
	h.tt.FingerDown(4, 400)
	for x := float32(20); x <= 300; x += 20 {
		h.tt.FingerMove(x, 400)
		h.frame()
	}
	h.tt.FingerUp(300, 400)
	h.frame()
	if r := h.a.Route(); r.Name != "home" {
		t.Fatalf("after the swipe the phone shows %+v", r)
	}
}

func TestAPhonesSettingsShowEachSectionAsAPage(t *testing.T) {
	h := newPhoneHarness(t)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Settings")
	h.until("the sections", func() bool { return h.hasText("Session defaults") })
	if h.hasText("Language") {
		t.Error("a section's settings show beside the list of sections")
	}
	if h.hasText("This computer") {
		t.Error("a phone offers to use itself as the computer")
	}
	h.click("General")
	h.until("General's settings", func() bool { return h.hasText("Language") })
	h.click("Back")
	h.until("the sections again", func() bool { return h.hasText("Session defaults") && !h.hasText("Language") })
	h.click("Computer")
	h.until("the Computer section", func() bool { return h.hasText("Another computer") })
	if h.hasText("This computer") {
		t.Error("a phone offers to use itself as the computer")
	}
}
