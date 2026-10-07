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
