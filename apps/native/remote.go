package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// startGateway starts the Gateway that paired phones and this window connect
// through. The window presents localToken, which a Pairing Token reset or
// turning Remote Access off does not revoke.
func startGateway(h *host.Host, version, localToken string, moveToTrash func(string) error) (*gateway.Gateway, error) {
	s := h.Settings.Get()
	return gateway.Start(gateway.Options{
		Port:         gateway.PreferredPort,
		RemoteAccess: s.RemoteAccess,
		DaemonURL:    func() string { return h.Daemon.State().URL() },
		PairingToken: func() string { return h.Settings.Get().PairingToken },
		LocalToken:   func() string { return localToken },
		Credential:   h.Credential,
		AppendSystemPrompt: func() string {
			if p := h.Settings.Get().AppendSystemPrompt; p != nil {
				return *p
			}
			return ""
		},
		Version:      version,
		ComputerName: gateway.ComputerName(),
		ComputerID:   s.ComputerID,
		// Its refusal of a folder outside the Scratch folder answers 400.
		Scratch: &gateway.ScratchFolders{Root: h.ScratchFolder, MoveToTrash: moveToTrash},
		FindSessionFile: func(id string) string {
			return host.FindSessionTranscript(filepath.Join(h.SharedFactoryHome(), "sessions"), id)
		},
	})
}

// gatewayURL is the controller's ResolveURL: the Gateway's Daemon socket,
// once the Daemon runs, never the Daemon's own address. A Gateway that could
// not listen (gwErr) is an error the window shows.
func gatewayURL(gw *gateway.Gateway, gwErr error, localToken string, daemonUp func(context.Context) (string, error)) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if gw == nil {
			return "", fmt.Errorf("Droi's Gateway could not listen, so the window cannot reach the Daemon: %w", gwErr)
		}
		if _, err := daemonUp(ctx); err != nil {
			return "", err
		}
		return gw.DaemonSocketURL(localToken), nil
	}
}
