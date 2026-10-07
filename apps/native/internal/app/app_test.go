package app

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/theme"
)

// The reference screens are 1024x640 at 2x (tests/web/native-reference.spec.ts).
const refW, refH = 1024, 640

// harness is the window against a Fake Daemon: Update queues work for the
// test goroutine, which runs it before every frame, as the main thread would.
type harness struct {
	t   *testing.T
	d   *fakedaemon.Daemon
	ctl *controller.Controller
	a   *App
	tt  *ui.Tester

	mu    sync.Mutex
	queue []func()
}

func newHarness(t *testing.T, sc fakedaemon.Scenario, themeName string) *harness {
	t.Helper()
	return newHarnessWith(t, sc, themeName, nil)
}

// newHarnessWith is newHarness with the app's Config changed by with.
func newHarnessWith(t *testing.T, sc fakedaemon.Scenario, themeName string, with func(*Config)) *harness {
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
	p := prefs.Memory()
	if themeName != "" {
		prefs.Theme.Set(p, themeName)
	}
	cfg := Config{
		Controller: ctl,
		Prefs:      p,
		Update: func(fn func()) {
			h.mu.Lock()
			h.queue = append(h.queue, fn)
			h.mu.Unlock()
		},
		Now:         func() time.Time { return time.Now() },
		FactoryHome: t.TempDir(),
		// The reference is the Local Client in the Desktop Shell's window:
		// traffic lights over the sidebar, and Finder with no icon to read.
		InsetTop:   true,
		OpenInApps: func() []host.OpenInApp { return []host.OpenInApp{{ID: "finder", Label: "Finder"}} },
	}
	if with != nil {
		with(&cfg)
	}
	h.a = New(cfg)
	h.a.Start()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := ctl.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	h.tt = ui.NewTester(h.a.View, refW, refH)
	h.tt.SetScale(2)
	h.frame()
	return h
}

func (h *harness) frame() {
	h.mu.Lock()
	q := h.queue
	h.queue = nil
	h.mu.Unlock()
	for _, fn := range q {
		fn()
	}
	h.tt.Frame()
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

func (h *harness) click(name string) {
	h.t.Helper()
	if err := h.tt.Click(name); err != nil {
		h.t.Fatalf("click %q: %v", name, err)
	}
	h.frame()
}

// settle waits for the Daemon's answers and the animations to end.
func (h *harness) settle() {
	for i := 0; i < 30; i++ {
		h.frame()
		time.Sleep(20 * time.Millisecond)
	}
	h.tt.Move(refW-1, refH-1)
	h.frame()
}

// shoot compares the window with the reference screen of that name and,
// when DROI_NATIVE_SHOTS names a folder, writes the shot and a diff there.
func (h *harness) shoot(name string) float64 {
	h.t.Helper()
	h.settle()
	got := h.tt.Image()
	ref := readPNG(filepath.Join("..", "..", "testdata", "reference", name+".png"))
	diff, frac := diffImages(got, ref)
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		writePNG(filepath.Join(dir, name+".png"), got)
		if diff != nil {
			writePNG(filepath.Join(dir, name+"-diff.png"), diff)
		}
	}
	h.t.Logf("%s: %.2f%% of pixels differ from the reference", name, frac*100)
	return frac
}

func readPNG(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
}

// diffImages marks in red the pixels whose channels differ by more than
// 24 of 255, over a faded copy of the reference, and gives their share.
func diffImages(got, ref image.Image) (*image.RGBA, float64) {
	if ref == nil || got.Bounds() != ref.Bounds() {
		return nil, 1
	}
	b := got.Bounds()
	out := image.NewRGBA(b)
	bad := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r1, g1, b1, _ := got.At(x, y).RGBA()
			r2, g2, b2, _ := ref.At(x, y).RGBA()
			if far(r1, r2) || far(g1, g2) || far(b1, b2) {
				bad++
				out.Set(x, y, color.RGBA{255, 0, 0, 255})
				continue
			}
			l := uint8((r2>>8 + g2>>8 + b2>>8) / 3)
			l = 255 - (255-l)/4
			out.Set(x, y, color.RGBA{l, l, l, 255})
		}
	}
	return out, float64(bad) / float64(b.Dx()*b.Dy())
}

func far(a, b uint32) bool {
	d := int(a>>8) - int(b>>8)
	return d > 24 || d < -24
}

// history is the reference's "Fix the login race" Session.
func history() []fakedaemon.Message {
	return []fakedaemon.Message{
		{Role: "user", Text: "Login sometimes fails right after the token expires. Can you find out why?"},
		{Role: "assistant", Content: []map[string]any{
			{"type": "thinking", "thinking": "The token refresh is not awaited; I should confirm before editing.", "signature": "", "durationMs": 1400},
			{"type": "tool_use", "id": "call_1", "name": "Read", "input": map[string]any{"file_path": "src/auth/login.ts"}},
		}},
		{Role: "tool", Content: []map[string]any{
			{"type": "tool_result", "toolUseId": "call_1", "content": "export async function login(user) {\n  refreshToken(user)\n  return session(user)\n}"},
		}},
		{Role: "assistant", Text: strings.Join([]string{
			"`login()` calls `refreshToken()` without awaiting it, so `session()` can run with the stale token.",
			"",
			"I will add the missing `await` and a regression test:",
			"",
			"```ts",
			"export async function login(user) {",
			"  await refreshToken(user)",
			"  return session(user)",
			"}",
			"```",
			"",
			"- `src/auth/login.ts`: await the refresh",
			"- `src/auth/login.test.ts`: cover the expired-token path",
		}, "\n")},
	}
}

func sessionsScenario() fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Refactor invoice generator", Cwd: "/Users/dev/billing-service", Messages: []fakedaemon.Message{{Role: "user", Text: "Refactor the invoice generator"}}},
		{Title: "Add dark mode toggle", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Add dark mode"}}},
		{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: history()},
	}}
}

func (h *harness) openSession(title string) {
	h.t.Helper()
	h.until("the Session list", func() bool { return h.hasText(title) })
	h.click(title)
}

func TestSessionView(t *testing.T) {
	for _, theme := range []string{"light", "dark", "solarized-light"} {
		t.Run(theme, func(t *testing.T) {
			h := newHarness(t, sessionsScenario(), theme)
			h.openSession("Fix the login race")
			h.until("the reply", func() bool { return h.hasText("regression test") })
			for _, want := range []string{"Login sometimes fails", "Copy", "Reasoned for"} {
				if !h.hasText(want) {
					t.Errorf("no %q in %q", want, h.tt.Texts())
				}
			}
			h.shoot("session-" + theme)
		})
	}
}

func TestSessionExpanded(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("regression test") })
	h.until("the reasoning row", func() bool { _, ok := h.findPrefix("Reasoned for"); return ok })
	name, _ := h.findPrefix("Reasoned for")
	h.click(name)
	h.until("the Read row", func() bool { _, ok := h.findPrefix("Read"); return ok })
	name, _ = h.findPrefix("Read")
	h.click(name)
	h.shoot("session-expanded")
}

// findPrefix is the first text on screen starting with prefix.
func (h *harness) findPrefix(prefix string) (string, bool) {
	for _, s := range h.tt.Texts() {
		if strings.HasPrefix(s, prefix) {
			return s, true
		}
	}
	return "", false
}

func TestSidebarHidden(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("regression test") })
	h.click("Hide sidebar")
	h.shoot("sidebar-hidden")
}

func TestNewSessionPage(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.shoot("new-session")
}

// The logo and the question stand in the middle of the room above the
// composer, as the web Client's flex column centres them.
func TestNewSessionPageCentresItsQuestion(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.settle()
	logo, ok1 := h.tt.Find("Droi")
	question, ok2 := h.tt.Find("What do you want to build in")
	composer, ok3 := h.tt.Find("Message composer")
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("logo %v, question %v, composer %v", ok1, ok2, ok3)
	}
	const header = 44
	mid := (logo.Y + question.Y + question.H) / 2
	room := (header + composer.Y) / 2
	if d := mid - room; d < -4 || d > 4 {
		t.Fatalf("the question's middle is %.0f, the room's %.0f", mid, room)
	}
}

// The Workspace opens a picker: its search narrows the recent Workspaces
// and Enter takes the one left; "Don't work in a project" leaves the
// question without one and "Work in a project" under the composer brings
// the picker back.
func TestTheWorkspacePickerSearchesAndCanPickNoProject(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Workspace")
	h.until("the search", func() bool { _, ok := h.tt.Find("Search projects"); return ok })
	// The texts of the picker's list, each row's name and label once.
	listed := func() string {
		all := strings.Join(h.tt.Texts(), "|")
		_, after, _ := strings.Cut(all, "|Projects|")
		before, _, _ := strings.Cut(after, "|Other folder…")
		return before
	}
	if got := listed(); got != "acme-web|acme-web|billing-service|billing-service" {
		t.Fatalf("the picker lists %q", got)
	}
	h.tt.Type("bill")
	h.frame()
	if got := listed(); got != "billing-service|billing-service" {
		t.Fatalf("the search left %q", got)
	}
	h.tt.Key(0, ui.KeyEnter)
	h.frame()
	if got := h.a.newPage.pick; got != "/Users/dev/billing-service" {
		t.Fatalf("Enter picked %q", got)
	}

	h.click("Workspace")
	h.click("Don't work in a project")
	h.until("the question without a project", func() bool { return h.hasText("What should we work on?") })
	if _, ok := h.tt.Find("Workspace"); ok {
		t.Fatal("the question still has a Workspace")
	}
	if _, ok := h.tt.Find("Start session"); !ok {
		t.Fatal("no start button")
	}
	h.click("Work in a project")
	h.until("the search", func() bool { _, ok := h.tt.Find("Search projects"); return ok })
	h.tt.Type("acme")
	h.tt.Key(0, ui.KeyEnter)
	h.until("the question with acme-web", func() bool { return h.hasText("What do you want to build in") })
	if got := h.a.newPage.pick; got != "/Users/dev/acme-web" {
		t.Fatalf("the click picked %q", got)
	}
}

// Hiding the sidebar slides the main panel over, as the web Client's width
// transition does, rather than moving it at once.
func TestTheSidebarSlides(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.settle()
	x := func() float32 {
		r, _ := h.tt.Find("Message composer")
		return r.X
	}
	start := x()
	_ = h.tt.Click("Hide sidebar")
	var seen []float32
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.tt.Frame()
		seen = append(seen, x())
		time.Sleep(10 * time.Millisecond)
	}
	end := seen[len(seen)-1]
	if end >= start {
		t.Fatalf("the composer stayed at %.0f", start)
	}
	between := false
	for _, v := range seen {
		between = between || v < start-1 && v > end+1
	}
	if !between {
		t.Fatalf("the composer jumped from %.0f to %.0f: %v", start, end, seen)
	}
}

// A paragraph of the reply is selectable and copies what was selected.
// Its text comes from the runs built inside it, so Selectable must come
// after them: before, a drag selected nothing and Copy copied "".
func TestAReplyParagraphCopiesItsSelection(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("regression test") })
	h.settle()
	const para = "I will add the missing await and a regression test:"
	r, ok := h.tt.Find(para)
	if !ok {
		t.Fatalf("no paragraph %q", para)
	}
	h.tt.Press(r.X+1, r.Y+r.H/2)
	h.tt.Move(r.X+r.W-1, r.Y+r.H/2)
	h.tt.Release(r.X+r.W-1, r.Y+r.H/2)
	h.frame()
	h.tt.Command("copy")
	h.frame()
	if got := h.tt.Clipboard(); got == "" || !strings.HasPrefix(para, got) {
		t.Fatalf("copied %q", got)
	}
}

// The end of a reply has a Copy button that copies the turn's text as
// Markdown, and the time without the word "took".
func TestAReplyEndsWithACopyButton(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Fix the login race")
	h.until("the Copy button", func() bool { _, ok := h.tt.Find("Copy reply"); return ok })
	if h.hasText("took") {
		t.Fatalf("the turn's end still says took: %q", h.tt.Texts())
	}
	h.click("Copy reply")
	got := h.tt.Clipboard()
	if !strings.HasPrefix(got, "`login()` calls `refreshToken()`") || !strings.Contains(got, "```ts\nexport async function login(user) {") ||
		strings.Contains(got, "stale token.\n\n\n") || strings.Contains(got, "confirm before editing") {
		t.Fatalf("copied %q", got)
	}
}

func TestSettingsPages(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Settings")
	for _, tab := range []struct{ label, ref string }{
		{"General", "settings-general"},
		{"Session defaults", "settings-session-defaults"},
		{"Notifications", "settings-notifications"},
		{"About", "settings-about"},
	} {
		h.click(tab.label)
		h.until(tab.label+" heading", func() bool { return h.hasText(tab.label) })
		h.shoot(tab.ref)
	}
}

func deployScenario(turn *fakedaemon.Turn) fakedaemon.Scenario {
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Deploy", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}},
		Turn:     turn,
	}
}

func (h *harness) send(text string) {
	h.t.Helper()
	// The composer takes text once the Session is loaded.
	h.until("the composer to take text", func() bool {
		if _, ok := h.tt.Find("Message"); !ok {
			return false
		}
		_ = h.tt.Click("Message")
		h.frame()
		h.tt.Type(text)
		h.frame()
		for _, v := range h.a.views {
			if v.composer.text == text {
				return true
			}
		}
		return false
	})
	h.click("Send")
}

func TestPermissionCard(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "permission", Command: "rm -rf build", Reply: "Build directory removed."}), "")
	h.openSession("Deploy")
	h.send("Clean the build dir")
	h.until("the permission card", func() bool { return h.hasText("rm -rf build") })
	h.shoot("permission")
	h.click("Yes, allow")
	h.until("the reply after the permission", func() bool { return h.hasText("Build directory removed.") })
}

func TestAskUserCard(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "askUser", Question: "Which environment?", Options: []string{"staging", "production"}}), "")
	h.openSession("Deploy")
	h.send("Ship it")
	h.until("the question", func() bool { return h.hasText("Which environment?") })
	h.shoot("ask-user")
	// The question can be selected and copied.
	r, ok := h.tt.Find("Which environment?")
	if !ok {
		t.Fatal("no question")
	}
	h.tt.Press(r.X+1, r.Y+r.H/2)
	h.tt.Move(r.X+r.W-1, r.Y+r.H/2)
	h.tt.Release(r.X+r.W-1, r.Y+r.H/2)
	h.frame()
	h.tt.Command("copy")
	h.frame()
	if got := h.tt.Clipboard(); got == "" || !strings.HasPrefix("Which environment?", got) {
		t.Fatalf("copied %q", got)
	}
}

func TestReplyStreams(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Hello ", "from ", "the Daemon."}}), "")
	h.openSession("Deploy")
	h.send("Say hello")
	h.until("the streamed reply", func() bool { return h.hasText("Hello from the Daemon.") })
	if !h.hasText("Say hello") {
		t.Errorf("the sent message is not shown: %q", h.tt.Texts())
	}
}
