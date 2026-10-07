package main

import (
	"log"
	"path/filepath"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// startGateway starts the Gateway that paired phones connect through; this
// window talks to the Daemon directly, so it holds no Local Token. Nil when
// it cannot listen, which leaves the app without Remote Access.
func startGateway(h *host.Host, version string, moveToTrash func(string) error) *gateway.Gateway {
	s := h.Settings.Get()
	gw, err := gateway.Start(gateway.Options{
		Port:         gateway.PreferredPort,
		RemoteAccess: s.RemoteAccess,
		DaemonURL:    func() string { return h.Daemon.State().URL() },
		PairingToken: func() string { return h.Settings.Get().PairingToken },
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
			return host.FindSessionTranscript(filepath.Join(h.FactoryHome(), "sessions"), id)
		},
	})
	if err != nil {
		log.Println("droi: gateway:", err)
		return nil
	}
	return gw
}
