// Command native is Droi as a native app: the Host starts the Daemon and
// supplies the credential, and one MyGo window draws the Client (package
// app) and talks to the Daemon directly through the Go SDK.
package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/app"
	"github.com/kkkk2323/droi/apps/native/internal/highlight"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/theme"
)

// version is the release version; the build sets it with -ldflags.
var version = "1.33.5"

func main() {
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
	})
	ctl := controller.New(controller.Config{
		ResolveURL: h.WaitForDaemonURL,
		Credential: h.Credential,
		Caller:     "droi-native",
		// The Daemon restarts on its own (the Host's Supervisor); keep trying.
		MaxReconnectAttempts: 1000,
		DefaultMessageLimit:  app.LoadedMessageLimit,
	})

	theme.RegisterFonts()
	mygo.App.SetName("Droi")
	mygo.App.SetVersion(version)
	mygo.App.RequestSingleInstanceLock()

	var win *mygo.Window
	update := func(fn func()) {
		if win != nil {
			win.Update(fn)
		}
	}
	a := app.New(app.Config{
		Controller:   ctl,
		Prefs:        prefs.Open(filepath.Join(userData, "preferences.json")),
		SystemPrompt: func() []byte { return h.SystemPrompt() },
		Update:       update,
		InsetTop:     true,
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
		Focused: func() bool { return win != nil && win.IsFocused() },
	})
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

	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(menu())
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Droi",
			Width:     1280,
			Height:    800,
			MinWidth:  720,
			MinHeight: 480,
			StateKey:  "main",
			// Centres the traffic lights (about 14pt tall) in the Client's 44pt
			// top strip, where the sidebar toggle and the page headers sit.
			TitleBarStyle:        mygo.TitleBarHiddenInset,
			TrafficLightPosition: &mygo.Point{X: 13, Y: 15},
			BackgroundColor:      "#f3f3f3",
			Content:              ui.View(a.View),
		})
		h.Start()
		a.Start()
		go func() {
			if err := ctl.Connect(context.Background()); err != nil {
				log.Println("droi: connect:", err)
			}
		}()
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
		ctl.Close()
		h.Stop()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// menu is the Desktop Shell's: the app, File, Edit, View and Window menus.
func menu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleFileMenu},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
	})
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
	go func() { _ = exec.Command("/usr/bin/afplay", path).Run() }()
}
