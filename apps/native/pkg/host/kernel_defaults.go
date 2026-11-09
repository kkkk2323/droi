package host

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SessionDefaults are what a new Kernel conversation starts with, as Settings → Session defaults shows them. The
// Kernel keeps no settings of its own, and droid's are droid's: these are Droi's.
type SessionDefaults struct {
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	AutonomyLevel   string `json:"autonomyLevel,omitempty"`
}

// KernelSessionDefaults are Settings' Session defaults. Until it has some, droid's (~/.factory/settings.json) are
// taken, once, so a Droi that stops running droid starts where it left off.
func (h *Host) KernelSessionDefaults() SessionDefaults {
	if d := h.Settings.Get().SessionDefaults; d != nil {
		return *d
	}
	h.seedDefaults.Do(func() {
		d := droidSessionDefaults(filepath.Join(h.SharedFactoryHome(), "settings.json"))
		_ = h.Settings.Update(func(st *Settings) {
			if st.SessionDefaults == nil {
				st.SessionDefaults = &d
			}
		})
	})
	if d := h.Settings.Get().SessionDefaults; d != nil {
		return *d
	}
	return SessionDefaults{}
}

// SetKernelSessionDefaults changes the Session defaults with fn and keeps them.
func (h *Host) SetKernelSessionDefaults(fn func(*SessionDefaults)) error {
	d := h.KernelSessionDefaults()
	fn(&d)
	return h.Settings.Update(func(st *Settings) { st.SessionDefaults = &d })
}

// droidSessionDefaults are the sessionDefaultSettings of droid's settings file, empty when it has none.
func droidSessionDefaults(file string) SessionDefaults {
	var f struct {
		SessionDefaultSettings SessionDefaults `json:"sessionDefaultSettings"`
	}
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f.SessionDefaultSettings
}
