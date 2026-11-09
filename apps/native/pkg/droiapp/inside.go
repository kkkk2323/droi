package droiapp

import (
	"context"
	"log"

	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/v2/internal/app"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// inside is what the window draws for the computer it follows: the App,
// the Controller it uses, and what keeps that Controller connected. A
// switch to another computer makes a new one and closes the old.
type inside struct {
	ctl       *controller.Controller
	app       *app.App
	following bool
	stop      context.CancelFunc
	// unhear stops the window hearing the Host's Kernel, and link is the connection it keeps to it; both nil when
	// the window does not use one.
	unhear func()
	link   *host.KernelLink
}

// start starts the App and keeps its Controller connected, and its Kernel connection when this computer's Host runs
// one. A window that follows another computer stays on that computer's Daemon.
func (in *inside) start(h *host.Host) {
	in.app.Start()
	ctx, cancel := context.WithCancel(context.Background())
	in.stop = cancel
	go connectWhenSignedIn(ctx, in.ctl, h, in.following)
	if !in.following && h.KernelEnabled() {
		in.link = host.NewKernelLink(host.KernelLinkOptions{
			Client:   "window",
			Token:    h.KernelToken,
			OnClient: func(c *kernel.Client, gen int) { in.app.UseKernel(c, int64(gen)) },
			Log:      func(s string) { log.Println("droi: kernel:", s) },
		})
		in.unhear = h.OnKernelState(in.link.Use)
	}
}

// close stops what keeps the Controller connected, the App, and then the
// Controller, whose closing the App no longer hears. The App closes before its
// Kernel connection: it writes what it saw on the way out.
func (in *inside) close() {
	if in.stop != nil {
		in.stop()
	}
	if in.unhear != nil {
		in.unhear()
	}
	in.app.Close()
	if in.link != nil {
		in.link.Close()
	}
	in.ctl.Close()
}
