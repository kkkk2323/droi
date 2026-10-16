package sessionview

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// harness is one Session's View against a Fake Daemon: Update queues work
// for the test goroutine, which runs it before every frame, as the UI
// thread would.
type harness struct {
	t   *testing.T
	d   *fakedaemon.Daemon
	ctl *controller.Controller
	v   *View
	tt  *ui.Tester

	mu    sync.Mutex
	queue []func()
}

func newHarness(t *testing.T, sc fakedaemon.Scenario) *harness {
	t.Helper()
	theme.RegisterFonts()
	d := fakedaemon.Start(t, sc)
	ctl := controller.New(controller.Config{
		URL:                 d.URL,
		Credential:          func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		DefaultMessageLimit: LoadedMessageLimit,
	})
	t.Cleanup(func() { ctl.Close() })
	h := &harness{t: t, d: d, ctl: ctl}
	revs := new(Revisions)
	t.Cleanup(revs.Watch(ctl.Store(), func() { h.update(func() {}) }))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := ctl.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	k := kit.New(theme.Parse("light"), theme.FontChoice(""), theme.TextSize(""), 1)
	h.v = New(d.Sessions[0].SessionID, Env{
		Controller: ctl,
		Ctx:        context.Background(),
		Kit:        func() *kit.Kit { return k },
		Update:     h.update,
		Revisions:  revs,
	})
	h.v.Open()
	h.tt = ui.NewTester(func(c *ui.Context) {
		c.SetTheme(k.UITheme(c.Theme()))
		h.v.Build(c, nil)
	}, 900, 640)
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

// until draws frames until ok holds, failing after a few seconds.
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

func (h *harness) hasText(sub string) bool {
	for _, s := range h.tt.Texts() {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// send types text into the plain composer once the Session is loaded, and
// presses Enter.
func (h *harness) send(text string) {
	h.t.Helper()
	h.until("the Session loaded", func() bool { return h.ctl.Store().LoadState(h.v.ID()) == session.Loaded })
	if err := h.tt.Click("Message"); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
	h.tt.Type(text)
	h.frame()
	h.tt.Key(0, ui.KeyEnter)
	h.frame()
}

func deployScenario(turn *fakedaemon.Turn) fakedaemon.Scenario {
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Deploy", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}},
		Turn:     turn,
	}
}

func TestASessionsMessagesShow(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Login", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
		{Role: "user", Text: "Why does login fail?"},
		{Role: "assistant", Content: []map[string]any{
			{"type": "tool_use", "id": "call_1", "name": "Read", "input": map[string]any{"file_path": "src/auth/login.ts"}},
		}},
		{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "call_1", "content": "refreshToken(user)"}}},
		{Role: "assistant", Text: "`login()` does not **await** the refresh."},
	}}}})
	h.until("the reply", func() bool { return h.hasText("does not") })
	for _, want := range []string{"Why does login fail?", "Read", "await"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if _, ok := h.tt.Find("Transcript"); !ok {
		t.Error("no transcript")
	}
}

func TestEnterInThePlainComposerSendsTheMessage(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Hello ", "from ", "the Daemon."}}))
	h.send("Say hello")
	req, err := h.d.WaitForRequest("daemon.add_user_message", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Text != "Say hello" {
		t.Fatalf("sent %s (%v)", req.Params, err)
	}
	h.until("the streamed reply", func() bool { return h.hasText("Hello from the Daemon.") })
	if !h.hasText("Say hello") {
		t.Errorf("the sent message is not shown: %q", h.tt.Texts())
	}
	if h.v.plain.text != "" {
		t.Errorf("the composer kept %q", h.v.plain.text)
	}
}

func TestAPermissionIsAnsweredFromItsCard(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "permission", Command: "rm -rf build", Reply: "Build directory removed."}))
	h.send("Clean the build dir")
	h.until("the permission card", func() bool { return h.hasText("rm -rf build") })
	if _, ok := h.tt.Find("Message"); ok {
		t.Error("the composer shows beside the permission card")
	}
	if err := h.tt.Click("Yes, allow"); err != nil {
		t.Fatal(err)
	}
	h.until("the reply after the permission", func() bool { return h.hasText("Build directory removed.") })
	if got := h.ctl.PendingPermissions(h.v.ID()); len(got) != 0 {
		t.Fatalf("still pending: %+v", got)
	}
	h.until("the composer back", func() bool { _, ok := h.tt.Find("Message"); return ok })
}

func TestQuoteText(t *testing.T) {
	for _, c := range []struct{ message, want string }{
		{"", "> a\n> b\n\n"},
		{"see", "see\n\n> a\n> b\n\n"},
		{"see\n", "see\n\n> a\n> b\n\n"},
		{"see\n\n", "see\n\n> a\n> b\n\n"},
	} {
		if got := QuoteText(c.message, " a\nb "); got != c.want {
			t.Errorf("QuoteText(%q) = %q, want %q", c.message, got, c.want)
		}
	}
}
