package trayicon

import (
	"bytes"
	"image/png"
	"testing"
)

func TestEveryStateHasAnIcon(t *testing.T) {
	for _, state := range []string{"ok", "warning", "danger", "unknown", "expiring"} {
		img, err := png.Decode(bytes.NewReader(For(state)))
		if err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		if b := img.Bounds(); b.Dx() != 48 || b.Dy() != 48 {
			t.Fatalf("%s is %v, want 48x48", state, b)
		}
	}
	if !bytes.Equal(For("nonsense"), For("unknown")) {
		t.Fatal("an unknown state does not get the unknown icon")
	}
	if IsTemplate("expiring") || !IsTemplate("ok") {
		t.Fatal("only the expiring icon keeps its color")
	}
}
