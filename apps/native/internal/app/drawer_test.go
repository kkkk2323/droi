package app

import (
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

func TestANarrowWindowPutsTheSidebarInADrawer(t *testing.T) {
	h := newHarness(t, oneSession(), "")
	h.tt.SetSize(700, 700)
	h.settle()
	if h.hasText("One") {
		t.Fatal("the sidebar stands beside the page in a narrow window")
	}
	h.click("Open sessions")
	h.settle()
	if !h.a.drawerOpen || !h.hasText("One") {
		t.Fatalf("the drawer did not open: %q", h.tt.Texts())
	}
	// The drawer is the sidebar's colour from the left edge to 288, the
	// dimmed page past it.
	img := h.tt.Image()
	sx := img.Bounds().Dx() / 700
	side, page := img.RGBAAt(100*sx, 600*sx), img.RGBAAt(500*sx, 600*sx)
	if side == page || page.R > 200 {
		t.Errorf("no drawer over a dimmed page: sidebar %v, page %v", side, page)
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		f, _ := os.Create(dir + "/drawer.png")
		_ = png.Encode(f, img)
		f.Close()
	}
	h.click("One")
	h.settle()
	if r := h.a.Route(); r.Name != "session" {
		t.Fatalf("picking a Session went to %+v", r)
	}
	if h.a.drawerOpen {
		t.Error("the drawer stayed open after a pick")
	}
	h.click("Open sessions")
	h.settle()
	h.tt.SetSize(refW, refH)
	h.settle()
	if h.a.drawerOpen {
		t.Error("widening the window kept the drawer")
	}
	if _, ok := h.tt.Find("Open sessions"); ok {
		t.Error("a wide window has the drawer's button")
	}
}

func TestZoomingInNarrowsThePage(t *testing.T) {
	h := newHarness(t, oneSession(), "")
	h.settle()
	if _, ok := h.tt.Find("Open sessions"); ok {
		t.Fatal("narrow at 100%")
	}
	for i := 0; i < 3; i++ {
		h.a.Zoom(1)
	}
	h.settle()
	if _, ok := h.tt.Find("Open sessions"); !ok {
		t.Error("zoomed in past the breakpoint, the sidebar still stands beside the page")
	}
}

// Long drafts and attachments must not strand the New session actions below
// a short window, including when the user chooses the largest text size.
func TestShortWindowKeepsNewSessionActionsReachable(t *testing.T) {
	for _, size := range []string{"default", "largest"} {
		t.Run(size, func(t *testing.T) {
			h := newHarnessWith(t, draftScenario(), "", func(cfg *Config) {
				cfg.ReadImage = pngBytes
			})
			h.tt.SetSize(1280, 480)
			prefs.TextSize.Set(h.a.prefs, size)
			h.a.applyPrefs()
			h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
			h.click("New session")
			h.click("Message")
			h.tt.Command("paste")
			h.frame()
			h.tt.Type(strings.Repeat("A line in the draft\n", 7) + "Last line")
			h.frame()
			h.tt.Scroll(900, 300, 0, 1200)
			h.settle()
			button, ok := h.tt.Find("Start session")
			if !ok || button.Y < 44 || button.Y+button.H > 480 {
				t.Fatalf("Start session is unreachable after scrolling: %+v (found %v)", button, ok)
			}
			if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
				writePNG(dir+"/new-session-short-"+size+".png", h.tt.Image())
			}
			h.click("Start session")
			h.until("the sent Session", func() bool { return h.a.Route().Name == "session" })
			h.until("the reply", func() bool { return h.hasText("On it.") })
		})
	}
}
