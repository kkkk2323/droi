package host

import (
	"log"
	"maps"
	"slices"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// Two of the Kernel's settings that are Pi's rather than the Daemon's: codemode's mode, and the context window of
// each model, which is also where the Kernel compacts it. Both reach a running Kernel without a restart.

// KernelCodemode is codemode's mode as Settings has it: kernel.CodemodeOnly, else kernel.CodemodeOn.
func (h *Host) KernelCodemode() string {
	if h.Settings.Get().CodemodeMode == kernel.CodemodeOnly {
		return kernel.CodemodeOnly
	}
	return kernel.CodemodeOn
}

// SetKernelCodemode stores codemode's mode and gives it to the running Kernel.
func (h *Host) SetKernelCodemode(mode string) error {
	if mode != kernel.CodemodeOnly {
		mode = ""
	}
	if err := h.Settings.Update(func(s *Settings) { s.CodemodeMode = mode }); err != nil {
		return err
	}
	h.Changed()
	return nil
}

// kernelCodemodeChanged gives the running Kernel codemode's mode when it changed since the Kernel last had it: every
// conversation has it from its next request. A Kernel that cannot take it is started again.
func (h *Host) kernelCodemodeChanged() {
	k := &h.kernel
	k.configuring.Lock()
	defer k.configuring.Unlock()
	mode := h.KernelCodemode()
	k.mu.Lock()
	sup := k.sup
	if sup == nil || k.codemode == mode {
		k.mu.Unlock()
		return
	}
	k.codemode = mode
	runs := k.runs
	k.mu.Unlock()
	err := h.configureKernel(kernel.HarnessConfigureParams{Codemode: &kernel.CodemodeSettings{Mode: mode}})
	if err == nil {
		log.Println("droi: kernel: codemode's mode changed in the running Kernel:", mode)
		return
	}
	log.Println("droi: kernel: codemode's mode changed; starting the Kernel again:", err)
	h.restartKernel(sup, runs)
}

// ModelKey is how ModelContextWindows names a Kernel model: its provider and its id.
func ModelKey(provider, id string) string { return provider + "/" + id }

// KernelModelContext is one of the Kernel's models with the context window its Model Source lists and the one
// Settings sets in its place, 0 for none.
type KernelModelContext struct {
	Provider, ID, Name string
	Listed, Set        int
}

// KernelModelContexts are the models the running Kernel was last given, in the picker's order, with their context
// windows; nil while no Kernel runs.
func (h *Host) KernelModelContexts() []KernelModelContext {
	k := &h.kernel
	k.mu.Lock()
	listed := slices.Clone(k.listed)
	running := k.sup != nil
	k.mu.Unlock()
	if !running {
		return nil
	}
	set := h.Settings.Get().ModelContextWindows
	for i := range listed {
		listed[i].Set = set[ModelKey(listed[i].Provider, listed[i].ID)]
	}
	return listed
}

// SetModelContextWindow sets the context window of a Kernel model, in tokens; 0 gives it back the one its Model
// Source lists. The running Kernel has it from each conversation's next request.
func (h *Host) SetModelContextWindow(provider, id string, tokens int) error {
	key := ModelKey(provider, id)
	if err := h.Settings.Update(func(s *Settings) {
		next := maps.Clone(s.ModelContextWindows)
		if next == nil {
			next = map[string]int{}
		}
		if tokens > 0 {
			next[key] = tokens
		} else {
			delete(next, key)
		}
		if len(next) == 0 {
			next = nil
		}
		s.ModelContextWindows = next
	}); err != nil {
		return err
	}
	h.Changed()
	return nil
}

// withContextWindows is providers with each model's context window as set, and the models as their sources list them.
// A model's output limit stays below a window set smaller than it, as pi-ai expects of a catalog.
func withContextWindows(providers []kernelProvider, set map[string]int) ([]kernelProvider, []KernelModelContext) {
	var listed []KernelModelContext
	for i := range providers {
		p := &providers[i]
		models := slices.Clone(p.Models)
		for j := range models {
			m := &models[j]
			listed = append(listed, KernelModelContext{Provider: p.ID, ID: m.ID, Name: m.Name, Listed: m.ContextWindow})
			if tokens := set[ModelKey(p.ID, m.ID)]; tokens > 0 {
				m.ContextWindow = tokens
				if m.MaxTokens > tokens {
					m.MaxTokens = tokens
				}
			}
		}
		p.Models = models
	}
	return providers, listed
}
