package hostrun

import (
	"strings"
	"testing"

	"rsc.io/qr"
)

func TestTerminalQRDrawsEveryModuleOfTheCode(t *testing.T) {
	link := "http://192.168.1.5:41417/#pair=abcDEF123"
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(renderQR(link), "\n"), "\n")
	size := code.Size + 2*quietZone
	if want := (size + 1) / 2; len(lines) != want {
		t.Fatalf("%d lines, want %d", len(lines), want)
	}
	// Each cell is a character with the white background and black
	// foreground set, so it reads the same on a dark and a light terminal.
	black := func(x, y int) bool { return code.Black(x-quietZone, y-quietZone) }
	for row, line := range lines {
		cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, qrOn), qrOff), "")
		if len(cells) != size {
			t.Fatalf("line %d: %d cells, want %d", row, len(cells), size)
		}
		for x, cell := range cells {
			top, bottom := black(x, 2*row), black(x, 2*row+1)
			want := map[[2]bool]string{{false, false}: " ", {true, false}: "▀", {false, true}: "▄", {true, true}: "█"}[[2]bool{top, bottom}]
			if cell != want {
				t.Fatalf("line %d cell %d: %q, want %q", row, x, cell, want)
			}
		}
	}
}
