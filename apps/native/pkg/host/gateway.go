package host

import (
	"path/filepath"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// GatewayOptions is the Gateway of h's computer, as the app and `droi host`
// both run it. The caller sets what differs: Port, RemoteAccess, LocalToken
// and App.
func (h *Host) GatewayOptions(version string, moveToTrash func(string) error) gateway.Options {
	return gateway.Options{
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
		ComputerID:   h.Settings.Get().ComputerID,
		// Its refusal of a folder outside the Scratch folder answers 400.
		Scratch: &gateway.ScratchFolders{Root: h.ScratchFolder, MoveToTrash: moveToTrash},
		FindSessionFile: func(id string) string {
			return FindSessionTranscript(filepath.Join(h.SharedFactoryHome(), "sessions"), id)
		},
	}
}
