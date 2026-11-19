package settingsview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/egoist/mygo/ui"
	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// harness draws one section against a Fake Daemon: Update queues work for
// the test goroutine, which runs it before every frame.
type harness struct {
	t     *testing.T
	d     *fakedaemon.Daemon
	s     *Settings
	prefs *prefs.Store
	tt    *ui.Tester

	mu    sync.Mutex
	queue []func()
}

func newHarness(t *testing.T, build func(c *ui.Context, s *Settings)) *harness {
	t.Helper()
	theme.RegisterFonts()
	d := fakedaemon.Start(t, fakedaemon.Scenario{})
	ctl := controller.New(controller.Config{
		URL:        d.URL,
		Credential: func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
	})
	t.Cleanup(func() { ctl.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := ctl.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, d: d, prefs: prefs.Memory()}
	k := kit.New(theme.Light, "", "", 1)
	h.s = New(Env{
		Kit:      func() *kit.Kit { return k },
		Prefs:    h.prefs,
		Update:   h.update,
		Defaults: &Defaults{Controller: ctl, Update: h.update},
		Failed:   func(msg string) { t.Errorf("failed: %s", msg) },
	})
	h.tt = ui.NewTester(func(c *ui.Context) {
		c.SetTheme(k.UITheme(c.Theme()))
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Gap(k.Px(24)).Padding(k.Px(32)).Children(func() { build(c, h.s) })
		})
	}, 900, 2000)
	h.frame()
	return h
}

func (h *harness) update(fn func()) {
	h.mu.Lock()
	h.queue = append(h.queue, fn)
	h.mu.Unlock()
}

func (h *harness) frame() {
	h.mu.Lock()
	q := h.queue
	h.queue = nil
	h.mu.Unlock()
	for _, fn := range q {
		fn()
	}
	keepTime(h.tt)
	h.tt.Frame()
}

// keepTime moves the Tester's clock to the system's: the Fake Daemon answers
// in real time, so the view's animations and timers have to follow it.
func keepTime(tt *ui.Tester) {
	if d := time.Since(tt.Now()); d > 0 {
		tt.Advance(d)
	}
}

func (h *harness) until(what string, ok func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		h.frame()
		if ok() {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("never: %s; texts %q", what, h.tt.Texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *harness) click(name string) {
	h.t.Helper()
	if err := h.tt.Click(name); err != nil {
		h.t.Fatalf("click %q: %v", name, err)
	}
	h.frame()
}

func (h *harness) hasText(sub string) bool {
	for _, s := range h.tt.Texts() {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Picking a theme keeps it in the preferences and has the app apply it.
func TestGeneralKeepsTheThemePicked(t *testing.T) {
	applied := 0
	h := newHarness(t, func(c *ui.Context, s *Settings) { s.General(c, func() { applied++ }) })
	for _, want := range []string{"Appearance", "Language", "Font", "Text size"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	h.click("Light")
	h.click("Dark")
	if got := prefs.Theme.Get(h.prefs); got != string(theme.Dark) || applied != 1 {
		t.Fatalf("theme %q, applied %d times", got, applied)
	}
}

// A Session default picked is saved through the Daemon and shown at once.
func TestSessionDefaultsSavesAPick(t *testing.T) {
	h := newHarness(t, func(c *ui.Context, s *Settings) {
		s.SessionDefaults(c, "", func() { ui.Text(c, "From the app") })
	})
	h.until("the defaults", func() bool { return h.hasText("Limits for specific models") })
	if !h.hasText("Shared with the droid CLI") || !h.hasText("From the app") {
		t.Fatalf("no note or banner: %q", h.tt.Texts())
	}
	h.click("Default interaction mode")
	h.click("Spec")
	req, err := h.d.WaitForRequest("daemon.update_session_defaults", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(req.Params), `"spec"`) {
		t.Fatalf("update_session_defaults %s", req.Params)
	}
	if dv := h.s.env.Defaults.Get(); dv == nil || dv.InteractionMode != "spec" {
		t.Fatalf("the defaults shown: %+v", dv)
	}
}

// When droid's legacy-model-upgrade layer rewrites a newly picked older model
// (for example gemini-3.8-flash -> gemini-4-hydrogen or gpt-6.1-sol ->
// gpt-6.2-sol), Save retries once with acknowledgeLegacyModelId so the choice
// sticks. When the Daemon rewrote a non-legacy model for another reason and
// rejects the acknowledgement, Save does not report a failure.
func TestDefaultsSaveAcknowledgesLegacyModel(t *testing.T) {
	upgrades := map[string]string{
		"gemini-3.8-flash": "gemini-4-hydrogen",
		"gpt-6.1-sol":      "gpt-6.2-sol",
	}
	var (
		mu              sync.Mutex
		compactionModel = "gemini-4-hydrogen"
		subagentModels  = map[string]string{"lightModel": "custom:magpie-openai-gpt-6-1-sol-abcdef"}
		overrides       = map[string]bool{}
		updates         []map[string]any
		rejectAck       bool
	)
	effective := func() map[string]any {
		cm := compactionModel
		if up, ok := upgrades[cm]; ok && !overrides[cm] {
			cm = up
		}
		sub := map[string]string{}
		for k, v := range subagentModels {
			if up, ok := upgrades[v]; ok && !overrides[v] {
				v = up
			}
			sub[k] = v
		}
		return map[string]any{
			"compactionModel":       cm,
			"subagentModelSettings": sub,
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, raw, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var req struct {
				ID     string         `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				return
			}
			var resp map[string]any
			switch req.Method {
			case "daemon.authenticate":
				resp = map[string]any{"type": "response", "id": req.ID, "result": map[string]any{"authenticated": true}}
			case "daemon.get_default_settings":
				mu.Lock()
				resp = map[string]any{"type": "response", "id": req.ID, "result": effective()}
				mu.Unlock()
			case "daemon.update_session_defaults":
				mu.Lock()
				updates = append(updates, req.Params)
				ack, _ := req.Params["acknowledgeLegacyModelId"].(string)
				if ack != "" && rejectAck {
					resp = map[string]any{"type": "response", "id": req.ID, "error": map[string]any{"code": -32603, "message": "Model is not legacy"}}
				} else {
					if ack != "" {
						overrides[ack] = true
					}
					if cm, ok := req.Params["compactionModel"].(string); ok {
						compactionModel = cm
					}
					if sub, ok := req.Params["subagentModelSettings"].(map[string]any); ok {
						for k, v := range sub {
							if s, ok := v.(string); ok {
								subagentModels[k] = s
							}
						}
					}
					resp = map[string]any{"type": "response", "id": req.ID, "result": map[string]any{"success": true, "defaults": effective()}}
				}
				mu.Unlock()
			default:
				resp = map[string]any{"type": "response", "id": req.ID, "result": map[string]any{}}
			}
			b, _ := json.Marshal(resp)
			if err := conn.Write(r.Context(), websocket.MessageText, b); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctl := controller.New(controller.Config{
		URL:        "ws" + strings.TrimPrefix(srv.URL, "http"),
		Credential: func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
	})
	t.Cleanup(func() { ctl.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ctl.Connect(ctx); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{}, 16)
	var failedErr error
	defs := &Defaults{
		Controller: ctl,
		Update: func(fn func()) {
			fn()
			select {
			case done <- struct{}{}:
			default:
			}
		},
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			if ok() {
				return
			}
			select {
			case <-done:
			case <-deadline:
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}

	defs.Get()
	wait("initial defaults", func() bool { return defs.Get() != nil })

	// 1. Summary model: gemini-3.8-flash is rewritten to gemini-4-hydrogen on
	// the first call, then acknowledged and kept.
	cm := "gemini-3.8-flash"
	defs.Save(*defs.Get(), defaults.Patch{CompactionModel: &cm}, func(err error) { failedErr = err })
	wait("compaction model reload", func() bool {
		mu.Lock()
		n := len(updates)
		mu.Unlock()
		return n >= 2 && defs.Get().CompactionModel == "gemini-3.8-flash"
	})
	mu.Lock()
	if len(updates) != 2 || updates[0]["acknowledgeLegacyModelId"] != nil || updates[1]["acknowledgeLegacyModelId"] != "gemini-3.8-flash" {
		t.Fatalf("compaction updates = %v", updates)
	}
	mu.Unlock()

	// 2. Subagent light task model: gpt-6.1-sol is rewritten to gpt-6.2-sol,
	// then acknowledged and kept.
	dv := *defs.Get()
	defs.Save(dv, defaults.Patch{SubagentModelSettings: defaults.WithSubagentTierModel(dv.SubagentModelSettings, "light", "gpt-6.1-sol")}, func(err error) { failedErr = err })
	wait("subagent model reload", func() bool {
		mu.Lock()
		n := len(updates)
		mu.Unlock()
		return n >= 4 && defs.Get().SubagentModelSettings["lightModel"] == "gpt-6.1-sol"
	})
	mu.Lock()
	if len(updates) != 4 || updates[2]["acknowledgeLegacyModelId"] != nil || updates[3]["acknowledgeLegacyModelId"] != "gpt-6.1-sol" {
		t.Fatalf("subagent updates = %v", updates)
	}
	// 3. A retry rejected as "Model is not legacy" does not fail the save.
	delete(overrides, "gemini-3.8-flash")
	rejectAck = true
	mu.Unlock()

	defs.Save(*defs.Get(), defaults.Patch{CompactionModel: &cm}, func(err error) { failedErr = err })
	wait("non-legacy retry completion", func() bool {
		mu.Lock()
		n := len(updates)
		mu.Unlock()
		return n >= 6 && defs.Get().CompactionModel == "gemini-4-hydrogen"
	})
	if failedErr != nil {
		t.Fatalf("unexpected save error: %v", failedErr)
	}
}
