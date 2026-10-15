package main

import (
	"context"

	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/app"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// inside is what the window draws for the computer it follows: the App,
// the Controller it uses, and what keeps that Controller connected. A
// switch to another computer makes a new one and closes the old.
type inside struct {
	ctl       *controller.Controller
	app       *app.App
	following bool
	stop      context.CancelFunc
}

// start starts the App and keeps its Controller connected.
func (in *inside) start(h *host.Host) {
	in.app.Start()
	ctx, cancel := context.WithCancel(context.Background())
	in.stop = cancel
	go connectWhenSignedIn(ctx, in.ctl, h, in.following)
}

// close stops what keeps the Controller connected, the App, and then the
// Controller, whose closing the App no longer hears.
func (in *inside) close() {
	if in.stop != nil {
		in.stop()
	}
	in.app.Close()
	in.ctl.Close()
}
