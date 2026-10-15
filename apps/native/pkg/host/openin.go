package host

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// OpenInApp is an installed app that can open a folder, for the Session
// header's "open in" control.
type OpenInApp struct {
	ID    string
	Label string
	// AppPath is the .app bundle, handed to `open -a`; on Windows the
	// program, run with Args and the folder.
	AppPath string
	Args    []string
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

// windowsOpenInCatalog is the catalog on Windows, with each program's usual
// places; "~" stands for %LOCALAPPDATA% (the user's AppData\Local).
var windowsOpenInCatalog = []struct {
	id, label string
	programs  []string // the first one found wins
	args      []string // before the folder
}{
	{"vscode", "VS Code", []string{`~\Programs\Microsoft VS Code\Code.exe`, `C:\Program Files\Microsoft VS Code\Code.exe`}, nil},
	{"cursor", "Cursor", []string{`~\Programs\cursor\Cursor.exe`}, nil},
	{"zed", "Zed", []string{`~\Programs\Zed\Zed.exe`}, nil},
	{"explorer", "File Explorer", []string{`C:\Windows\explorer.exe`}, nil},
	{"windows-terminal", "Windows Terminal", []string{`~\Microsoft\WindowsApps\wt.exe`}, []string{"-d"}},
}

// LocateOpenInApps lists the catalog's apps installed in the usual
// folders, in catalog order; on macOS and Windows, elsewhere none.
func LocateOpenInApps(goos, home string, exists func(string) bool) []OpenInApp {
	if goos == "windows" {
		var found []OpenInApp
		for _, e := range windowsOpenInCatalog {
			for _, p := range e.programs {
				if rest, ok := strings.CutPrefix(p, "~"); ok {
					p = home + `\AppData\Local` + rest
				}
				if exists(p) {
					found = append(found, OpenInApp{ID: e.id, Label: e.label, AppPath: p, Args: e.args})
					break
				}
			}
		}
		return found
	}
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
		if app.ID != appID {
			continue
		}
		if runtime.GOOS != "windows" {
			return exec.Command("open", "-a", app.AppPath, path).Run()
		}
		// Not Run: a GUI program keeps running, and Explorer exits 1 even when it opened the folder.
		cmd := exec.Command(app.AppPath, append(append([]string{}, app.Args...), path)...)
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return fmt.Errorf("unknown app: %s", appID)
}
