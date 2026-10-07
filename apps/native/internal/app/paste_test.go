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
