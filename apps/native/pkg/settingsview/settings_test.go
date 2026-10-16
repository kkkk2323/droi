package settingsview

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

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
