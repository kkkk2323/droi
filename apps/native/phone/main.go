// Command phone is the Phone App: Droi's window on an iPhone, which follows
// one Paired Computer through its Gateway (ADR 0040). It draws the same
// views as the native app, with no Host of its own.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/app"
	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// devVersion is the version of `go run`; a build takes mygo.json's.
const devVersion = "1.99.2"

func main() {
	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	_ = os.MkdirAll(userData, 0o755)
	version := mygo.App.Version()
	if version == "" {
		version = devVersion
	}
	p := &phone{
		prefs:    prefs.Open(filepath.Join(userData, "preferences.json")),
		userData: userData,
		version:  version,
	}
	writeSounds(filepath.Join(userData, "sounds"))
	theme.RegisterFonts()
	mygo.App.SetName("Droi")
	mygo.App.SetLanguages(string(l10n.English), string(l10n.Chinese))
	mygo.App.WhenReady(func() {
		p.win = mygo.NewWindow(mygo.WindowOptions{Title: "Droi", BackgroundColor: "#ffffff", Content: ui.View(p.view)})
		p.show(computer.Load(p.prefs))
	})
	// Back from the background the socket may be long gone; connect at once
	// rather than when the wait after the last failure ends.
	mygo.Power.OnResume(func() {
		p.update(func() {
			if p.shown != nil {
				p.shown.wake()
			}
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// phone is the app's state: the window, and what it shows in it, the
// pairing page while no computer is paired, else that computer's App.
type phone struct {
	prefs    *prefs.Store
	userData string
	version  string
	win      *mygo.Window

	shown *inside
	pair  pairing
}

// inside is the window's insides for one Paired Computer.
type inside struct {
	ctl  *controller.Controller
	app  *app.App
	stop context.CancelFunc
	poke chan struct{}
}

// wake connects now if the phone is not connected.
func (in *inside) wake() {
	select {
	case in.poke <- struct{}{}:
	default:
	}
}

func (p *phone) update(fn func()) {
	if p.win != nil {
		p.win.Update(fn)
	}
}

// show builds the window's insides for to, the pairing page when nil, in
// place of what it showed. Main thread only.
func (p *phone) show(to *computer.Paired) {
	old := p.shown
	p.shown = nil
	if to != nil {
		p.shown = p.open(*to)
	}
	if old != nil {
		old.close()
	}
	p.update(func() {})
}

func (p *phone) open(to computer.Paired) *inside {
	via := newRoute(to)
	ctl := controller.New(controller.Config{
		ResolveURL:           via.resolve,
		Credential:           gateway.PlaceholderCredential,
		Caller:               "droi-phone",
		MaxReconnectAttempts: 1000,
		DefaultMessageLimit:  app.LoadedMessageLimit,
	})
	a := app.New(app.Config{
		Controller: ctl,
		Prefs:      p.prefs,
		Computer:   &to,
		Phone:      true,
		Switch: func(next *computer.Paired) {
			p.update(func() { p.show(next) })
		},
		Update:     p.update,
		StatsFile:  filepath.Join(p.userData, "session-stats.json"),
		Gateway:    gateway.NewClientFunc(via.socketURL),
		Version:    p.version,
		Env:        os.Getenv,
		ReadImage:  mygo.Clipboard.ReadImage,
		PickImages: pickImages,
		ScanQR:     scanQR,
		Alerted:    p.alerted,
	})
	a.SetLocale(systemLocale())
	in := &inside{ctl: ctl, app: a, poke: make(chan struct{}, 1)}
	a.Start()
	ctx, cancel := context.WithCancel(context.Background())
	in.stop = cancel
	go keepConnected(ctx, ctl, in.poke)
	return in
}

func (in *inside) close() {
	in.stop()
	in.app.Close()
	in.ctl.Close()
}

func (p *phone) view(c *ui.Context) {
	if p.shown != nil {
		p.shown.app.View(c)
		return
	}
	p.pairingView(c, app.PrefsKit(p.prefs, systemLocale()))
}

// systemLocale is the language the user reads best, such as "zh-Hans-CN".
func systemLocale() string {
	if langs := mygo.App.PreferredLanguages(); len(langs) > 0 {
		return langs[0]
	}
	return ""
}
