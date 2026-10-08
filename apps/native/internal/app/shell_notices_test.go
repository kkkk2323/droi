package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/alerts"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// testHost is a Host in throwaway folders: no login, no API key, no
// Daemon started.
func testHost(t *testing.T) *host.Host {
	t.Helper()
	home := t.TempDir()
	return host.New(host.Config{UserData: t.TempDir(), Home: home, Env: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, MoveToTrash: os.Remove})
}

func oneSession() fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}}}
}

func TestSignInPageUntilThereIsACredential(t *testing.T) {
	h0 := testHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the sign-in page", func() bool { return h.hasText("Sign in to Droi") })
	if h.hasText("New session") {
		t.Fatal("the sessions show before Droi can authenticate")
	}
	h.click("Factory API key")
	h.tt.Type("fk-x")
	h.click("Continue with API key")
	h.until("the app", func() bool { return !h.hasText("Sign in to Droi") && h.hasText("New session") })
	if h0.Settings.APIKey() != "fk-x" {
		t.Fatalf("stored key %q", h0.Settings.APIKey())
	}
}

func TestSignInPageKeepsSettingsReachable(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t) })
	h.until("the sign-in page", func() bool { return h.hasText("Sign in to Droi") })
	h.click("Settings")
	if r := h.a.Route(); r.Name != "settings" || r.Tab != "advanced" {
		t.Fatalf("Settings went to %+v", r)
	}
	h.click("Back")
	h.until("the sign-in page again", func() bool { return h.hasText("Sign in to Droi") })
}

func TestStartingUpWaitsForTheFirstConnection(t *testing.T) {
	if !startingUp(controller.Status{}, false) {
		t.Error("not connected yet should be starting up")
	}
	if startingUp(controller.Status{Connected: true}, true) || startingUp(controller.Status{Reconnecting: true}, true) {
		t.Error("a connection lost later is not starting up")
	}
}

func TestStartingUpSaysWhyDroidIsMissing(t *testing.T) {
	h0 := testHost(t)
	h0.Settings.Update(func(s *host.Settings) { s.APIKey = host.OptString("fk-x") })
	idle := controller.New(controller.Config{ResolveURL: h0.WaitForDaemonURL})
	t.Cleanup(func() { idle.Close() })
	h0.Start()
	t.Cleanup(h0.Stop)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host, cfg.Controller = h0, idle })
	h.until("the reason", func() bool { return h.hasText("droid was not found") })
	if h.hasText("Starting the Daemon") {
		t.Fatal("still says it is starting")
	}
	h.click("Settings")
	if r := h.a.Route(); r.Name != "settings" {
		t.Fatalf("Settings went to %+v", r)
	}
}

func TestStartingUpSaysWhyItCannotConnect(t *testing.T) {
	h0 := testHost(t)
	h0.Settings.Update(func(s *host.Settings) { s.APIKey = host.OptString("fk-x") })
	rejected := controller.New(controller.Config{
		URL:                "ws://127.0.0.1:1",
		MaxConnectAttempts: 1,
		Credential:         func(context.Context) (*droid.Credential, error) { return nil, errors.New("no login") },
	})
	t.Cleanup(func() { rejected.Close() })
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host, cfg.Controller = h0, rejected })
	if err := rejected.Connect(context.Background()); err == nil {
		t.Fatal("connected to nothing")
	}
	h.until("the reason", func() bool { return h.hasText("Droi could not connect to the Daemon") })
}

func TestCustomSoundIsPlayed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ding.wav")
	p := alertPrefs{CompletionSound: "custom", CustomCompletionSound: &path, AwaitingInputSound: "custom"}
	if got := soundFor(p, alerts.Completion); got != path {
		t.Errorf("completion: %q", got)
	}
	if got := soundFor(p, alerts.AwaitingInput); got != "bell" {
		t.Errorf("custom without a file falls back to the bell, got %q", got)
	}
}
