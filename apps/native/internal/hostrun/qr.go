package hostrun

import (
	"strings"

	"rsc.io/qr"
)

const (
	quietZone = 2
	// White background, black foreground: the code keeps its polarity on a
	// dark terminal, where a scanner would otherwise see it inverted.
	qrOn  = "\x1b[47;30m"
	qrOff = "\x1b[0m"
)

// renderQR draws link as a QR code for a terminal, two modules to a
// character by half blocks. "" when it does not fit a code.
func renderQR(link string) string {
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		return ""
	}
	size := code.Size + 2*quietZone
	black := func(x, y int) bool { return code.Black(x-quietZone, y-quietZone) }
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		b.WriteString(qrOn)
		for x := range size {
			switch top, bottom := black(x, y), black(x, y+1); {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString(qrOff + "\n")
	}
	return b.String()
}
