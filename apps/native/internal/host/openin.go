package host

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenInApp is an installed app that can open a folder, for the Session
// header's "open in" control.
type OpenInApp struct {
	ID    string
	Label string
	// AppPath is the .app bundle, handed to `open -a`.
	AppPath string
}

// The catalog and its order follow Waku's: editors, the file manager,
// terminals, IDEs.
var openInCatalog = []struct {
	id, label string
	bundles   []string // the first one found wins
}{
	{"vscode", "VS Code", []string{"Visual Studio Code.app"}},
	{"cursor", "Cursor", []string{"Cursor.app"}},
	{"zed", "Zed", []string{"Zed.app", "Zed Preview.app"}},
	{"finder", "Finder", []string{"Finder.app"}},
	{"terminal", "Terminal", []string{"Terminal.app"}},
	{"iterm2", "iTerm2", []string{"iTerm.app"}},
	{"kitty", "Kitty", []string{"kitty.app"}},
	{"ghostty", "Ghostty", []string{"Ghostty.app"}},
	{"warp", "Warp", []string{"Warp.app"}},
	{"xcode", "Xcode", []string{"Xcode.app"}},
	{"android-studio", "Android Studio", []string{"Android Studio.app"}},
}

// LocateOpenInApps lists the catalog's apps installed in the usual
// folders, in catalog order; macOS only, elsewhere none.
func LocateOpenInApps(goos, home string, exists func(string) bool) []OpenInApp {
	if goos != "darwin" {
		return nil
	}
	folders := []string{
		"/Applications",
		filepath.Join(home, "Applications"),
		"/System/Applications",
		"/System/Applications/Utilities",
		"/System/Library/CoreServices",
	}
	var found []OpenInApp
	for _, e := range openInCatalog {
	search:
		for _, b := range e.bundles {
			for _, f := range folders {
				if p := filepath.Join(f, b); exists(p) {
					found = append(found, OpenInApp{ID: e.id, Label: e.label, AppPath: p})
					break search
				}
			}
		}
	}
	return found
}

// OpenIn opens a directory in an app of apps.
func OpenIn(apps []OpenInApp, path, appID string) error {
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	for _, app := range apps {
		if app.ID == appID {
			return exec.Command("open", "-a", app.AppPath, path).Run()
		}
	}
	return fmt.Errorf("unknown app: %s", appID)
}

// AppIconPNG is a bundle's icon as a PNG of size px, read with sips from
// the icon file its Info.plist names; nil when it has none.
func AppIconPNG(appPath string, size int) []byte {
	out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleIconFile", "raw", filepath.Join(appPath, "Contents", "Info.plist")).Output()
	if err != nil {
		return nil
	}
	name := strings.TrimSpace(string(out))
	if filepath.Ext(name) == "" {
		name += ".icns"
	}
	icns := filepath.Join(appPath, "Contents", "Resources", name)
	dir, err := os.MkdirTemp("", "droi-icon")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	png := filepath.Join(dir, "icon.png")
	var stderr bytes.Buffer
	cmd := exec.Command("/usr/bin/sips", "-s", "format", "png", "-Z", fmt.Sprint(size), icns, "--out", png)
	cmd.Stderr = &stderr
	if cmd.Run() != nil {
		return nil
	}
	data, _ := os.ReadFile(png)
	return data
}
