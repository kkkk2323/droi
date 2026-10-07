package app

import (
	"image"
	"image/color"

	"github.com/egoist/mygo/ui"
	"rsc.io/qr"
)

// pairingQR is the pairing link as a QR code, a one-module margin as the
// web Client draws it (the qrcode package's margin: 1, level M), each
// module a block of scale pixels so it stays sharp.
func pairingQR(link string, scale int) *ui.Bitmap {
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		return nil
	}
	n := (code.Size + 2) * scale
	img := image.NewGray(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			v := uint8(255)
			if code.Black(x/scale-1, y/scale-1) {
				v = 0
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	return ui.NewBitmap(img)
}
