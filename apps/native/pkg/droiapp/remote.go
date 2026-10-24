package droiapp

import (
	"context"
	"fmt"
	"log"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

// startGateway starts the Gateway that paired phones and this window connect
// through. The window presents localToken, which a Pairing Token reset or
// turning Remote Access off does not revoke.
func startGateway(h *host.Host, version, localToken string, moveToTrash func(string) error, list gateway.SessionList) (*gateway.Gateway, error) {
	opts := h.GatewayOptions(version, moveToTrash)
	opts.SessionList = list
	opts.Port = brand.Current().Port
	opts.RemoteAccess = h.Settings.Get().RemoteAccess
	opts.LocalToken = func() string { return localToken }
	return gateway.Start(opts)
}

// windowURL is the controller's ResolveURL: the Gateway of the computer the
// window follows, which needs nothing of this computer's Daemon, or this
// computer's own.
func windowURL(followed *computer.Paired, gw *gateway.Gateway, gwErr error, localToken string, daemonUp func(context.Context) (string, error)) func(context.Context) (string, error) {
	if followed != nil {
		url := followed.SocketURL()
		if url == "" {
			// A saved address that cannot be used still shows the way back
			// to this computer; the app must not stop at launch.
			err := fmt.Errorf("the saved address of %s, %q, cannot be used", followed.Name, followed.Gateway)
			return func(context.Context) (string, error) { return "", err }
		}
		return func(context.Context) (string, error) { return url, nil }
	}
	return gatewayURL(gw, gwErr, localToken, daemonUp)
}

// gatewayURL is the controller's ResolveURL: the Gateway's Daemon socket,
// once the Daemon runs, never the Daemon's own address. A Gateway that could
// not listen (gwErr) is an error the window shows.
func gatewayURL(gw *gateway.Gateway, gwErr error, localToken string, daemonUp func(context.Context) (string, error)) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if gw == nil {
			return "", fmt.Errorf("%s's Gateway could not listen, so the window cannot reach the Daemon: %w", brand.Name(), gwErr)
		}
		if _, err := daemonUp(ctx); err != nil {
			return "", err
		}
		return gw.DaemonSocketURL(localToken), nil
	}
}

// windowClient is the window's side of the Gateway's plain endpoints: the
// followed computer's, or this computer's own. Nil when there is none to
// reach, which the window answers as a computer without a Scratch folder.
func windowClient(followed *computer.Paired, gw *gateway.Gateway, localToken string) *gateway.Client {
	var socketURL string
	switch {
	case followed != nil:
		socketURL = followed.SocketURL()
	case gw != nil:
		socketURL = gw.DaemonSocketURL(localToken)
	default:
		return nil
	}
	c, err := gateway.NewClient(socketURL)
	if err != nil {
		log.Println("droi: gateway client:", err)
		return nil
	}
	return c
}
