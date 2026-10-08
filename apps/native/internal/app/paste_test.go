package app

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

func TestPastingAnImageAttachesIt(t *testing.T) {
	var clip []byte
	h := newHarnessWith(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}}}, "", func(cfg *Config) {
		cfg.ReadImage = func() []byte { return clip }
	})
	h.openSession("One")
	// Text on the clipboard pastes as text, once the Session is loaded.
	h.tt.SetClipboard("plain words")
	h.until("a text paste", func() bool {
		if _, ok := h.tt.Find("Message"); !ok {
			return false
		}
		_ = h.tt.Click("Message")
		h.frame()
		h.tt.Command("paste")
		h.frame()
		return h.a.views[h.a.Route().SessionID].composer.text == "plain words"
	})
	if v := h.a.views[h.a.Route().SessionID]; len(v.composer.images) != 0 {
		t.Fatalf("a text paste attached %d images", len(v.composer.images))
	}

	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	clip = b.Bytes()
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	v := h.a.views[h.a.Route().SessionID]
	if len(v.composer.images) != 1 || v.composer.images[0].MediaType != "image/png" {
		t.Fatalf("image paste: %+v", v.composer.images)
	}
	if v.composer.text != "plain words" {
		t.Fatalf("an image paste changed the text: %q", v.composer.text)
	}
}

// Paste of the Edit menu (⌘V in the app, as the menu takes the key) asks
// the focused composer for the clipboard's image before pasting text.
func TestPasteOfTheEditMenuAttachesAnImage(t *testing.T) {
	var clip []byte
	h := newHarnessWith(t, sessionsScenario(), "", func(cfg *Config) {
		cfg.ReadImage = func() []byte { return clip }
	})
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	if h.a.PasteImage() {
		t.Fatal("no image on the clipboard, yet the paste took one")
	}
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	clip = b.Bytes()
	if !h.a.PasteImage() {
		t.Fatal("the New session page's composer did not take the image")
	}
	h.frame()
	if _, ok := h.tt.Find("Pasted image.png"); !ok {
		t.Fatal("the pasted image is not shown")
	}

	h.openSession("Fix the login race")
	h.until("the composer", func() bool {
		if _, ok := h.tt.Find("Message composer"); !ok {
			return false
		}
		_ = h.tt.Click("Message")
		h.frame()
		return h.a.PasteImage()
	})
	if v := h.a.views[h.a.Route().SessionID]; len(v.composer.images) != 1 {
		t.Fatalf("the Session's composer has %d images", len(v.composer.images))
	}
	_ = h.tt.Click("Search sessions")
	h.frame()
	if h.a.PasteImage() {
		t.Fatal("the image went to a composer without the focus")
	}
}

func TestAnImagePastedOnTheNewSessionPageShows(t *testing.T) {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	h := newHarnessWith(t, sessionsScenario(), "", func(cfg *Config) {
		cfg.ReadImage = func() []byte { return b.Bytes() }
	})
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if len(h.a.newPage.images) != 1 {
		t.Fatalf("the paste attached %d images", len(h.a.newPage.images))
	}
	if _, ok := h.tt.Find("Pasted image.png"); !ok {
		t.Fatal("the pasted image is not shown")
	}
	h.click("Remove Pasted image.png")
	if len(h.a.newPage.images) != 0 {
		t.Fatal("removing the image kept it")
	}
}
