package brand

import (
	"path/filepath"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

func TestSetFillsInDroisAndNamesTheTexts(t *testing.T) {
	defer Set(Droi)
	if b := Current(); b.Name != "Droi" || b.ID != "droi" || b.Port != 41417 {
		t.Fatalf("default: %+v", b)
	}
	Set(Brand{Name: "Acme Agent", ID: "acme"})
	if b := Current(); b.Name != "Acme Agent" || b.ID != "acme" || b.Port != Droi.Port {
		t.Errorf("set: %+v", b)
	}
	if got := ScratchFolder("/home/u"); got != filepath.Join("/home/u", ".acme", "chats") {
		t.Errorf("scratch folder: %q", got)
	}
	if got := l10n.L("Quit Droi"); got != "Quit Acme Agent" {
		t.Errorf("texts: %q", got)
	}
	if got := ShortName(); got != "Acme Agent" {
		t.Errorf("short name without Short: %q", got)
	}
	Set(Brand{Name: "Acme Agent", Short: "Acme"})
	if got, name := ShortName(), Name(); got != "Acme" || name != "Acme Agent" {
		t.Errorf("short name %q, name %q", got, name)
	}
}
