package app

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

func pngOf(w, h int) string {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// A wide screenshot attached to a message stays inside the transcript's
// column, as the web Client's max-w-full does.
func TestAWideAttachedImageFitsTheColumn(t *testing.T) {
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Shots", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Content: []map[string]any{
			{"type": "text", "text": "look"},
			{"type": "image", "source": map[string]any{"type": "base64", "mediaType": "image/png", "data": pngOf(3000, 300)}},
		}}}}}}
	h := newHarness(t, sc, "")
	h.openSession("Shots")
	h.until("the image", func() bool { _, ok := h.tt.Find("Attached image"); return ok })
	h.frame()
	img, _ := h.tt.Find("Attached image")
	list, _ := h.tt.Find("Transcript")
	if img.W <= 0 || img.X+img.W > list.X+list.W-24 || img.X < list.X {
		t.Fatalf("image %+v outside the transcript %+v", img, list)
	}
	if ratio := img.W / img.H; ratio < 9.5 || ratio > 10.5 {
		t.Fatalf("image %+v lost its 10:1 shape", img)
	}
}

// An image a reply names by its path on the computer, as Droid writes a
// screenshot it took, comes through the Daemon and shows in the transcript;
// one that is not there shows its path.
func TestAReplysImageByPathIsShown(t *testing.T) {
	sc := fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Shots", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
			{Role: "user", Text: "show me"},
			{Role: "assistant", Text: "Here it is:\n\n![The banner](/Users/dev/acme-web/my%20shot.png)\n\n![Old shot](/tmp/missing.png)"},
		}}},
		Input: map[string]any{"files": map[string]any{
			"/Users/dev/acme-web/my shot.png": map[string]any{"mimeType": "image/png", "base64": pngOf(800, 400)},
		}},
	}
	h := newHarness(t, sc, "")
	h.openSession("Shots")
	h.until("the image", func() bool { _, ok := h.tt.Find("The banner"); return ok })
	h.until("the missing image's path", func() bool { return h.hasText("/tmp/missing.png") })
	h.frame()
	img, _ := h.tt.Find("The banner")
	if ratio := img.W / img.H; img.W <= 0 || ratio < 1.9 || ratio > 2.1 {
		t.Fatalf("image %+v lost its 2:1 shape", img)
	}
	if h.hasText("[image]") {
		t.Fatalf("the reply still reads [image]: %q", h.tt.Texts())
	}
}

// Clicking a picture in the transcript shows it enlarged; a click closes it.
func TestClickingAPictureEnlargesIt(t *testing.T) {
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Shots", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Content: []map[string]any{
			{"type": "text", "text": "look"},
			{"type": "image", "source": map[string]any{"type": "base64", "mediaType": "image/png", "data": pngOf(800, 600)}},
		}}}}}}
	h := newHarness(t, sc, "")
	h.openSession("Shots")
	h.until("the image", func() bool { _, ok := h.tt.Find("Attached image"); return ok })
	h.frame()
	small, _ := h.tt.Find("Attached image")
	h.click("Attached image")
	v := h.a.view(h.a.Route().SessionID)
	if !v.zoomOpen {
		t.Fatal("the picture did not open")
	}
	h.frame()
	big, ok := h.tt.Find("Enlarged image")
	if !ok || big.W <= small.W*1.5 {
		t.Fatalf("enlarged %+v (found %v), thumbnail %+v", big, ok, small)
	}
	h.tt.ClickAt(big.X+big.W/2, big.Y+big.H/2)
	h.frame()
	if v.zoomOpen {
		t.Fatal("a click did not close the picture")
	}
}
