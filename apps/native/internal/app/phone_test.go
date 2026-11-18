package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
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

// The composer's bar fits a phone's width in one row, in icons: in words it
// wrapped to three.
func TestAPhonesComposerBarIsOneRow(t *testing.T) {
	h := newPhoneHarness(t)
	h.openSession("Fix the login race")
	h.until("the composer", func() bool { _, ok := h.tt.Find("Send"); return ok })
	send, _ := h.tt.Find("Send")
	// Compact, the autonomy button's name tells its level.
	autonomy := "Autonomy"
	for _, l := range autonomyLevels {
		if name := "Autonomy: " + l10n.T(models.AutonomyLabels[l]); h.hasText(name) {
			autonomy = name
		}
	}
	for _, label := range []string{"Add image", "Spec mode", autonomy, "Skills and MCP servers"} {
		r, ok := h.tt.Find(label)
		if mid := r.Y + r.H/2; !ok || mid < send.Y || mid > send.Y+send.H {
			t.Errorf("%s is not in the Send button's row: at y %v, Send at %v", label, r.Y, send.Y)
		}
	}
}

// The bands past a phone's safe area take the colour of the page above them:
// the Session list's on it, the page's on a Session.
func TestAPhonesSafeAreaBandsTakeThePagesColour(t *testing.T) {
	h := newPhoneHarness(t)
	h.tt.SetInsets(47, 0, 34, 0)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	band := func() ui.Color {
		h.frame()
		img := h.tt.Image()
		b := img.Bounds()
		c := img.RGBAAt(b.Dx()/2, b.Max.Y-2)
		return ui.Color{R: c.R, G: c.G, B: c.B, A: c.A}
	}
	same := func(a, b ui.Color) bool { return a.R == b.R && a.G == b.G && a.B == b.B }
	if got, want := band(), h.a.kit.T.Sidebar; !same(got, want) {
		t.Errorf("under the Session list the band is %v, not the list's %v", got, want)
	}
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("regression test") })
	if got, want := band(), h.a.kit.T.Background; !same(got, want) {
		t.Errorf("under a Session the band is %v, not the page's %v", got, want)
	}
}

// A phone's model picker is a sheet from the bottom: its search does not take
// the focus, which would bring up the keyboard over it, and its reasoning
// effort chips all show, where the popover's row ran off the screen.
func TestAPhonesModelPickerIsASheet(t *testing.T) {
	h := newPhoneHarness(t)
	h.openSession("Fix the login race")
	h.until("the composer", func() bool { _, ok := h.tt.Find("Send"); return ok })
	h.click("Model and reasoning effort")
	h.until("the sheet", func() bool { _, ok := h.tt.Find("Choose a model"); return ok })
	for i := 0; i < 30; i++ {
		h.tt.Advance(16 * time.Millisecond)
	}
	if h.tt.Focused("Search models") {
		t.Error("the search took the focus, which brings up the keyboard")
	}
	sheet, _ := h.tt.Find("Choose a model")
	if sheet.X < 0 || sheet.X+sheet.W > 402.5 {
		t.Errorf("the sheet runs off the screen: %+v", sheet)
	}
	efforts, ok := h.tt.Find("Reasoning effort")
	if !ok || efforts.Y+efforts.H > 874 || efforts.X+efforts.W > 402.5 {
		t.Errorf("the reasoning effort is not on screen: %+v", efforts)
	}
}
