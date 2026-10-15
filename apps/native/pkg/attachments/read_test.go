package attachments

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/tiff"
)

func writePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFileKeepsASmallImage(t *testing.T) {
	p := writePNG(t, 40, 30)
	img, ok, err := ReadFile(p, "a")
	if err != nil || !ok {
		t.Fatalf("ok %v err %v", ok, err)
	}
	if img.MediaType != "image/png" || img.Name != "shot.png" || img.ID != "a" {
		t.Fatalf("%+v", img)
	}
}

func TestReadFileShrinksALargeImageToAJPEG(t *testing.T) {
	p := writePNG(t, 3000, 1000)
	img, ok, err := ReadFile(p, "b")
	if err != nil || !ok {
		t.Fatalf("ok %v err %v", ok, err)
	}
	if img.MediaType != "image/jpeg" {
		t.Fatalf("media type %s", img.MediaType)
	}
	data, _ := base64.StdEncoding.DecodeString(img.Data)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != MaxImageEdge || cfg.Height != 523 {
		t.Fatalf("%dx%d %v", cfg.Width, cfg.Height, err)
	}
}

func TestReadFileSkipsAFileThatIsNotAnImage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o644)
	if _, ok, err := ReadFile(p, "c"); ok || err != nil {
		t.Fatalf("ok %v err %v", ok, err)
	}
}

// A TIFF, as Safari drags its pictures, goes as a PNG.
func TestFromBytesTakesATIFF(t *testing.T) {
	var b bytes.Buffer
	if err := tiff.Encode(&b, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	img, ok, err := FromBytes(b.Bytes(), "Dropped image.png", "id")
	if err != nil || !ok || img.MediaType != "image/png" {
		t.Fatalf("%v %v %q", err, ok, img.MediaType)
	}
}
