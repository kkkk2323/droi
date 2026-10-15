package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// macOS keeps the files in another app's container from Droi: a picture
// dragged out of WeChat comes as such a path, which Droi cannot open.
func TestAFileInAnotherAppsDataSaysHowToAddIt(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getuid() == 0 {
		t.Skip("needs macOS and a user the file permissions apply to")
	}
	dir := filepath.Join(t.TempDir(), "Library", "Containers", "com.tencent.xinWeChat", "Data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	picture := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(picture, pngBytes(), 0o000); err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}}}, "", func(cfg *Config) {
		cfg.ReadFiles = func() []string { return []string{picture} }
	})
	h.openSession("One")
	h.focusComposer()
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	got := h.a.views[h.a.Route().SessionID].Err()
	for _, want := range []string{"photo.png", "another app", "Downloads", "Full Disk Access"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the error does not say %q: %q", want, got)
		}
	}
}
