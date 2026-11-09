package host

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/llmproxy/factory"
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
	// kernelMagpieKeyEnv is the Magpie provider's apiKeyEnv. A launch always sets it, so Magpie turned on later
	// reaches a running Kernel with its key already there.
	kernelMagpieKeyEnv = "DROI_KERNEL_MAGPIE_KEY"
)

// kernelHost is the Host's Kernel: the Supervisor, the token the proxy knows the Kernel by, and who hears its state.
type kernelHost struct {
	mu  sync.Mutex
	sup *KernelSupervisor
	// runs counts startKernel and stopKernel, so a launch that was preparing its environment while the Kernel was
	// stopped does not hand the proxy a token again.
	runs int
	// llmToken is the token of the latest launch; each launch has its own.
	llmToken string
	// given is the providers the Kernel was last given, as JSON, and sources how each Model Source stood then.
	given   string
	sources []ModelSourceStatus
	// memoryOn is whether the latest launch was given the Memory folder.
	memoryOn bool
	// section is the prompt section the Kernel has, as the latest launch or harness.configure gave it.
	section string
	// link is the Host's own connection to the Kernel, over which a change of its settings reaches it
	// (configureKernel); unhearLink stops it hearing the Supervisor.
	link *KernelLink
	// configuring is held from reading a setting to the Kernel having it, so two changes reach it in their order.
	configuring sync.Mutex
	unhearLink  func()
	// starting is closed once the launch startKernel began off the caller's goroutine has started the Supervisor,
	// or decided not to; stopKernel waits for it, so a Supervisor it stopped is never started after.
	starting  chan struct{}
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
	k.runs++
	k.sup = &KernelSupervisor{
		Binary:  h.kernelBinary(),
		DataDir: filepath.Join(h.cfg.UserData, "kernel"),
		UserDir: brand.UserFolder(h.cfg.Home),
		Env:     h.kernelEnv,
		OnState: h.kernelChanged,
	}
	sup, runs := k.sup, k.runs
	starting := make(chan struct{})
	k.starting = starting
	k.mu.Unlock()
	h.linkKernel()
	// Off the caller's goroutine, the window's at launch: the first copy of a large skills folder takes a while.
	go func() {
		defer close(starting)
		// Before the Kernel starts: a Kernel that made an empty skills folder first would read as the user's choice.
		if copied, err := h.CopyFactoryHome(); err != nil {
			log.Println("droi: kernel: copying ~/.factory:", err)
		} else if !copied.Empty() || len(copied.Skipped) > 0 {
			log.Printf("droi: kernel: copied from ~/.factory: %d skills, mcp.json %v, AGENTS.md %v; left out %v",
				copied.Skills, copied.MCP, copied.Agents, copied.Skipped)
		}
		k.mu.Lock()
		current := k.sup == sup && k.runs == runs
		k.mu.Unlock()
		if current {
			sup.Start()
		}
	}()
}

// linkKernel gives the Host a connection of its own to the Kernel, for configureKernel. Every notification reaches
// every connection; this one reads them and lets them go, so its queue never fills and closes it.
func (h *Host) linkKernel() {
	link := NewKernelLink(KernelLinkOptions{
		Client: kernel.ClientHost,
		Token:  h.KernelToken,
		Log:    func(s string) { log.Println("droi: kernel: host link:", s) },
		OnClient: func(c *kernel.Client, _ int) {
			go func() {
				for range c.Notifications() {
				}
			}()
		},
	})
	unhear := h.OnKernelState(link.Use)
	k := &h.kernel
	k.mu.Lock()
	if k.link != nil || k.sup == nil {
		k.mu.Unlock()
		unhear()
		link.Close()
		return
	}
	k.link, k.unhearLink = link, unhear
	k.mu.Unlock()
}

// stopKernel stops the Kernel and lets go of the token it was known by, and of the Key Pool's proxy when droid does
// not send through it.
func (h *Host) stopKernel() {
	k := &h.kernel
	k.mu.Lock()
	sup := k.sup
	k.sup = nil
	k.runs++
	k.llmToken, k.given, k.sources = "", "", nil
	h.KeyPool().SetKernelToken("")
	link, unhear := k.link, k.unhearLink
	k.link, k.unhearLink = nil, nil
	starting := k.starting
	k.starting = nil
	k.mu.Unlock()
	if link != nil {
		unhear()
		link.Close()
	}
	if sup == nil {
		return
	}
	if starting != nil {
		<-starting
	}
	// A change under way ends with the link closed under it; its fallback restart then sees runs moved on. Waiting
	// for it keeps a restart that already began from launching after the Stop below.
	k.configuring.Lock()
	k.configuring.Unlock()
	sup.Stop()
	sup.Close()
	if !h.usesDroiDroid() {
		h.KeyPool().Stop()
	}
}

// kernelRunning reports whether the Host runs a Kernel now.
func (h *Host) kernelRunning() bool {
	k := &h.kernel
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.sup != nil
}

// releaseKeyPool stops the Key Pool's proxy for a Daemon whose droid does not send through it, unless the Kernel's
// models do: the Kernel was told the proxy's address at launch, and would reach nothing.
func (h *Host) releaseKeyPool() {
	if h.kernelRunning() {
		return
	}
	h.KeyPool().Stop()
}

// kernelAgent is what the Kernel is given for every conversation: the app's Agent (Config.KernelAgent), its section
// after Settings' System Prompt Addition. An app that gives no section of its own has its SystemPrompt there
// instead, as the Daemon's Sessions do; one that gives both has it once.
func (h *Host) kernelAgent() kernel.AppAgent {
	var a kernel.AppAgent
	if h.cfg.KernelAgent != nil {
		a = h.cfg.KernelAgent(h)
	}
	var parts []string
	if p := h.Settings.Get().AppendSystemPrompt; p != nil && strings.TrimSpace(*p) != "" {
		parts = append(parts, *p)
	}
	app := strings.TrimSpace(a.Section)
	if app == "" && h.cfg.AppendSystemPrompt != nil {
		app = strings.TrimSpace(h.cfg.AppendSystemPrompt())
	}
	if app != "" {
		parts = append(parts, app)
	}
	a.Section = strings.Join(parts, "\n\n")
	return a
}

// kernelSectionChanged gives the running Kernel the section as Settings and the app have it now, when it changed
// since: each conversation has it from its next turn. A Kernel that cannot take it is started again.
func (h *Host) kernelSectionChanged() {
	k := &h.kernel
	k.configuring.Lock()
	defer k.configuring.Unlock()
	section := h.kernelAgent().Section
	k.mu.Lock()
	sup := k.sup
	if sup == nil || k.section == section {
		k.mu.Unlock()
		return
	}
	k.section = section
	runs := k.runs
	k.mu.Unlock()
	change := kernel.None[string]()
	if section != "" {
		change = kernel.Some(section)
	}
	err := h.configureKernel(kernel.HarnessConfigureParams{Section: change})
	if err == nil {
		log.Println("droi: kernel: the prompt section changed in the running Kernel")
		return
	}
	log.Println("droi: kernel: the prompt section changed; starting the Kernel again:", err)
	h.restartKernel(sup, runs)
}

// kernelEnv is what a Kernel launch gets: the models of every Model Source, and a token of this launch's that the
// Factory requests carry. With no source able to give any (no key yet, Magpie off) the Kernel starts all the same,
// its conversations stay readable, and it gets models once a source has some (kernelModelsChanged).
func (h *Host) kernelEnv() []string {
	k := &h.kernel
	k.mu.Lock()
	runs := k.runs
	k.mu.Unlock()
	token, err := kernel.NewToken()
	if err != nil {
		log.Println("droi: kernel: no token for its model requests:", err)
		return nil
	}
	env := []string{kernelLLMTokenEnv + "=" + token, kernelMagpieKeyEnv + "=" + magpieKey}
	memoryOn := h.Settings.Get().MemoryEnabled
	if memoryOn {
		env = append(env, kernel.MemoryDirEnv+"="+h.MemoryDir())
	}
	agent := h.kernelAgent()
	if !agent.Empty() {
		if b, err := json.Marshal(agent); err == nil {
			env = append(env, kernel.AppAgentEnv+"="+string(b))
		}
	}
	k.mu.Lock()
	k.memoryOn = memoryOn
	k.section = agent.Section
	k.mu.Unlock()
	// Stopped, or started again, while this launch was preparing: the proxy does not take its token.
	providers, _, ok := h.loadModelSources(token, runs, true)
	// The Kernel refuses an empty list in its environment; no providers is no variable.
	if !ok || string(providers) == "[]" {
		return env
	}
	return append(env, kernelProvidersEnv+"="+string(providers))
}

// kernelMemoryChanged turns Memory on or off in the running Kernel when it was turned on or off since: each
// conversation has the Memory tools and its Memory, or not, from its next turn. A Kernel that cannot take the change
// is started again, as a launch is what gives one the Memory folder; a Kernel left as it was would keep its Memory
// tools, and write Memory Notes nothing folds in, after Memory was turned off.
func (h *Host) kernelMemoryChanged() {
	k := &h.kernel
	k.configuring.Lock()
	defer k.configuring.Unlock()
	on := h.Settings.Get().MemoryEnabled
	k.mu.Lock()
	sup := k.sup
	if sup == nil || k.memoryOn == on {
		k.mu.Unlock()
		return
	}
	k.memoryOn = on
	runs := k.runs
	k.mu.Unlock()
	dir := kernel.None[string]()
	if on {
		dir = kernel.Some(h.MemoryDir())
	}
	err := h.configureKernel(kernel.HarnessConfigureParams{MemoryDir: dir})
	if err == nil {
		log.Println("droi: kernel: Memory was turned on or off in the running Kernel")
		return
	}
	log.Println("droi: kernel: Memory was turned on or off; starting the Kernel again:", err)
	h.restartKernel(sup, runs)
}

// restartKernel starts sup's Kernel again, unless it was stopped, or started anew, since run runs: a change whose
// call failed because the Kernel was stopping must not launch a Kernel nobody owns. The caller holds configuring,
// which stopKernel waits for, so the check and the restart are one step to it.
func (h *Host) restartKernel(sup *KernelSupervisor, runs int) {
	k := &h.kernel
	k.mu.Lock()
	current := k.sup == sup && k.runs == runs
	k.mu.Unlock()
	if current {
		sup.Restart()
	}
}

// kernelConfigureTimeout bounds harness.configure, which changes what the Kernel holds and touches no run.
const kernelConfigureTimeout = 10 * time.Second

// configureKernel gives the running Kernel a change of the Host's settings, over the Host's own connection.
func (h *Host) configureKernel(p kernel.HarnessConfigureParams) error {
	k := &h.kernel
	k.mu.Lock()
	link := k.link
	k.mu.Unlock()
	if link == nil {
		return errors.New("no Kernel runs")
	}
	c, _ := link.Current()
	if c == nil {
		return errors.New("the Host is not connected to the Kernel yet")
	}
	ctx, cancel := context.WithTimeout(context.Background(), kernelConfigureTimeout)
	defer cancel()
	return c.HarnessConfigure(ctx, p)
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

// KernelState is how the Kernel stood when the Supervisor last said; the zero state while none was started.
func (h *Host) KernelState() KernelState {
	k := &h.kernel
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.sup == nil {
		return KernelState{}
	}
	return k.last
}

// RestartKernel starts the running Kernel again; its runs stop and its conversations stay.
func (h *Host) RestartKernel() {
	k := &h.kernel
	k.mu.Lock()
	sup := k.sup
	k.mu.Unlock()
	if sup != nil {
		sup.Restart()
	}
}

// KernelURL is the running Kernel's /rpc address, "" while none runs.
func (h *Host) KernelURL() string {
	k := &h.kernel
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.sup == nil {
		return ""
	}
	return k.last.RPCURL()
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
	// ThinkingLevelMap is pi-ai's: each of pi's levels as the effort Factory takes, or null when it takes none.
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap,omitempty"`
	Compat           *kernelCompat      `json:"compat,omitempty"`
}

type kernelCompat struct {
	ForceAdaptiveThinking bool `json:"forceAdaptiveThinking"`
}

// piThinkingLevels are pi-ai's thinking levels. pi-ai sends a level its model's map leaves out as another: xhigh
// and max as high, and off as effort "none" on Responses, which a model without it refuses.
var piThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// factoryProviders are Factory's models on the proxy at base, one provider per wire. A model droid's registry gives no
// limits stays out: the Kernel needs them.
func factoryProviders(base string) []kernelProvider {
	base = strings.TrimRight(base, "/")
	wires := []struct {
		wire factory.Wire
		p    kernelProvider
	}{
		{factory.WireAnthropic, kernelProvider{ID: "factory-anthropic", Name: "Factory", BaseURL: base + "/api/llm/a", API: "anthropic-messages"}},
		{factory.WireResponses, kernelProvider{ID: "factory-openai", Name: "Factory", BaseURL: base + "/api/llm/o/v1", API: "openai-responses"}},
		{factory.WireChat, kernelProvider{ID: "factory-chat", Name: "Factory", BaseURL: base + "/api/llm/o/v1", API: "openai-completions"}},
		// Google's client sends to <base>/models/<model>:streamGenerateContent; the proxy moves it to /generate.
		{factory.WireGemini, kernelProvider{ID: "factory-google", Name: "Factory", BaseURL: base + "/api/llm/g/v1", API: "google-generative-ai"}},
	}
	out := []kernelProvider{}
	for _, w := range wires {
		p := w.p
		p.APIKeyEnv = kernelLLMTokenEnv
		for _, m := range factory.Models() {
			if m.Wire != w.wire || m.Context <= 0 || m.Output <= 0 {
				continue
			}
			p.Models = append(p.Models, kernelModelOf(m))
		}
		// The Kernel refuses a provider without models, and would not start.
		if len(p.Models) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// kernelModelOf is a Factory model in the Kernel's words: Factory's effort "none" is pi's thinking level "off".
func kernelModelOf(m factory.Model) kernelModel {
	km := kernelModel{ID: m.ID, Name: m.Name, ContextWindow: m.Context, MaxTokens: m.Output, Input: []string{"text"}}
	if m.Images {
		km.Input = append(km.Input, "image")
	}
	km.thinkWith(m.Efforts)
	// Every Claude model Factory offers the Kernel is 4.6 or later, which thinks adaptively: pi-ai budgets Claude's
	// thinking unless told so, and Haiku 5.5 and Opus 4.7 and later refuse a budget with a 400.
	if m.Wire == factory.WireAnthropic && strings.HasPrefix(m.ID, "claude-") && !strings.HasPrefix(m.ID, "claude-haiku-4-5") {
		km.Compat = &kernelCompat{ForceAdaptiveThinking: true}
	}
	return km
}

// thinkWith gives the model the efforts its provider takes, as pi's thinking levels: effort "none" is pi's "off".
func (km *kernelModel) thinkWith(efforts []string) {
	for _, e := range efforts {
		if e == "none" {
			km.Thinking = append(km.Thinking, "off")
			continue
		}
		km.Thinking = append(km.Thinking, e)
		km.Reasoning = true
	}
	if !km.Reasoning {
		return
	}
	km.ThinkingLevelMap = map[string]*string{}
	for _, level := range piThinkingLevels {
		effort := level
		if level == "off" {
			effort = "none"
		}
		if slices.Contains(efforts, effort) {
			km.ThinkingLevelMap[level] = &effort
		} else {
			km.ThinkingLevelMap[level] = nil
		}
	}
}
