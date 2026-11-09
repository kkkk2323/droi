package host

import (
	"encoding/json"
	"errors"
	"log"
	"slices"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/keypool"
)

// Model Sources (ADR 0044): the places the Kernel's models come from. The Host asks every source, gives the Kernel
// all their providers together, and gives it them again whenever one of them changes. A source is one more type with
// a load; the Kernel, the window and the picker need nothing new for it.

// Model Source ids, as ModelSourceStatus.ID names them.
const (
	SourceFactory = "factory"
	SourceMagpie  = "magpie"
)

// ModelSourceState is how a Model Source stands.
type ModelSourceState string

const (
	// SourceReady offers models.
	SourceReady ModelSourceState = "ready"
	// SourceOff is turned off in Settings.
	SourceOff ModelSourceState = "off"
	// SourceEmpty is on and works, but has nothing to run with: the Key Pool has no key, the gateway lists no model.
	SourceEmpty ModelSourceState = "empty"
	// SourceUnreachable is on and could not be reached; Err says why.
	SourceUnreachable ModelSourceState = "unreachable"
)

// ModelSourceStatus is how one Model Source stood when the Kernel was last given its models.
type ModelSourceStatus struct {
	ID    string
	State ModelSourceState
	// Models is how many models it gave the Kernel. An unreachable source may still give the ones it last had.
	Models int
	Err    string
}

// modelSource is one place the Kernel's models come from.
type modelSource interface {
	// load starts what the source needs and answers what it offers the Kernel now. It lets no request in: admit does
	// that, and the Host calls it only for a Kernel that is still the one the models are for.
	load() sourceLoad
}

type sourceLoad struct {
	providers []kernelProvider
	status    ModelSourceStatus
	// admit lets the requests that carry token in; nil when the source takes any.
	admit func(token string)
}

// modelSources are the Host's sources, in the order the picker lists their models.
func (h *Host) modelSources() []modelSource {
	return []modelSource{factorySource{h}, magpieSource{h}}
}

// KernelModelSources is how each Model Source stood when the running Kernel was last given its models; nil while
// no Kernel runs.
func (h *Host) KernelModelSources() []ModelSourceStatus {
	k := &h.kernel
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.sup == nil {
		return nil
	}
	return slices.Clone(k.sources)
}

// loadModelSources loads every source for the Kernel of run runs whose requests carry token, and keeps what it
// gives it. A launch takes token as its own; a change needs the Kernel to still hold it. ok is false when that Kernel
// stopped, or was started again, meanwhile: nothing is let in for it. changed says the providers are not the ones
// the Kernel was last given.
func (h *Host) loadModelSources(token string, runs int, launch bool) (providers json.RawMessage, changed, ok bool) {
	all := []kernelProvider{}
	var statuses []ModelSourceStatus
	var admits []func(string)
	for _, s := range h.modelSources() {
		l := s.load()
		all = append(all, l.providers...)
		statuses = append(statuses, l.status)
		if l.admit != nil {
			admits = append(admits, l.admit)
		}
	}
	b, err := json.Marshal(all)
	if err != nil {
		return nil, false, false
	}
	k := &h.kernel
	k.mu.Lock()
	if k.sup == nil || k.runs != runs || !launch && k.llmToken != token {
		k.mu.Unlock()
		return nil, false, false
	}
	if launch {
		k.llmToken = token
	}
	changed = launch || k.given != string(b)
	moved := !slices.Equal(k.sources, statuses)
	k.given, k.sources = string(b), statuses
	for _, admit := range admits {
		admit(token)
	}
	k.mu.Unlock()
	if moved {
		h.changed()
	}
	return b, changed, true
}

// kernelModelsChanged gives the running Kernel its models again when a source changed under it: the Key Pool's proxy
// stopped or moved, its first key arrived, Magpie was turned on or off or its list changed. The Kernel keeps running,
// and so do its runs; only a Kernel that cannot take the change is started again.
func (h *Host) kernelModelsChanged() {
	k := &h.kernel
	k.configuring.Lock()
	defer k.configuring.Unlock()
	k.mu.Lock()
	sup, token, runs := k.sup, k.llmToken, k.runs
	k.mu.Unlock()
	// A Kernel still launching loads the sources itself.
	if sup == nil || token == "" {
		return
	}
	providers, changed, ok := h.loadModelSources(token, runs, false)
	if !ok || !changed {
		return
	}
	err := h.configureKernel(kernel.HarnessConfigureParams{Providers: providers})
	if err == nil {
		log.Println("droi: kernel: a model source changed; the Kernel has its models")
		return
	}
	log.Println("droi: kernel: a model source changed, and the Kernel did not take its models; starting it again:", err)
	h.restartKernel(sup, runs)
}

// factorySource is Factory's models through the Key Pool's proxy: the requests carry the Kernel's token, and the
// proxy stamps a key of the pool on them (ADR 0039). FACTORY_API_BASE_URL is the Daemon's, and plays no part.
type factorySource struct{ h *Host }

func (s factorySource) load() sourceLoad {
	pool := s.h.KeyPool()
	pool.Configure(s.h.keyPoolUpstream(), s.h.FactoryHome())
	base, err := pool.Start()
	st := ModelSourceStatus{ID: SourceFactory}
	switch {
	case errors.Is(err, keypool.ErrNoKeys):
		st.State = SourceEmpty
		return sourceLoad{status: st}
	case err != nil:
		st.State, st.Err = SourceUnreachable, err.Error()
		log.Println("droi: kernel: no Factory models:", err)
		return sourceLoad{status: st}
	}
	providers := factoryProviders(base)
	st.State = SourceReady
	for _, p := range providers {
		st.Models += len(p.Models)
	}
	return sourceLoad{providers: providers, status: st, admit: pool.SetKernelToken}
}

// magpieSource is a Magpie gateway's models, when Settings turns it on: the Kernel speaks OpenAI Chat Completions
// to it straight, on this computer, and the gateway takes them to each model's provider.
type magpieSource struct{ h *Host }

func (s magpieSource) load() sourceLoad {
	st := ModelSourceStatus{ID: SourceMagpie}
	if !s.h.Settings.Get().MagpieEnabled {
		st.State = SourceOff
		return sourceLoad{status: st}
	}
	cat := s.h.magpieModels()
	var providers []kernelProvider
	if p, ok := magpieProvider(cat); ok {
		providers = []kernelProvider{p}
		st.Models = len(p.Models)
	}
	switch {
	case cat.Err != "":
		// The models read before stay: a Magpie that restarts does not empty the picker.
		st.State, st.Err = SourceUnreachable, cat.Err
	case st.Models == 0:
		st.State = SourceEmpty
	default:
		st.State = SourceReady
	}
	return sourceLoad{providers: providers, status: st}
}
