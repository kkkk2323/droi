package attachments

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ReadFile reads an image file as an attachment. One larger than
// MaxImageEdge or MaxImageBytes is re-encoded as a JPEG at quality 85 that
// fits MaxImageEdge; a small one goes as it is. ok is false for a file that
// is not an image.
func ReadFile(path, id string) (img Image, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Image{}, false, err
	}
	mediaType := http.DetectContentType(data)
	if !IsImageMediaType(mediaType) {
		return Image{}, false, nil
	}
	name := filepath.Base(path)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, false, nil
	}
	w, h := FitWithin(cfg.Width, cfg.Height, MaxImageEdge)
	if w == cfg.Width && h == cfg.Height && len(data) <= MaxImageBytes {
		return Image{ID: id, Name: name, MediaType: mediaType, Data: base64.StdEncoding.EncodeToString(data)}, true, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, false, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// JPEG has no alpha: transparent pixels go on white, as a canvas does.
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return Image{}, false, err
	}
	return Image{ID: id, Name: name, MediaType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(out.Bytes())}, true, nil
}
