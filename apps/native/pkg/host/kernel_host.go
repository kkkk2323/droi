package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/factory"
)

// The Host's Kernel (ADR 0038): one sidecar beside the Daemon, for every window of this computer. Its model requests
// go to the Key Pool's proxy with a token of the Host's own in place of a key, and the proxy picks the key (Q15).

const (
	// KernelEnv turns the Kernel on: "1" starts it, and the window's conversations are its. Until the Kernel does
	// all the Daemon does, a person asks for it.
	KernelEnv = "DROI_KERNEL"
	// KernelBinEnv names the Kernel executable, in place of the one beside the app.
	KernelBinEnv = "DROI_KERNEL_BIN"
	// kernelLLMTokenEnv is where the Kernel reads the token its model requests carry: its providers' apiKeyEnv.
	kernelLLMTokenEnv = "DROI_KERNEL_LLM_TOKEN"
	// kernelProvidersEnv carries the providers, as the Kernel reads them (apps/kernel/src/config.ts).
	kernelProvidersEnv = "DROI_KERNEL_PROVIDERS"
)

// kernelHost is the Host's Kernel: the Supervisor, the token the proxy knows the Kernel by, and who hears its state.
type kernelHost struct {
	mu        sync.Mutex
	sup       *KernelSupervisor
	llmToken  string
	last      KernelState
	listeners map[int]func(KernelState)
	next      int
}

// KernelEnabled reports whether this Host runs a Kernel: a person asked for it, and there is one to run.
func (h *Host) KernelEnabled() bool {
	return h.env(KernelEnv) == "1" && h.kernelBinary() != ""
}

func (h *Host) env(name string) string {
	if h.cfg.Env == nil {
		return ""
	}
	return h.cfg.Env(name)
}

// kernelBinary is the Kernel executable: KernelBinEnv's, or the one the app carries beside its own executable (in a
// macOS bundle, in its Resources). "" when there is none.
func (h *Host) kernelBinary() string {
	name := "kernel"
	if runtime.GOOS == "windows" {
		name = "kernel.exe"
	}
	candidates := []string{h.env(KernelBinEnv)}
	if exe := h.cfg.Executable; exe != "" {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, name), filepath.Join(dir, "..", "Resources", name))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// startKernel starts the Kernel, once.
func (h *Host) startKernel() {
	k := &h.kernel
	k.mu.Lock()
	if k.sup != nil {
		k.mu.Unlock()
		return
	}
	token, err := kernel.NewToken()
	if err != nil {
		k.mu.Unlock()
		return
	}
	k.llmToken = token
	k.sup = &KernelSupervisor{
		Binary:  h.kernelBinary(),
		DataDir: filepath.Join(h.cfg.UserData, "kernel"),
		Env:     h.kernelEnv,
		OnState: h.kernelChanged,
	}
	sup := k.sup
	k.mu.Unlock()
	sup.Start()
}

// stopKernel stops the Kernel and lets go of the token it was known by.
func (h *Host) stopKernel() {
	k := &h.kernel
	k.mu.Lock()
	sup := k.sup
	k.sup = nil
	k.mu.Unlock()
	if sup == nil {
		return
	}
	sup.Stop()
	sup.Close()
	h.KeyPool().SetKernelToken("")
}

// kernelEnv is what a Kernel launch gets: Factory's models on the Key Pool's proxy, and the token their requests
// carry. A pool that cannot start (no key yet) gives the Kernel no models; it starts all the same, and its
// conversations stay readable.
func (h *Host) kernelEnv() []string {
	k := &h.kernel
	k.mu.Lock()
	token := k.llmToken
	k.mu.Unlock()
	env := []string{kernelLLMTokenEnv + "=" + token}
	pool := h.KeyPool()
	if h.baseURLConflict() != "" {
		return env
	}
	pool.Configure(h.keyPoolUpstream(), h.FactoryHome())
	base, err := pool.Start()
	if err != nil {
		return env
	}
	pool.SetKernelToken(token)
	return append(env, kernelProvidersEnv+"="+kernelProviders(base))
}

// kernelChanged hears the Supervisor's state and tells whoever listens.
func (h *Host) kernelChanged(st KernelState) {
	k := &h.kernel
	k.mu.Lock()
	k.last = st
	fns := make([]func(KernelState), 0, len(k.listeners))
	for _, fn := range k.listeners {
		fns = append(fns, fn)
	}
	k.mu.Unlock()
	for _, fn := range fns {
		fn(st)
	}
	h.changed()
}

// OnKernelState hears the Kernel's state as it settles, starting with the state now when there is one. A window's
// KernelLink takes it (link.Use).
func (h *Host) OnKernelState(fn func(KernelState)) (unsubscribe func()) {
	k := &h.kernel
	k.mu.Lock()
	if k.listeners == nil {
		k.listeners = map[int]func(KernelState){}
	}
	id := k.next
	k.next++
	k.listeners[id] = fn
	last := k.last
	k.mu.Unlock()
	if last.Status != "" {
		fn(last)
	}
	return func() {
		k.mu.Lock()
		delete(k.listeners, id)
		k.mu.Unlock()
	}
}

// KernelToken is the running Kernel's connection token, which a KernelLink dials with; "" when none runs.
func (h *Host) KernelToken() string {
	k := &h.kernel
	k.mu.Lock()
	sup := k.sup
	k.mu.Unlock()
	if sup == nil {
		return ""
	}
	return sup.Token()
}

// kernelProvider and kernelModel are a provider of the Kernel's config, as apps/kernel/src/config.ts reads it.
type kernelProvider struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	BaseURL   string        `json:"baseUrl"`
	API       string        `json:"api"`
	APIKeyEnv string        `json:"apiKeyEnv"`
	Models    []kernelModel `json:"models"`
}

type kernelModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
	Input         []string `json:"input"`
	Reasoning     bool     `json:"reasoning"`
	Thinking      []string `json:"thinking"`
}

// kernelProviders are Factory's models on the proxy at base, one provider per wire, as JSON. Gemini's models wait for
// their request shape (M4 4B/4C), and a model droid's registry gives no limits stays out: the Kernel needs them.
func kernelProviders(base string) string {
	base = strings.TrimRight(base, "/")
	wires := []struct {
		wire factory.Wire
		p    kernelProvider
	}{
		{factory.WireAnthropic, kernelProvider{ID: "factory-anthropic", Name: "Factory", BaseURL: base + "/api/llm/a", API: "anthropic-messages"}},
		{factory.WireResponses, kernelProvider{ID: "factory-openai", Name: "Factory", BaseURL: base + "/api/llm/o/v1", API: "openai-responses"}},
		{factory.WireChat, kernelProvider{ID: "factory-chat", Name: "Factory", BaseURL: base + "/api/llm/o/v1", API: "openai-completions"}},
	}
	var out []kernelProvider
	for _, w := range wires {
		p := w.p
		p.APIKeyEnv = kernelLLMTokenEnv
		for _, m := range factory.Models() {
			if m.Wire != w.wire || m.Context <= 0 || m.Output <= 0 {
				continue
			}
			p.Models = append(p.Models, kernelModelOf(m))
		}
		out = append(out, p)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// kernelModelOf is a Factory model in the Kernel's words: Factory's effort "none" is pi's thinking level "off".
func kernelModelOf(m factory.Model) kernelModel {
	km := kernelModel{ID: m.ID, Name: m.Name, ContextWindow: m.Context, MaxTokens: m.Output, Input: []string{"text"}}
	if m.Images {
		km.Input = append(km.Input, "image")
	}
	for _, e := range m.Efforts {
		if e == "none" {
			km.Thinking = append(km.Thinking, "off")
			continue
		}
		km.Thinking = append(km.Thinking, e)
		km.Reasoning = true
	}
	return km
}
