package main

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// route is how the phone reaches its computer: the Gateway's network
// address, or its Relay when that does not answer (ADR 0034), chosen at
// every connect. The Gateway client follows the choice.
type route struct {
	p   computer.Paired
	cur atomic.Value // string: the Daemon socket URL last chosen
}

func newRoute(p computer.Paired) *route {
	r := &route{p: p}
	r.cur.Store(p.SocketURL())
	return r
}

func (r *route) resolve(ctx context.Context) (string, error) {
	url, err := gateway.SocketURL(r.p.Route(ctx), r.p.Token)
	if err != nil {
		return "", err
	}
	r.cur.Store(url)
	return url, nil
}

func (r *route) socketURL() string { return r.cur.Load().(string) }

// keepConnected connects ctl, and again whenever it drops, waiting longer
// after each failure, until ctx ends.
// A value on wake ends the wait at once.
func keepConnected(ctx context.Context, ctl *controller.Controller, wake <-chan struct{}) {
	poke := make(chan struct{}, 1)
	defer ctl.Subscribe(func(e controller.Event) {
		if _, ok := e.(controller.StatusChanged); ok {
			select {
			case poke <- struct{}{}:
			default:
			}
		}
	})()
	var delay time.Duration
	for ctx.Err() == nil {
		s := ctl.Status()
		if !s.Connected && !s.Reconnecting {
			if err := ctl.Connect(ctx); err != nil && ctx.Err() == nil {
				log.Println("droi: connect:", err)
				delay = min(max(2*delay, time.Second), 30*time.Second)
			} else {
				delay = 0
			}
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-wake:
				delay = 0
			case <-ctx.Done():
			}
			continue
		}
		select {
		case <-poke:
		case <-wake:
		case <-ctx.Done():
		}
	}
}
