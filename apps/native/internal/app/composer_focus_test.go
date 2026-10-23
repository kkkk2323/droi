package app

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// What shows up before the text area (an attachment, an error, the shelf)
// must not take the keyboard focus from it: typing goes on without a click.

func pngBytes() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	return b.Bytes()
}

func (h *harness) typeInto(text string) {
	h.t.Helper()
	h.tt.Type(text)
	h.frame()
}

func (h *harness) sessionText() string {
	return h.a.views[h.a.Route().SessionID].composer.text
}

func (h *harness) focusComposer() {
	h.t.Helper()
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	h.click("Message")
}

func TestTypingGoesOnAfterPastingAnImage(t *testing.T) {
	clip := pngBytes()
	h := newHarnessWith(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}}}, "", func(cfg *Config) {
		cfg.ReadImage = func() []byte { return clip }
	})
	h.openSession("One")
	h.focusComposer()
	h.typeInto("see ")
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if _, ok := h.tt.Find("Pasted image.png"); !ok {
		t.Fatal("the pasted image is not shown")
	}
	h.typeInto("this")
	if got := h.sessionText(); got != "see this" {
		t.Fatalf("text after the paste: %q", got)
	}
}

func TestTypingGoesOnAfterAPasteFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.png")
	h := newHarnessWith(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}}}, "", func(cfg *Config) {
		cfg.ReadFiles = func() []string { return []string{missing} }
	})
	h.openSession("One")
	h.focusComposer()
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if h.a.views[h.a.Route().SessionID].Err() == "" {
		t.Fatal("the failed paste shows no error")
	}
	h.typeInto("still here")
	if got := h.sessionText(); got != "still here" {
		t.Fatalf("text after the error: %q", got)
	}
}

func TestTypingGoesOnAfterQueueingAMessage(t *testing.T) {
	h := newHarness(t, deployScenario(slowTurn()), "")
	h.openSession("Deploy")
	h.send("Deploy it")
	h.until("the turn to run", func() bool { _, ok := h.tt.Find("Cancel"); return ok })
	h.click("Message")
	h.typeInto("later")
	h.tt.Key(0, ui.KeyEnter)
	h.until("the queued message", func() bool { _, ok := h.tt.Find("later"); return ok })
	h.typeInto("next")
	if got := h.sessionText(); got != "next" {
		t.Fatalf("text after queueing: %q", got)
	}
}

func TestTypingGoesOnAfterPastingAnImageOnTheNewSessionPage(t *testing.T) {
	clip := pngBytes()
	h := newHarnessWith(t, sessionsScenario(), "", func(cfg *Config) {
		cfg.ReadImage = func() []byte { return clip }
	})
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.typeInto("see ")
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	h.typeInto("this")
	if got := h.a.newPage.text; got != "see this" {
		t.Fatalf("text after the paste: %q", got)
	}
}

func TestTypingGoesOnAfterAPermissionIsAnswered(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "permission", Command: "rm -rf build", Reply: "Build directory removed."}), "")
	h.openSession("Deploy")
	h.send("Clean the build dir")
	h.until("the permission card", func() bool { return h.hasText("rm -rf build") })
	h.click("Yes, allow")
	h.until("the composer back", func() bool { _, ok := h.tt.Find("Message"); return ok })
	h.typeInto("next")
	if got := h.sessionText(); got != "next" {
		t.Fatalf("text after the permission: %q", got)
	}
}

func TestOpeningASessionPutsTheFocusInTheComposer(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Refactor invoice generator")
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	if !h.tt.Focused("Message") {
		t.Fatal("the composer does not have the focus")
	}
}

// The composer takes the focus when it appears, a moment after its Session
// opens; by then a click may have put the focus in the sidebar's search.
func TestTheComposerLeavesTheFocusInTheSidebarsSearch(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Refactor invoice generator") })
	id := h.d.Sessions[0].SessionID
	h.click("Search sessions and workspaces")
	h.a.Go(Route{Name: "session", SessionID: id})
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	h.typeInto("zzzz")
	if h.a.sidebar.query != "zzzz" || !h.tt.Focused("Search sessions and workspaces") {
		t.Fatalf("the search has %q and the composer %q", h.a.sidebar.query, h.a.views[id].composer.field)
	}
}
