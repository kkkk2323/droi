package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMagpie is enough of a Magpie gateway for the Kernel: its model list and a streamed chat completion. While down
// it answers nothing but 502, as a port with no gateway behind it would fail.
type fakeMagpie struct {
	mu     sync.Mutex
	bodies []string
	down   atomic.Bool
	*httptest.Server
}

const fakeMagpieModels = `{"object":"list","data":[
	{"id":"zcode/GLM-5.3-Flash","display_name":"GLM-5.3-Flash","context_window":1000000,"max_output_tokens":128000,
	 "modalities":{"input":["text","image"]},"reasoning":true,"reasoning_levels":["low","high","max"]},
	{"id":"grok/grok-4.7@500k","display_name":"Grok 4.7 @ 500k","context_length":500000,
	 "modalities":{"input":["text"]},"reasoning":true,"reasoning_levels":["none","low","medium","high","xhigh"]}
]}`

func newFakeMagpie(t *testing.T) *fakeMagpie {
	t.Helper()
	m := &fakeMagpie{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+magpieKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, fakeMagpieModels)
		case "/v1/chat/completions":
			body, _ := io.ReadAll(r.Body)
			m.mu.Lock()
			m.bodies = append(m.bodies, string(body))
			m.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			for _, chunk := range []string{
				`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello from Magpie"},"finish_reason":null}]}`,
				`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
			} {
				io.WriteString(w, "data: "+chunk+"\n\n")
			}
			io.WriteString(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.Close)
	return m
}

// bodiesSeen are the chat completions the gateway was asked for.
func (m *fakeMagpie) bodiesSeen() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

// useMagpie points the Host at m, with Magpie on or off.
func useMagpie(t *testing.T, h *Host, m *fakeMagpie, on bool) {
	t.Helper()
	if err := h.Settings.Update(func(st *Settings) {
		st.MagpieEnabled = on
		st.MagpieBaseURL = OptString(m.URL)
	}); err != nil {
		t.Fatal(err)
	}
}

// sourceState is how the running Kernel's Model Source id stood when it was last given its models.
func sourceState(h *Host, id string) ModelSourceState {
	for _, s := range h.KernelModelSources() {
		if s.ID == id {
			return s.State
		}
	}
	return ""
}

// A Magpie model is the gateway's model on its OpenAI endpoint, under the gateway's own id, with the window and the
// efforts the gateway lists: effort "none" is pi's "off", and a model that lists none takes no thinking level.
func TestMagpiesModelsAreTheKernelsMagpieProvider(t *testing.T) {
	var list struct{ Data []MagpieModel }
	if err := json.Unmarshal([]byte(fakeMagpieModels), &list); err != nil {
		t.Fatal(err)
	}
	list.Data = append(list.Data, MagpieModel{ID: "plain/model"}, MagpieModel{ID: "  "})
	p, ok := magpieProvider(MagpieCatalog{BaseURL: "http://127.0.0.1:3425/", Models: list.Data})
	if !ok {
		t.Fatal("no provider")
	}
	if p.ID != "magpie" || p.API != "openai-completions" || p.BaseURL != "http://127.0.0.1:3425/v1" || p.APIKeyEnv != kernelMagpieKeyEnv {
		t.Fatalf("provider %+v", p)
	}
	if len(p.Models) != 3 {
		t.Fatalf("%d models, want 3 (a blank id is left out)", len(p.Models))
	}
	byID := map[string]kernelModel{}
	for _, m := range p.Models {
		byID[m.ID] = m
	}
	glm := byID["zcode/GLM-5.3-Flash"]
	if glm.Name != "Magpie · GLM-5.3-Flash" || glm.ContextWindow != 1000000 || glm.MaxTokens != 128000 ||
		strings.Join(glm.Input, ",") != "text,image" || strings.Join(glm.Thinking, ",") != "low,high,max" || !glm.Reasoning {
		t.Fatalf("GLM: %+v", glm)
	}
	if glm.ThinkingLevelMap["off"] != nil || *glm.ThinkingLevelMap["max"] != "max" || glm.ThinkingLevelMap["medium"] != nil {
		t.Fatalf("GLM's levels: %v", glm.ThinkingLevelMap)
	}
	grok := byID["grok/grok-4.7@500k"]
	if grok.ContextWindow != 500000 || grok.MaxTokens != magpieDefaultOutput || strings.Join(grok.Thinking, ",") != "off,low,medium,high,xhigh" || *grok.ThinkingLevelMap["off"] != "none" {
		t.Fatalf("Grok: %+v", grok)
	}
	plain := byID["plain/model"]
	if plain.ContextWindow != magpieDefaultContext || plain.Reasoning || plain.Thinking != nil || plain.ThinkingLevelMap != nil {
		t.Fatalf("a model that says nothing: %+v", plain)
	}
	if _, ok := magpieProvider(MagpieCatalog{BaseURL: "http://127.0.0.1:3425"}); ok {
		t.Fatal("a gateway with no models gives a provider, which the Kernel refuses")
	}
}

// With no Factory key, Magpie alone gives the Kernel its models: a conversation runs on one through the gateway, with
// the key a loopback client sends, and nothing of it goes to Factory. Each source says how it stands.
func TestTheHostsKernelRunsMagpiesModelsWithNoFactoryKey(t *testing.T) {
	f := newFakeFactory(t)
	m := newFakeMagpie(t)
	hk := startHostKernelPrepared(t, f, false, nil, func(h *Host) { useMagpie(t, h, m, true) })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	if _, ok := offered(t, ctx, c, "factory-chat", ""); ok {
		t.Fatal("a Kernel with no key behind it offers Factory's models")
	}
	glm, ok := offered(t, ctx, c, "magpie", "zcode/GLM-5.3-Flash")
	if !ok {
		t.Fatal("harness.info does not offer Magpie's GLM-5.3-Flash")
	}
	answerSays(t, ctx, c, glm, "Hello from Magpie")
	bodies := m.bodiesSeen()
	if len(bodies) != 1 || !strings.Contains(bodies[0], `"model":"zcode/GLM-5.3-Flash"`) {
		t.Fatalf("the gateway was asked %v", bodies)
	}
	if seen := f.requests(); len(seen) != 0 {
		t.Fatalf("Factory saw %d model requests", len(seen))
	}
	if got := sourceState(hk.h, SourceFactory); got != SourceEmpty {
		t.Errorf("Factory with no key is %q", got)
	}
	if got := sourceState(hk.h, SourceMagpie); got != SourceReady {
		t.Errorf("Magpie is %q", got)
	}
}

// Turning Magpie on or off reaches the running Kernel: its models come and go, and the Kernel and its connections keep
// running. Factory's stay throughout.
func TestTurningMagpieOnOrOffReachesTheRunningKernel(t *testing.T) {
	f := newFakeFactory(t)
	m := newFakeMagpie(t)
	hk := startHostKernel(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	first := hk.generation()
	if _, ok := offered(t, ctx, c, "magpie", ""); ok {
		t.Fatal("Magpie is off, and the Kernel offers its models")
	}
	if got := sourceState(hk.h, SourceMagpie); got != SourceOff {
		t.Fatalf("Magpie off is %q", got)
	}
	for _, on := range []bool{true, false} {
		useMagpie(t, hk.h, m, on)
		hk.h.Changed()
		harnessChanged(t, ctx, c)
		hk.stillRunning(t, first)
		if _, ok := offered(t, ctx, c, "magpie", "zcode/GLM-5.3-Flash"); ok != on {
			t.Fatalf("Magpie on is %v, and the Kernel offers its models: %v", on, ok)
		}
		if _, ok := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017"); !ok {
			t.Fatalf("Magpie on is %v, and Factory's models are gone", on)
		}
	}
	chat, _ := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017")
	answer(t, ctx, c, chat)
}

// A Magpie that does not answer leaves the Kernel Factory's models and says so; once it answers, a refresh gives the
// running Kernel its models.
func TestAMagpieThatAnswersLaterReachesTheRunningKernel(t *testing.T) {
	f := newFakeFactory(t)
	m := newFakeMagpie(t)
	m.down.Store(true)
	hk := startHostKernelPrepared(t, f, true, nil, func(h *Host) { useMagpie(t, h, m, true) })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	first := hk.generation()
	if _, ok := offered(t, ctx, c, "magpie", ""); ok {
		t.Fatal("a Magpie that never answered gives models")
	}
	if got := sourceState(hk.h, SourceMagpie); got != SourceUnreachable {
		t.Fatalf("a Magpie that does not answer is %q", got)
	}
	m.down.Store(false)
	hk.h.RefreshMagpie()
	harnessChanged(t, ctx, c)
	hk.stillRunning(t, first)
	if _, ok := offered(t, ctx, c, "magpie", "zcode/GLM-5.3-Flash"); !ok {
		t.Fatal("no Magpie models once it answers")
	}
	if got := sourceState(hk.h, SourceMagpie); got != SourceReady {
		t.Fatalf("a Magpie that answers is %q", got)
	}
}
