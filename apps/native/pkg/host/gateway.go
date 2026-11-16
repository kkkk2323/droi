package host

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// SystemPromptAddition is Settings' System Prompt Addition, then the app's
// own (Config.AppendSystemPrompt), a blank line between them: what the
// Gateway adds to a Client's Session, and the Host to its own.
func (h *Host) SystemPromptAddition() string {
	var parts []string
	if p := h.Settings.Get().AppendSystemPrompt; p != nil && strings.TrimSpace(*p) != "" {
		parts = append(parts, *p)
	}
	if h.cfg.AppendSystemPrompt != nil {
		if p := h.cfg.AppendSystemPrompt(); strings.TrimSpace(p) != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}

// clientHiddenTools is Config.ClientHiddenTools, then Config.HiddenTools;
// nil from a droid that does not take MCP selectors and llm ids in
// disabledToolIds.
func (h *Host) clientHiddenTools() []string {
	if h.cfg.ClientHiddenTools == nil && h.cfg.HiddenTools == nil {
		return nil
	}
	if h.olderThanHiddenToolsDroid() {
		return nil
	}
	var ids []string
	if h.cfg.ClientHiddenTools != nil {
		ids = h.cfg.ClientHiddenTools()
	}
	return addNew(ids, h.HiddenTools())
}

// HiddenTools is Config.HiddenTools, which the Host's own Sessions leave out
// too; nil from a droid that does not take MCP selectors and llm ids in
// disabledToolIds.
func (h *Host) HiddenTools() []string {
	if h.cfg.HiddenTools == nil || h.olderThanHiddenToolsDroid() {
		return nil
	}
	return h.cfg.HiddenTools()
}

func (h *Host) olderThanHiddenToolsDroid() bool {
	// DroidVersion reads the droid on disk (Droi's droid when the Key Pool
	// is on), which differs from the running Daemon's only until it restarts.
	return olderThan(h.DroidVersion(), hiddenToolsDroid)
}

// addNew is ids with those of more it lacks.
func addNew(ids, more []string) []string {
	for _, id := range more {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// GatewayOptions is the Gateway of h's computer, as the app and `droi host`
// both run it. The caller sets what differs: Port, RemoteAccess, LocalToken
// and App.
func (h *Host) GatewayOptions(version string, moveToTrash func(string) error) gateway.Options {
	return gateway.Options{
		DaemonURL:          func() string { return h.Daemon.State().URL() },
		PairingToken:       func() string { return h.Settings.Get().PairingToken },
		Credential:         h.Credential,
		AppendSystemPrompt: h.SystemPromptAddition,
		DisabledTools:      h.clientHiddenTools,
		Version:            version,
		ComputerName:       gateway.ComputerName(),
		ComputerID:         h.Settings.Get().ComputerID,
		// Its refusal of a folder outside the Scratch folder answers 400.
		Scratch: &gateway.ScratchFolders{Root: h.ScratchFolder, MoveToTrash: moveToTrash},
		FindSessionFile: func(id string) string {
			return FindSessionTranscript(filepath.Join(h.SharedFactoryHome(), "sessions"), id)
		},
		SessionTimes: h.Times,
		ScriptModels: h.ScriptModelList,
		Relay: func() gateway.RelayConfig {
			s := h.Settings.Get()
			// relayUrl may hold the whole relay address, key and all.
			address, key := RelayAddressOf(deref(s.RelayURL))
			if key == nil {
				key = s.RelayKey
			}
			return gateway.RelayConfig{URL: deref(address), Key: deref(key), ID: s.RelayID}
		},
	}
}
