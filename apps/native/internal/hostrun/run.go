// Package hostrun is `droi host` (ADR 0019): a Host with no window. It
// starts the Daemon, the Gateway with Remote Access always on, Memory and
// the Chat Bridge, prints the pairing link, and stops them all when its
// context ends.
package hostrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// ErrHostRunning is returned when a Droi Host already answers on this
// computer: the app, or another `droi host`.
var ErrHostRunning = errors.New("a Droi Host already runs on this computer")

// Options is what Run needs from the program around it.
type Options struct {
	Home    string
	Version string
	// Env reads the environment: XDG_CONFIG_HOME, XDG_DATA_HOME, PATH and
	// FACTORY_API_KEY. Nil reads nothing.
	Env func(string) string
	// Executable runs the Memory Server and the hook; "" leaves Memory out.
	Executable string
	// Port is the Gateway's preferred port, gateway.PreferredPort when 0.
	Port int
	// Out gets the pairing link and, when Terminal is set, its QR code.
	Out      io.Writer
	Terminal bool
}

// Run runs a Host until ctx ends. It returns an error, with nothing left
// running, when it cannot start.
func Run(ctx context.Context, o Options) error {
	env := o.Env
	if env == nil {
		env = func(string) string { return "" }
	}
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	port := o.Port
	if port == 0 {
		port = gateway.PreferredPort
	}
	if hostAnswers(ctx, port) {
		return fmt.Errorf("%w (its Gateway answers on port %d); stop it first", ErrHostRunning, port)
	}

	trash := &host.FreedesktopTrash{Dir: TrashDir(env, o.Home)}
	h := host.New(host.Config{
		UserData:   ConfigDir(env, o.Home),
		Home:       o.Home,
		Version:    o.Version,
		Env:        env,
		Executable: o.Executable,
	})
	if err := checkDroid(h); err != nil {
		return err
	}

	opts := GatewayOptions(h, o.Version, trash.Move)
	opts.Port, opts.RemoteAccess = port, true
	gw, err := gateway.Start(opts)
	if err != nil {
		return fmt.Errorf("the Gateway cannot listen: %w", err)
	}
	mem := StartMemory(h, o.Home, func() {})
	chat := StartChatBridge(h)
	h.Start()
	defer func() {
		_ = gw.Close()
		chat.Stop()
		mem.Stop()
		h.Stop()
	}()

	if !h.HasCredential() {
		fmt.Fprintln(out, "Warning: no login. Run `droid login` on this computer, or set FACTORY_API_KEY, then restart.")
	}
	// The Gateway listens whether or not the Daemon is up, so the link comes
	// first: a Daemon that takes long to start must not hide it.
	announce(out, o.Terminal, gw, h, o.Version)
	if err := waitForDaemon(ctx, h); err != nil {
		fmt.Fprintf(out, "Warning: the Daemon is not running yet: %s\n", err)
	}
	<-ctx.Done()
	return nil
}

// hostAnswers reports that a Droi Gateway answers /meta on port, as the app's
// does and a Host's does.
func hostAnswers(ctx context.Context, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := gateway.FetchMeta(ctx, "http://127.0.0.1:"+strconv.Itoa(port))
	return err == nil
}

// checkDroid refuses a missing droid, or one older than Droi needs, before
// anything listens: a Supervisor would retry such a Daemon forever.
func checkDroid(h *host.Host) error {
	if h.DroidPath() == "" {
		return errors.New("droid was not found; install it and run `droid login`")
	}
	if v := h.DroidVersion(); host.OlderThanMinDroid(v) {
		return fmt.Errorf("droid %s is too old: Droi needs droid %s or newer; run `droid update`", v, host.MinDroid)
	}
	return nil
}

func waitForDaemon(ctx context.Context, h *host.Host) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if _, err := h.WaitForDaemonURL(ctx); err != nil {
		if r := h.Daemon.State().Reason; r != "" {
			return errors.New(r)
		}
		return err
	}
	return nil
}

func announce(out io.Writer, terminal bool, gw *gateway.Gateway, h *host.Host, version string) {
	s := h.Settings.Get()
	info := gw.Pairing(pairingHost(s), s.PairingToken)
	link := info.Link
	fmt.Fprintf(out, "Droi Host %s on %s\n", version, gateway.ComputerName())
	if link == "" {
		// Only loopback: reachable through a tunnel, not from the network.
		link = gateway.Pairing([]string{"127.0.0.1"}, info.Port, "", s.PairingToken).Link
		fmt.Fprintln(out, "No network address found; the Gateway listens on this computer only.")
	}
	fmt.Fprintf(out, "Pairing link: %s\n", link)
	if terminal {
		fmt.Fprint(out, renderQR(link))
	}
	fmt.Fprintln(out, "Press Ctrl-C to stop.")
}

func pairingHost(s host.Settings) string {
	if s.PairingHost != nil {
		return *s.PairingHost
	}
	return ""
}
