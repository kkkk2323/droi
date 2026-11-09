package app

import (
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
)

// TestOnKernelNoFactoryAccountIsAsked: a window on the Kernel whose Host has no Factory login and no API key opens on
// its Sessions, not on Droi's sign-in, and Settings has no Account tab: the Model Sources run its models, not an
// account.
func TestOnKernelNoFactoryAccountIsAsked(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t) })
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the Kernel", func() bool { return h.a.onKernel() })
	h.until("the Session list", func() bool { return h.hasText("One") })
	if h.hasText("Sign in to Droi") {
		t.Fatal("the window asks for a Factory sign-in on the Kernel")
	}
	h.openSettingsTab("account")
	h.until("Settings", func() bool { return h.hasButton("Advanced") })
	if h.hasButton("Account") || h.hasText("Factory API key") {
		t.Fatalf("Settings offers the Factory account on the Kernel: %q", h.tt.Texts())
	}
}

// Every Model Source that offers nothing says what gives it models, and one that offers some, nothing.
func TestEachModelSourceSaysWhatGivesItModels(t *testing.T) {
	h := testHost(t)
	url := h.MagpieBaseURL()
	for _, tc := range []struct {
		s    host.ModelSourceStatus
		want string
	}{
		{host.ModelSourceStatus{ID: host.SourceFactory, State: host.SourceEmpty}, "Add a Factory key on Settings → Usage."},
		{host.ModelSourceStatus{ID: host.SourceFactory, State: host.SourceUnreachable, Err: "port in use."}, "The Key Pool did not start: port in use."},
		{host.ModelSourceStatus{ID: host.SourceFactory, State: host.SourceReady, Models: 30}, ""},
		{host.ModelSourceStatus{ID: host.SourceMagpie, State: host.SourceOff}, "Or turn on Magpie in Settings → Advanced."},
		{host.ModelSourceStatus{ID: host.SourceMagpie, State: host.SourceUnreachable}, "Magpie does not answer at " + url + ": start it, then refresh in Settings → Advanced."},
		{host.ModelSourceStatus{ID: host.SourceMagpie, State: host.SourceEmpty}, "Magpie lists no models."},
		{host.ModelSourceStatus{ID: host.SourceMagpie, State: host.SourceReady, Models: 3}, ""},
	} {
		if got := sourceFix(h, tc.s); got != tc.want {
			t.Errorf("%s %s: %q, want %q", tc.s.ID, tc.s.State, got, tc.want)
		}
	}
	for _, tc := range []struct {
		s        host.ModelSourceStatus
		ok       bool
		pill     string
		descHas  string
		titleHas string
	}{
		{host.ModelSourceStatus{ID: host.SourceFactory, State: host.SourceReady, Models: 1}, true, "1 model", "Key Pool", "Factory"},
		{host.ModelSourceStatus{ID: host.SourceFactory, State: host.SourceEmpty}, false, "No key", "Usage tab", "Factory"},
		{host.ModelSourceStatus{ID: host.SourceMagpie, State: host.SourceReady, Models: 12}, true, "12 models", url, "Magpie"},
		{host.ModelSourceStatus{ID: host.SourceMagpie, State: host.SourceOff}, false, "Off", "Turn on Magpie", "Magpie"},
		{host.ModelSourceStatus{ID: host.SourceMagpie, State: host.SourceUnreachable, Models: 4}, false, "Not reachable", "stay offered", "Magpie"},
	} {
		title, ok, pill, desc := sourceRow(h, tc.s)
		if ok != tc.ok || pill != tc.pill || !strings.Contains(desc, tc.descHas) || title != tc.titleHas {
			t.Errorf("%s %s: %q %v %q %q", tc.s.ID, tc.s.State, title, ok, pill, desc)
		}
	}
}

// With no source on this computer to ask, the notice names both ways to models.
func TestTheNoModelsNoticeNamesBothSourcesWithoutAHost(t *testing.T) {
	a := &App{}
	got := a.noKernelModels()
	if !strings.Contains(got, "Usage") || !strings.Contains(got, "Magpie") {
		t.Fatalf("the notice: %q", got)
	}
}
