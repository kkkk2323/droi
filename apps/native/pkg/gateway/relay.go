package gateway

import (
	"context"
	"strings"

	"github.com/kkkk2323/droi/apps/native/v2/internal/relay"
)

// RelayConfig is the Relay a Host meets phones at when they are away from
// its network (ADR 0034): the Relay's address, its registration key ("" for
// an open Relay) and the Host's Relay Id.
type RelayConfig struct {
	URL, Key, ID string
}

func (c RelayConfig) usable() bool {
	return strings.TrimSpace(c.URL) != "" && relay.ValidID(c.ID)
}

// RelayState is where the Relay's Agent stands.
type RelayState = relay.State

// Where a Relay's Agent stands.
const (
	RelayOff        = relay.Off
	RelayConnecting = relay.Connecting
	RelayConnected  = relay.Connected
	RelayRetrying   = relay.Retrying
	RelayRefused    = relay.Refused
)

type relayRun struct {
	cfg    RelayConfig
	agent  *relay.Agent
	cancel context.CancelFunc
}

// ReconcileRelay starts, restarts or stops the Relay's Agent to match
// Remote Access and Options.Relay, after either changed.
func (g *Gateway) ReconcileRelay() {
	var want RelayConfig
	if g.RemoteAccess() && g.opts.Relay != nil {
		want = g.opts.Relay()
		want.URL = strings.TrimRight(strings.TrimSpace(want.URL), "/")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.relay != nil && g.relay.cfg == want && want.usable() {
		return
	}
	if g.relay != nil {
		g.relay.cancel()
		g.relay = nil
	}
	if g.closed || !want.usable() {
		g.changed()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := relay.NewAgent(relay.AgentOptions{
		URL: want.URL, Key: want.Key, ID: want.ID, Target: g.URL(),
		Name: g.opts.ComputerName, Version: g.opts.Version,
		OnState: func(relay.State) { g.changed() },
	})
	g.relay = &relayRun{cfg: want, agent: a, cancel: cancel}
	go a.Run(ctx)
	g.changed()
}

func (g *Gateway) stopRelay() {
	g.mu.Lock()
	r := g.relay
	g.relay = nil
	g.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}

func (g *Gateway) kickRelay() {
	g.mu.Lock()
	r := g.relay
	g.mu.Unlock()
	if r != nil {
		r.agent.Kick()
	}
}

// RelayState is where the Relay's Agent stands; Off while it does not run.
func (g *Gateway) RelayState() RelayState {
	g.mu.Lock()
	r := g.relay
	g.mu.Unlock()
	if r == nil {
		return RelayState{}
	}
	return r.agent.State()
}

// RelayAddress is where a phone reaches this Gateway through the Relay,
// "" while the Agent does not run.
func (g *Gateway) RelayAddress() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.relay == nil {
		return ""
	}
	return relay.ClientBase(g.relay.cfg.URL, g.relay.cfg.ID)
}
