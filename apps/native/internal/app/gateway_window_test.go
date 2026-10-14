package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

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
