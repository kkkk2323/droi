package textsize

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

func TestGetReadsOnlyKnownSizes(t *testing.T) {
	s := prefs.Memory()
	if Get(s) != Default {
		t.Error("nothing stored is the default")
	}
	s.Set("droi.textSize", "huge")
	if Get(s) != Default {
		t.Error("an unknown size reads as the default")
	}
	Set(s, Large)
	if v, _ := s.Get("droi.textSize"); v != "large" || Get(s) != Large {
		t.Errorf("stored %q", v)
	}
}
