package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// The window is a Client of its own Host's Gateway (ADR 0019): the harness
// puts a real Gateway between it and the Fake Daemon, so every view test
// runs window, SDK, Gateway, Fake Daemon.

func TestTheWindowAuthenticatesWithTheGatewaysCredentialNotItsOwn(t *testing.T) {
	h := newHarness(t, oneSession(), "")
	h.until("the Session list", func() bool { return h.hasText("One") })
	reqs, err := h.d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	authenticated := false
	for _, r := range reqs {
		if strings.Contains(string(r.Params), gateway.APIKeyPlaceholder) {
			t.Fatalf("the Daemon saw the placeholder in %s: %s", r.Method, r.Params)
		}
		if r.Method == "daemon.authenticate" {
			authenticated = strings.Contains(string(r.Params), harnessCredential)
		}
	}
	if !authenticated {
		t.Fatal("the Daemon never got the Gateway's credential")
	}
}

func initializeRequests(t *testing.T, h *harness) []string {
	t.Helper()
	reqs, err := h.d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range reqs {
		if r.Method == "daemon.initialize_session" {
			out = append(out, string(r.Params))
		}
	}
	return out
}

func TestSessionsStartedFromTheWindowGetTheSystemPromptAdditionOnceFromTheGateway(t *testing.T) {
	h := startHarness(t, draftScenario(), "", "Be brief", nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.click("Message")
	h.typeInto("go")
	h.click("Start session")
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })

	got := initializeRequests(t, h)
	if len(got) == 0 {
		t.Fatal("no Session was created")
	}
	for _, params := range got {
		var p struct {
			SystemPrompt map[string]string `json:"systemPrompt"`
		}
		if err := json.Unmarshal([]byte(params), &p); err != nil {
			t.Fatal(err)
		}
		sp := p.SystemPrompt
		if len(sp) != 3 || sp["type"] != "preset" || sp["preset"] != "droid" || sp["append"] != "Be brief" {
			t.Fatalf("systemPrompt %v in %s", sp, params)
		}
		if n := strings.Count(params, "Be brief"); n != 1 {
			t.Fatalf("the addition is in the request %d times: %s", n, params)
		}
	}
}

func TestResettingThePairingTokenOrTurningRemoteAccessOffKeepsTheWindowConnected(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	before := h.count("daemon.authenticate")

	h.gw.SetRemoteAccess(true)
	h.gw.Revoke(gateway.TokenPairing)
	h.gw.SetRemoteAccess(false)
	h.settle()
	if !h.ctl.Status().Connected || h.count("daemon.authenticate") != before {
		t.Fatal("the window lost its connection")
	}
	h.openSession("Fix the login race")
	h.until("a Session read through the same connection", func() bool { return h.hasText("regression test") })
}

func TestTheWindowReconnectsThroughTheGatewayWhenTheDaemonComesBack(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if err := h.d.GoDown(); err != nil {
		t.Fatal(err)
	}
	h.until("the loss", func() bool { return !h.ctl.Status().Connected })
	if err := h.d.ComeBack(); err != nil {
		t.Fatal(err)
	}
	h.until("the connection again", func() bool { return h.ctl.Status().Connected })
	h.openSession("Fix the login race")
	h.until("a Session read after it", func() bool { return h.hasText("regression test") })
}

func TestAGatewayThatCannotListenShowsAnErrorInTheWindow(t *testing.T) {
	h0 := testHost(t)
	h0.Settings.Update(func(s *host.Settings) { s.APIKey = host.OptString("fk-x") })
	none := controller.New(controller.Config{
		ResolveURL: func(context.Context) (string, error) {
			return "", errors.New("Droi's Gateway could not listen: address in use")
		},
		MaxConnectAttempts: 1,
	})
	t.Cleanup(func() { none.Close() })
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host, cfg.Controller = h0, none })
	// A Gateway that is not there counts as a Daemon that is not up yet, so
	// Connect keeps trying; the failure it records is what the window shows.
	go none.Connect(context.Background())
	h.until("the reason", func() bool { return h.hasText("address in use") })
}

// startScratchSession opens the start page for a Scratch Workspace and
// starts a Session from it.
func startScratchSession(t *testing.T, h *harness) {
	t.Helper()
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.a.Go(Route{Name: "new", Scratch: true})
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.typeInto("hi")
	h.click("Start session")
}

// madeFolder is a Scratch folder with a name the Fake Daemon can be told
// is a valid directory beforehand.
type madeFolder string

func (f madeFolder) Create() (string, error) { return string(f), os.MkdirAll(string(f), 0o755) }
func (f madeFolder) Trash(string) error      { return nil }
func (f madeFolder) Restore(string) error    { return nil }

func TestASessionWithoutAWorkspaceStartsInAScratchFolderTheGatewayMade(t *testing.T) {
	made := filepath.Join(t.TempDir(), "chats", "2026-10-14-0a1b2c")
	sc := draftScenario()
	sc.ValidDirectories = []string{made}
	h := startHarnessGateway(t, sc, "", "", func(o *gateway.Options) { o.Scratch = madeFolder(made) }, nil)
	startScratchSession(t, h)
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })

	var p struct {
		Cwd  string           `json:"cwd"`
		Tags []map[string]any `json:"tags"`
	}
	// The start page may have opened a Draft Session for its earlier Workspace.
	var scratch []string
	for _, params := range initializeRequests(t, h) {
		if strings.Contains(params, "droi.scratch") {
			scratch = append(scratch, params)
		}
	}
	if len(scratch) != 1 {
		t.Fatalf("%d Scratch Sessions created: %v", len(scratch), scratch)
	}
	if err := json.Unmarshal([]byte(scratch[0]), &p); err != nil {
		t.Fatal(err)
	}
	if p.Cwd != made {
		t.Fatalf("the Session works in %q, want the folder the Gateway made, %s", p.Cwd, made)
	}
	if info, err := os.Stat(p.Cwd); err != nil || !info.IsDir() {
		t.Fatalf("the folder is not there: %v", err)
	}
	if len(p.Tags) != 1 || p.Tags[0]["name"] != "droi.scratch" {
		t.Fatalf("tags %v", p.Tags)
	}
}

func TestAGatewayWithoutAScratchFolderLeavesTheStartPageWithTheReason(t *testing.T) {
	h := startHarnessGateway(t, draftScenario(), "", "", func(o *gateway.Options) { o.Scratch = nil }, nil)
	startScratchSession(t, h)
	h.until("the reason", func() bool { return h.hasText("This computer has no Scratch folder.") })
	if h.a.Route().Name != "new" {
		t.Fatalf("the window left the start page for %q", h.a.Route().Name)
	}
	for _, params := range initializeRequests(t, h) {
		if strings.Contains(params, "droi.scratch") {
			t.Fatalf("a Scratch Session was made: %s", params)
		}
	}
}

func TestACreatedFolderThatTheGatewayRefusesToMakeShowsItsReason(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h.scratchRoot = filepath.Join(blocker, "chats")
	startScratchSession(t, h)
	h.until("the reason", func() bool { return h.hasText("not a directory") })
}

func TestCopySessionDetailsHasTheTranscriptLocationTheGatewayGives(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Fix the login race")
	id := h.a.route.SessionID
	const file = "/srv/factory/sessions/-Users-dev-acme/transcript.jsonl"
	h.mu.Lock()
	h.transcripts[id] = file
	h.mu.Unlock()

	h.click("Session actions")
	h.click("Copy session details")
	h.until("the details", func() bool { return strings.Contains(h.tt.Clipboard(), "Transcript: "+file) })
	if got := h.tt.Clipboard(); !strings.Contains(got, id) {
		t.Fatalf("the details %q do not name the Session", got)
	}
}

func TestCopySessionDetailsWithoutATranscriptStillCopiesTheRest(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSession("Fix the login race")
	id := h.a.route.SessionID
	h.click("Session actions")
	h.click("Copy session details")
	h.until("the details", func() bool { return strings.Contains(h.tt.Clipboard(), id) })
	if got := h.tt.Clipboard(); strings.Contains(got, "Transcript") {
		t.Fatalf("the details %q name a transcript the Gateway does not have", got)
	}
}
