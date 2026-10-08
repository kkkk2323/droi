// Command native is Droi as a native app: the Host starts the Daemon and
// supplies the credential, and one MyGo window draws the Client (package
// app) and talks to the Daemon directly through the Go SDK.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/app"
	"github.com/kkkk2323/droi/apps/native/internal/highlight"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/theme"
	"github.com/kkkk2323/droi/apps/native/internal/updates"
)

// devVersion is the version of `go run`; a build takes mygo.json's.
const devVersion = "1.38.0"

func main() {
	// The Runtime Overlay runs the app's own executable as the Memory Server
	// and the hook; neither starts the app.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case host.MemoryServerCommand:
			os.Exit(memory.ServeMain())
		case host.MemoryHookCommand:
			os.Exit(memory.HookMain())
		}
	}
	version := mygo.App.Version()
	if version == "" {
		version = devVersion
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	// Tests point the app at a throwaway profile so they never touch real settings.
	userData := os.Getenv("DROI_USER_DATA_DIR")
	if userData == "" {
		if userData, err = mygo.App.Path(mygo.PathUserData); err != nil {
			log.Fatal(err)
		}
	}

	h := host.New(host.Config{
		UserData:    userData,
		Home:        home,
		Version:     version,
		MoveToTrash: mygo.Shell.TrashItem,
		Env:         os.Getenv,
		Executable:  executable(),
	})
	ctl := controller.New(controller.Config{
		ResolveURL: h.WaitForDaemonURL,
		Credential: h.Credential,
		Caller:     "droi-native",
		// The Daemon restarts on its own (the Host's Supervisor); keep trying.
		MaxReconnectAttempts: 1000,
		DefaultMessageLimit:  app.LoadedMessageLimit,
	})

	// A build signed for updates can replace itself; `go run` cannot.
	var updater *updates.Updater
	if mygo.Updater.Enabled() && os.Getenv("DROI_NO_UPDATE_CHECK") == "" {
		updater = updates.New(updates.MyGo{})
	}

	mac := runtime.GOOS == "darwin"
	theme.RegisterFonts()
	mygo.App.SetName("Droi")
	mygo.App.RequestSingleInstanceLock()

	var win *mygo.Window
	update := func(fn func()) {
		if win != nil {
			win.Update(fn)
		}
	}
	gw := startGateway(h, version, mygo.Shell.TrashItem)
	mem := startMemory(h, home, func() { update(func() {}) })
	var remote app.Remote
	if gw != nil {
		remote = gw
	}

	a := app.New(app.Config{
		Controller:   ctl,
		Prefs:        prefs.Open(filepath.Join(userData, "preferences.json")),
		SystemPrompt: func() []byte { return h.SystemPrompt() },
		Update:       update,
		InsetTop:     mac,
		FactoryHome:  h.FactoryHome(),
		OpenPath:     mygo.Shell.OpenPath,
		ShowInFolder: mygo.Shell.ShowItemInFolder,
		Scratch:      h.Scratch,
		Host:         h,
		Version:      version,
		Env:          os.Getenv,
		PlaySound:    func(s string) { playSound(h.FactoryHome(), s) },
		// The id is the Session's, so a click (even one that launches the
		// app again) knows what to open; a newer alert replaces the older.
		Notify: func(n app.Alert) {
			_ = mygo.NewNotification(mygo.NotificationOptions{ID: n.SessionID, Title: n.Title, Body: n.Body, Silent: true}).Show()
		},
		Focused:   func() bool { return win != nil && win.IsFocused() },
		ReadImage: mygo.Clipboard.ReadImage,
		Updater:   updater,
		Relaunch:  mygo.App.Relaunch,
		Remote:    remote,
		Memory:    mem,

		FactoryAppRunning: host.FactoryAppRunning,
	})
	if updater != nil {
		var waiting sync.Once
		updater.OnChange(func(s updates.State) {
			update(func() {})
			if s.Status == updates.Ready {
				waiting.Do(func() { go relaunchWhenIdle(a, update) })
			}
		})
	}
	mygo.App.OnNotificationClick(func(id string) {
		if win == nil {
			return
		}
		win.Show()
		win.Focus()
		win.Update(func() { a.OpenSession(id) })
	})
	highlight.Ready = func() { update(func() {}) }
	h.OnChange(func() { update(func() {}) })

	var setMenu func()
	a.SetLanguageChanged(func() {
		if setMenu != nil {
			setMenu()
		}
	})
	mygo.App.WhenReady(func() {
		setMenu = func() {
			mygo.App.SetMenu(menu(func(id string) {
				update(func() { a.Run(id) })
			}, func(step int) {
				if win != nil {
					win.Update(func() { a.Zoom(step) })
				}
			}, func() {
				if win == nil {
					nativePaste()
					return
				}
				win.Update(func() {
					if !a.PasteImage() {
						nativePaste()
					}
				})
			}))
		}
		a.SetLocale(mygo.App.Locale())
		setMenu()
		opts := mygo.WindowOptions{
			Title:           "Droi",
			Width:           1280,
			Height:          800,
			MinWidth:        720,
			MinHeight:       480,
			StateKey:        "main",
			BackgroundColor: "#f3f3f3",
			Content:         ui.View(a.View),
		}
		if mac {
			// Centres the traffic lights (about 14pt tall) in the Client's 44pt
			// top strip, where the sidebar toggle and the page headers sit.
			opts.TitleBarStyle = mygo.TitleBarHiddenInset
			opts.TrafficLightPosition = &mygo.Point{X: 13, Y: 15}
		} else {
			// The system's title bar: its buttons would cover the headers' right
			// end. The menu bar stays out of sight; Alt shows it.
			opts.AutoHideMenuBar = true
		}
		win = mygo.NewWindow(opts)
		h.Start()
		a.Start()
		if updater != nil {
			go updater.Schedule(context.Background())
		}
		go connectWhenSignedIn(ctl, h)
	})
	mygo.App.OnActivate(func(visible bool) {
		if win != nil && !visible {
			win.Show()
		}
	})
	mygo.App.OnSecondInstance(func([]string, string) {
		if win != nil {
			win.Show()
			win.Focus()
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if gw != nil {
			gw.Close()
		}
		mem.Stop()
		ctl.Close()
		h.Stop()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// menu is the Desktop Shell's: the app, File, Edit, View and Window menus,
// with zoom on ⌘= / ⌘- / ⌘0 as people press them, and the window's
// commands (app.Commands), which take their shortcuts here.
func menu(run func(id string), zoom func(step int), paste func()) *mygo.Menu {
	mac := runtime.GOOS == "darwin"
	item := func(label, acc string, step int, hidden bool) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Accelerator: acc, Hidden: hidden, Click: func(*mygo.MenuItem, *mygo.Window) { zoom(step) }}
	}
	cmds := map[string]*mygo.MenuItem{}
	var hidden []*mygo.MenuItem
	for _, c := range app.Commands() {
		it := &mygo.MenuItem{Label: c.Label, Accelerator: c.Key, Hidden: c.Hidden, Click: func(*mygo.MenuItem, *mygo.Window) { run(c.ID) }}
		cmds[c.ID] = it
		if c.Hidden {
			hidden = append(hidden, it)
		}
	}
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout, Label: l10n.L("About Droi")},
		mygo.Separator(),
		cmds[app.CmdSettings],
		mygo.Separator(),
		{Role: mygo.RoleServices, Label: l10n.L("Services")},
		mygo.Separator(),
		{Role: mygo.RoleHide, Label: l10n.L("Hide Droi")},
		{Role: mygo.RoleHideOthers, Label: l10n.L("Hide Others")},
		{Role: mygo.RoleUnhide, Label: l10n.L("Show All")},
		mygo.Separator(),
		quit(),
	}}
	file := []*mygo.MenuItem{cmds[app.CmdNewSession], mygo.Separator(), {Role: mygo.RoleClose, Label: l10n.L("Close Window")}}
	if !mac {
		// Without an app menu, Settings and Quit are in File.
		file = []*mygo.MenuItem{cmds[app.CmdNewSession], cmds[app.CmdSettings], mygo.Separator(), quit()}
	}
	view := []*mygo.MenuItem{
		cmds[app.CmdToggleSidebar],
		cmds[app.CmdScrollToLatest],
		mygo.Separator(),
		item(l10n.L("Actual Size"), "CmdOrCtrl+0", 0, false),
		item(l10n.L("Zoom In"), "CmdOrCtrl+=", 1, false),
		item(l10n.L("Zoom In"), "CmdOrCtrl+Plus", 1, true),
		item(l10n.L("Zoom Out"), "CmdOrCtrl+-", -1, false),
		mygo.Separator(),
		{Role: mygo.RoleToggleFullScreen, Label: l10n.L("Toggle Full Screen")},
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		appMenu,
		{Label: l10n.L("File"), Submenu: file},
		editMenu(paste),
		{Label: l10n.L("View"), Submenu: append(view, hidden...)},
		windowMenu(mac),
	})
}

// quit is the Quit item, labeled as MyGo labels the role on each platform.
func quit() *mygo.MenuItem {
	switch runtime.GOOS {
	case "darwin":
		return &mygo.MenuItem{Role: mygo.RoleQuit, Label: l10n.L("Quit Droi")}
	case "windows":
		return &mygo.MenuItem{Role: mygo.RoleQuit, Label: l10n.L("Exit")}
	}
	return &mygo.MenuItem{Role: mygo.RoleQuit, Label: l10n.L("Quit")}
}

// windowMenu is MyGo's Window menu, with the role's own items, so that its
// labels can be translated. The role stays: the system lists windows in it.
func windowMenu(mac bool) *mygo.MenuItem {
	items := []*mygo.MenuItem{
		{Role: mygo.RoleMinimize, Label: l10n.L("Minimize")},
		{Role: mygo.RoleClose, Label: l10n.L("Close Window")},
	}
	if mac {
		items = []*mygo.MenuItem{
			{Role: mygo.RoleMinimize, Label: l10n.L("Minimize")},
			{Role: mygo.RoleZoom, Label: l10n.L("Zoom")},
			mygo.Separator(),
			{Role: mygo.RoleFront, Label: l10n.L("Bring All to Front")},
		}
	}
	return &mygo.MenuItem{Role: mygo.RoleWindowMenu, Label: l10n.L("Window"), Submenu: items}
}

// editMenu is MyGo's Edit menu of macOS, but for Paste, which the app does
// itself: the role's Paste goes straight to the focused text area, and the
// composer must see it first to attach an image on the clipboard.
func editMenu(paste func()) *mygo.MenuItem {
	return &mygo.MenuItem{Label: l10n.L("Edit"), Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleUndo, Label: l10n.L("Undo")},
		{Role: mygo.RoleRedo, Label: l10n.L("Redo")},
		mygo.Separator(),
		{Role: mygo.RoleCut, Label: l10n.L("Cut")},
		{Role: mygo.RoleCopy, Label: l10n.L("Copy")},
		{Label: l10n.L("Paste"), Accelerator: "CmdOrCtrl+V", Click: func(*mygo.MenuItem, *mygo.Window) { paste() }},
		{Role: mygo.RolePasteAndMatchStyle, Label: l10n.L("Paste and Match Style")},
		{Role: mygo.RoleDelete, Label: l10n.L("Delete")},
		{Role: mygo.RoleSelectAll, Label: l10n.L("Select All")},
		mygo.Separator(),
		{Label: l10n.L("Speech"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleStartSpeaking, Label: l10n.L("Start Speaking")},
			{Role: mygo.RoleStopSpeaking, Label: l10n.L("Stop Speaking")},
		}},
	}}
}

// connectWhenSignedIn keeps the window connected to the Daemon. Connect,
// and the controller's own reconnecting, give up on a Daemon that rejects
// the credential (none yet, or signed out), so whenever the controller or
// the Host changes (a sign-in, an API key, a restarted Daemon) and the
// window is neither connected nor reconnecting, it connects again.
func connectWhenSignedIn(ctl *controller.Controller, h *host.Host) {
	poke := make(chan struct{}, 1)
	wake := func() {
		select {
		case poke <- struct{}{}:
		default:
		}
	}
	defer h.OnChange(wake)()
	defer ctl.Subscribe(func(e controller.Event) {
		if _, ok := e.(controller.StatusChanged); ok {
			wake()
		}
	})()
	for {
		if st := ctl.Status(); !st.Connected && !st.Reconnecting && h.HasCredential() {
			if err := ctl.Connect(context.Background()); err != nil {
				log.Println("droi: connect:", err)
			}
		}
		<-poke
	}
}

// executable is the app's own binary, which the Daemon runs for Memory;
// "" under `go run`, whose binary is gone once it exits.
func executable() string {
	exe, err := os.Executable()
	if err != nil || strings.Contains(exe, "go-build") {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe
}

// relaunchWhenIdle restarts into an installed update once no Session is
// busy and the window is in the background, checking every minute.
func relaunchWhenIdle(a *app.App, update func(func())) {
	for {
		time.Sleep(time.Minute)
		done := make(chan bool, 1)
		update(func() { done <- a.RelaunchIfIdle() })
		select {
		case ok := <-done:
			if ok {
				return
			}
		case <-time.After(10 * time.Second): // no window to ask
		}
	}
}

// playSound plays an alert sound: "bell" is the system's beep, a built-in
// name is a .wav the droid CLI keeps under ~/.factory/sounds, and anything
// else is a file the user chose.
func playSound(factoryHome, sound string) {
	switch {
	case sound == "" || sound == "off":
		return
	case sound == "bell":
		mygo.Shell.Beep()
		return
	}
	path := sound
	if !filepath.IsAbs(path) {
		if strings.ContainsAny(sound, `/\`) {
			return
		}
		path = filepath.Join(factoryHome, "sounds", sound+".wav")
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	go playFile(path)
}
