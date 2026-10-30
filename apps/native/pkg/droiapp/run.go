// Package droiapp is Droi as a native app, for a main package to run: the
// Host starts the Daemon and supplies the credential, and one MyGo window
// draws the Client (package app) and reaches the Daemon through the Host's
// own Gateway with the Go SDK. Droi's main runs it as Droi; another app runs
// it under its own brand with its own extensions (ADR 0029).
package droiapp

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/app"
	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/internal/hostrun"
	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/internal/updates"
	"github.com/kkkk2323/droi/apps/native/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// Options are what makes an app of Droi's.
type Options struct {
	// DevVersion is the version of a build MyGo did not make, such as go run;
	// a MyGo build takes mygo.json's.
	DevVersion string
	// Brand names the app; its empty fields are Droi's.
	Brand brand.Brand
	// Extensions are what the app adds to Droi.
	Extensions []extension.Extension
	// OwnFactoryHome runs the Daemon on a Factory home in the app's data
	// folder, <data>/factory/.factory, instead of the droid CLI's ~/.factory:
	// the CLI's login does not count as the app's, and the app's Sessions,
	// settings, skills and MCP servers are its own.
	OwnFactoryHome bool
	// HiddenSettings are the ids of Droi's Settings sections the app leaves
	// out: "account", "general", "notifications", "defaults", "memory",
	// "worktrees", "computer", "remote", "usage" or "advanced". About stays.
	// A link to a section left out opens the first one shown.
	HiddenSettings []string
	// DroidDownloads, optional, is where Droi's droid (ADR 0033) downloads
	// Factory's droid builds instead of Factory's releases: a mirror the app
	// keeps, with <version>/<os>/<arch>/droid and its .sha256 under it, and
	// the newest version in LATEST next to it (droidhook.LatestURLFor). Each
	// build is still checked against its checksum and with the hook.
	DroidDownloads string
	// RecipesURL was where the Patched Droid (ADR 0027) read its recipes.
	//
	// Deprecated: Droi no longer patches droid; it is ignored.
	RecipesURL string
	// Presets, optional, are the app's defaults for a new install: Session
	// Defaults, such as the model, and the tool mode of new Sessions. The
	// window gives them to this computer's Daemon once per Version, so what
	// the user changes afterwards stays.
	Presets *defaults.Presets
}

// useDroidMirror points Droi's droid at the app's mirror, if it has one.
func useDroidMirror(o Options) {
	if o.DroidDownloads != "" {
		droidhook.DownloadBase = o.DroidDownloads
	}
}

// hostUnsupported is why `droi host` does not run on goos, "" where it does:
// elsewhere the app is the Host (ADR 0019).
func hostUnsupported(goos string) string {
	if goos == "linux" {
		return ""
	}
	b := brand.Current()
	return fmt.Sprintf("%s host runs on Linux only. On this computer the %s app is the Host.", b.ID, b.Name)
}

// Run runs the app until it quits, or the subcommand os.Args names; it does
// not return.
func Run(o Options) {
	brand.Set(o.Brand)
	useDroidMirror(o)
	exts := newExtensions(o.Extensions)
	exts.register()
	version := mygo.App.Version()
	if version == "" {
		version = o.DevVersion
	}
	// The Runtime Overlay runs the app's own executable as the Memory Server
	// and the hook; neither starts the app. `droi host` is a Host with no
	// window.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case host.MemoryServerCommand:
			os.Exit(memory.ServeMain())
		case host.MemoryHookCommand:
			os.Exit(memory.HookMain())
		case "host":
			if msg := hostUnsupported(runtime.GOOS); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
				os.Exit(2)
			}
			os.Exit(hostrun.Main(os.Args[2:], hostrun.Options{
				Version:            version,
				Executable:         executable(),
				Overlay:            exts.overlay,
				Start:              exts.start,
				AppendSystemPrompt: exts.systemPrompt,
				OwnFactoryHome:     o.OwnFactoryHome,
			}))
		}
		if cmd := exts.command(os.Args[1]); cmd != nil {
			os.Exit(cmd(os.Args[2:]))
		}
	}
	if runtime.GOOS == "linux" {
		b := brand.Current()
		fmt.Fprintf(os.Stderr, "%s: only `%s host` is supported on Linux.\n", b.ID, b.ID)
		os.Exit(2)
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

	userPrefs := prefs.Open(filepath.Join(userData, "preferences.json"))
	factoryRoot := ""
	if o.OwnFactoryHome {
		factoryRoot = filepath.Join(userData, "factory")
	}
	var h *host.Host
	h = host.New(host.Config{
		UserData:    userData,
		Home:        home,
		Version:     version,
		Env:         os.Getenv,
		Executable:  executable(),
		FactoryRoot: factoryRoot,
		ScriptModels: func() []string {
			return prefs.ScriptModels.Get(userPrefs)
		},
		// Read when the Daemon starts, which is after New.
		Overlay:            func() host.OverlayOptions { return exts.overlay(h) },
		AppendSystemPrompt: exts.systemPrompt,
	})
	extCtx, stopExtensions := context.WithCancel(context.Background())
	localToken := gateway.NewLocalToken()
	var win *mygo.Window
	bar := &menuBar{win: func() *mygo.Window { return win }}
	update := func(fn func()) {
		if win != nil {
			win.Update(func() {
				fn()
				bar.sync()
				bar.invalidate()
			})
		}
	}
	sessionList := &app.SessionListShare{Prefs: userPrefs, Changed: func() { update(func() {}) }}
	gw, gwErr := startGateway(h, version, localToken, mygo.Shell.TrashItem, sessionList)
	if gwErr != nil {
		log.Println("droi: gateway:", gwErr)
	}

	// A build signed for updates can replace itself; `go run` cannot.
	var updater *updates.Updater
	if mygo.Updater.Enabled() && os.Getenv("DROI_NO_UPDATE_CHECK") == "" {
		updater = updates.New(updates.MyGo{})
	}

	servePprof()
	mac := runtime.GOOS == "darwin"
	theme.RegisterFonts()
	mygo.App.SetName(brand.Name())
	mygo.App.SetLanguages(string(l10n.English), string(l10n.Chinese))
	mygo.App.RequestSingleInstanceLock()

	mem := hostrun.StartMemory(h, home, func() { update(func() {}) })
	var remote app.Remote
	if gw != nil {
		remote = gw
	}

	var setMenu func()
	// shown is the window's insides for the computer it follows, built
	// again in place of the old when the window switches to another; this
	// computer's Host, Gateway and Memory run all the same, for its phones
	// and Automations. Main thread only.
	var shown *inside
	var switchTo func(to *computer.Paired)
	open := func(followed *computer.Paired) *inside {
		ctl := controller.New(controller.Config{
			ResolveURL: windowURL(followed, gw, gwErr, localToken, h.WaitForDaemonURL),
			Credential: gateway.PlaceholderCredential,
			Caller:     "droi-native",
			// The Daemon restarts on its own (the Host's Supervisor); keep trying.
			MaxReconnectAttempts: 1000,
			DefaultMessageLimit:  app.LoadedMessageLimit,
		})
		a := app.New(app.Config{
			Controller:   ctl,
			Prefs:        userPrefs,
			Computer:     followed,
			Switch:       func(to *computer.Paired) { switchTo(to) },
			Update:       update,
			InsetTop:     mac,
			StatsFile:    filepath.Join(userData, "session-stats.json"),
			OpenPath:     mygo.Shell.OpenPath,
			ShowInFolder: mygo.Shell.ShowItemInFolder,
			FileIcon:     mygo.Shell.FileIcon,
			Gateway:      windowClient(followed, gw, localToken),
			Host:         h,
			Version:      version,
			Env:          os.Getenv,
			PlaySound:    func(s string) { playSound(h.SharedFactoryHome(), s) },
			// The id is the Session's, so a click (even one that launches the
			// app again) knows what to open; a newer alert replaces the older.
			Notify: func(n app.Alert) {
				_ = mygo.NewNotification(mygo.NotificationOptions{ID: n.SessionID, Title: n.Title, Body: n.Body, Silent: true}).Show()
			},
			Focused:   func() bool { return win != nil && win.IsFocused() },
			ReadImage: mygo.Clipboard.ReadImage,
			ReadFiles: func() []string { p, _ := mygo.Clipboard.ReadFiles(); return p },
			Updater:   updater,
			Relaunch:  mygo.App.Relaunch,
			Remote:    remote,
			Memory:    mem,

			SessionList: sessionList,

			FactoryAppRunning: host.FactoryAppRunning,
			MenuBar:           bar.shell(),

			Pages:          exts.pages,
			SettingsTabs:   exts.settingsTabs,
			SignIn:         exts.signIn,
			HiddenSettings: o.HiddenSettings,
			Presets:        o.Presets,
		})
		a.SetLanguageChanged(func() {
			mygo.App.SetLocale(string(l10n.Code()))
			if setMenu != nil {
				setMenu()
			}
		})
		return &inside{ctl: ctl, app: a, following: followed != nil}
	}
	shown = open(computer.Load(userPrefs))
	switchTo = func(to *computer.Paired) {
		old := shown
		shown = open(to)
		shown.app.SetLocale(systemLocale())
		shown.start(h)
		old.close()
		update(func() {})
	}
	current := func() *app.App { return shown.app }
	bar.current = current

	if updater != nil {
		var waiting sync.Once
		updater.OnChange(func(s updates.State) {
			update(func() {})
			if s.Status == updates.Ready {
				waiting.Do(func() { go relaunchWhenIdle(current, update) })
			}
		})
	}
	mygo.App.OnNotificationClick(func(id string) {
		if win == nil {
			return
		}
		win.Show()
		win.Focus()
		win.Update(func() { current().OpenSession(id) })
	})
	h.OnChange(func() { update(func() {}) })

	mygo.App.WhenReady(func() {
		setMenu = func() {
			mygo.App.SetMenu(menu(func(id string) {
				update(func() { current().Run(id) })
			}, func(step int) {
				if win != nil {
					win.Update(func() { current().Zoom(step) })
				}
			}))
		}
		current().SetLocale(systemLocale())
		mygo.App.SetLocale(string(l10n.Code()))
		setMenu()
		opts := mygo.WindowOptions{
			Title:           brand.Name(),
			Width:           1280,
			Height:          800,
			MinWidth:        720,
			MinHeight:       480,
			StateKey:        "main",
			BackgroundColor: "#f3f3f3",
			Content:         ui.View(func(c *ui.Context) { current().View(c) }),
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
		exts.start(extCtx, h)
		shown.start(h)
		if updater != nil {
			go updater.Schedule(context.Background())
		}
		update(func() {})
		go bar.tick(update)
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
		stopExtensions()
		bar.remove()
		if gw != nil {
			gw.Close()
		}
		mem.Stop()
		shown.close()
		h.Stop()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// systemLocale is the language the user reads best, such as "zh-Hans-CN",
// which the app's language follows unless Settings picks one. App.Locale
// is not it: since MyGo 0.5 that is the language the app shows.
func systemLocale() string {
	if langs := mygo.App.PreferredLanguages(); len(langs) > 0 {
		return langs[0]
	}
	return ""
}

// menu is the Desktop Shell's: the app, File, Edit, View and Window menus,
// with zoom on ⌘= / ⌘- / ⌘0 as people press them, and the window's
// commands (app.Commands), which take their shortcuts here.
func menu(run func(id string), zoom func(step int)) *mygo.Menu {
	mac := runtime.GOOS == "darwin"
	item := func(label, acc string, step int, hidden bool) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Accelerator: acc, Hidden: hidden, Click: func(*mygo.MenuItem, *mygo.Window) { zoom(step) }}
	}
	cmds := map[string]*mygo.MenuItem{}
	for _, c := range app.Commands() {
		if !c.Hidden {
			cmds[c.ID] = &mygo.MenuItem{Label: c.Label, Accelerator: c.Key, Click: func(*mygo.MenuItem, *mygo.Window) { run(c.ID) }}
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
		editMenu(),
		{Label: l10n.L("View"), Submenu: view},
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

// editMenu is MyGo's Edit menu of macOS, with the role's own items, so that
// its labels can be translated.
func editMenu() *mygo.MenuItem {
	return &mygo.MenuItem{Label: l10n.L("Edit"), Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleUndo, Label: l10n.L("Undo")},
		{Role: mygo.RoleRedo, Label: l10n.L("Redo")},
		mygo.Separator(),
		{Role: mygo.RoleCut, Label: l10n.L("Cut")},
		{Role: mygo.RoleCopy, Label: l10n.L("Copy")},
		{Role: mygo.RolePaste, Label: l10n.L("Paste")},
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

// shouldConnect says whether the window should connect again: it is neither
// connected nor reconnecting, and the Daemon it reaches can authenticate it.
// This computer's Daemon needs this Host's login; another computer's Gateway
// holds its own, so this Host's is not asked.
func shouldConnect(st controller.Status, following bool, hasCredential func() bool) bool {
	return !st.Connected && !st.Reconnecting && (following || hasCredential())
}

// connectWhenSignedIn keeps the window connected to the Daemon until ctx
// ends. Connect, and the controller's own reconnecting, give up on a Daemon
// that rejects the credential (none yet, or signed out), so whenever the
// controller or the Host changes (a sign-in, an API key, a restarted
// Daemon) and the window is neither connected nor reconnecting, it connects
// again.
func connectWhenSignedIn(ctx context.Context, ctl *controller.Controller, h *host.Host, following bool) {
	poke, hostPoke := make(chan struct{}, 1), make(chan struct{}, 1)
	wake := func(ch chan struct{}) {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	defer h.OnChange(func() { wake(hostPoke) })()
	defer ctl.Subscribe(func(e controller.Event) {
		if _, ok := e.(controller.StatusChanged); ok {
			wake(poke)
		}
	})()
	var delay time.Duration
	for ctx.Err() == nil {
		if shouldConnect(ctl.Status(), following, h.HasCredential) {
			if err := ctl.Connect(ctx); err != nil && ctx.Err() == nil {
				log.Println("droi: connect:", err)
				delay = connectRetryDelay(delay)
			} else {
				delay = 0
			}
		}
		if delay > 0 {
			// A failed Connect changes the status, which would only connect
			// again at once: a Daemon that refuses the login refuses it again,
			// many times a second. Only a change on the Host (a sign-in, an
			// API key, a restarted Daemon) is worth trying before the wait ends.
			select {
			case <-hostPoke:
			case <-time.After(delay):
			case <-ctx.Done():
			}
			continue
		}
		select {
		case <-poke:
		case <-hostPoke:
		case <-ctx.Done():
		}
	}
}

// connectRetryDelay is the wait after a failed Connect that followed a wait
// of prev: a second, doubling to half a minute.
func connectRetryDelay(prev time.Duration) time.Duration {
	if prev <= 0 {
		return time.Second
	}
	return min(2*prev, 30*time.Second)
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

// relaunchWhenIdle restarts into an installed update once no Session of
// the window's current App is busy and the window is in the background,
// checking every minute. current is read on the main thread.
func relaunchWhenIdle(current func() *app.App, update func(func())) {
	for {
		time.Sleep(time.Minute)
		done := make(chan bool, 1)
		update(func() { done <- current().RelaunchIfIdle() })
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
