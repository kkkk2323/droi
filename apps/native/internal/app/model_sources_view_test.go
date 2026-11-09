package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/compaction"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
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

// TestOnKernelTheSidebarHasNoWorkstreams: Workstreams still run on the Daemon, so a window on the Kernel neither lists
// them nor offers to start one.
func TestOnKernelTheSidebarHasNoWorkstreams(t *testing.T) {
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Go on"}}}}}
	h := workstreamsHarnessOf(t, sc, &fakeWorkstreams{}, nil)
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the Kernel", func() bool { return h.a.onKernel() })
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if h.hasText("Workstreams") || h.finds("New Workstream") {
		t.Fatalf("the sidebar offers Workstreams on the Kernel: %q", h.tt.Texts())
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

// TestOnKernelAdvancedHasNoDaemon: on the Kernel, Settings → Advanced shows the Kernel and its model sources, and
// nothing of the Daemon: no status of it, no droid to find, no Factory API base URL to send it to.
func TestOnKernelAdvancedHasNoDaemon(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t) })
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the Kernel", func() bool { return h.a.onKernel() })
	h.openSettingsTab("advanced")
	h.until("the Kernel's row", func() bool { return h.finds("Restart Kernel") })
	for _, gone := range []string{"droid executable", "Restart Daemon", "Factory API base URL", "Daemon log"} {
		if h.hasText(gone) || h.finds(gone) {
			t.Errorf("Advanced shows the Daemon's %q on the Kernel", gone)
		}
	}
	if !h.hasText("Offer Magpie's models") {
		t.Errorf("Advanced lost Magpie: %q", h.tt.Texts())
	}
}

// TestOnKernelSessionDefaultsLeaveOutTheDaemons: the Session defaults on the Kernel are what a Kernel conversation
// starts with: no Spec mode, Daemon compaction or subagent settings, and no Script or Direct: codemode's mode, on or
// only, and the model that writes compaction summaries are the Kernel's.
func TestOnKernelSessionDefaultsLeaveOutTheDaemons(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t) })
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the Kernel", func() bool { return h.a.onKernel() })
	h.openSettingsTab("defaults")
	h.until("the defaults", func() bool { return h.hasText("Codemode") && h.hasText("Model and autonomy") })
	if !h.hasText("Summary model") {
		t.Error("the Session defaults on the Kernel have no compaction model")
	}
	for _, gone := range []string{"Spec mode", "Compact automatically", "Token limit", "Subagents", "Interaction mode", "Follow droid settings", "Script models", "Shared with the droid CLI", "Tool calls"} {
		for _, s := range h.tt.Texts() {
			if strings.Contains(s, gone) {
				t.Errorf("the Session defaults show %q on the Kernel", gone)
				break
			}
		}
	}
}

// TestOnKernelCompactAsksTheKernel: /compact in a Session on the Kernel starts a compaction in the Kernel, which
// every connection hears as a task, and asks the Daemon nothing.
func TestOnKernelCompactAsksTheKernel(t *testing.T) {
	h := kernelHarness(t)
	watch := h.testConn()
	h.openSession("Fix the login race")
	h.until("the transcript", func() bool { return h.hasText("Message") })
	conv, ok := h.a.kernelID(h.a.route.SessionID)
	if !ok {
		t.Fatal("the Session is not the Kernel's")
	}
	h.click("Message")
	h.tt.Type("/compact keep the test names")
	h.tt.Key(0, ui.KeyEnter)
	h.frame()
	deadline := time.After(15 * time.Second)
	for compacting := false; !compacting; {
		select {
		case n, ok := <-watch.Notifications():
			if !ok {
				t.Fatal("the test's connection closed")
			}
			var task kernel.TaskChanged
			if n.Method == kernel.NotifyTaskChanged && n.Decode(&task) == nil && task.ConversationID == conv && task.Kind == "compaction" {
				compacting = true
			}
		case <-deadline:
			t.Fatal("the Kernel started no compaction")
		}
	}
	if n := h.count("daemon.compact_session"); n != 0 {
		t.Errorf("the Daemon was asked to compact %d times", n)
	}
	// The Session is short: the whole of it is within what a summary keeps, and the footer says so.
	h.until("the footer's notice", func() bool { return h.hasText("Nothing to compact yet: the newest 20,000 tokens are kept as they are") })
}

// TestOnKernelCompactedSaysItOnce: a compaction on the Kernel that summarised is said by its row in the transcript,
// not again in the footer.
func TestOnKernelCompactedSaysItOnce(t *testing.T) {
	h := kernelHarness(t)
	h.openSession("Fix the login race")
	h.until("the transcript", func() bool { return h.hasText("Message") })
	id := h.a.route.SessionID
	h.a.compactions.Start(id)
	h.a.compactions.FinishDone(id, id, compaction.Done{FinishedAt: time.Now().UnixMilli(), Kernel: true})
	h.frame()
	if h.hasText("Compacted, older messages summarised") {
		t.Error("the footer says the compaction the transcript says")
	}
	h.a.compactions.Start(id)
	h.a.compactions.FinishDone(id, id, compaction.Done{FinishedAt: time.Now().UnixMilli(), Kernel: true, Nothing: true})
	h.until("the footer's notice", func() bool { return h.hasText("Nothing to compact yet: the newest 20,000 tokens are kept as they are") })
}

// TestOnKernelCodemodeIsOnOrOnly: Settings → Session defaults on the Kernel sets codemode's mode, on or only, in the
// Host's settings, which is what reaches the Kernel.
func TestOnKernelCodemodeIsOnOrOnly(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t) })
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.until("the Kernel", func() bool { return h.a.onKernel() })
	h.openSettingsTab("defaults")
	h.until("the Codemode group", func() bool { return h.hasText("Codemode") })
	h.click("Codemode mode")
	h.click("only")
	if got := h.a.cfg.Host.Settings.Get().CodemodeMode; got != "only" {
		t.Fatalf("codemode's mode is %q", got)
	}
}

func TestContextWindowsAreTypedAsPeopleWriteThem(t *testing.T) {
	for in, want := range map[string]int{"": 0, "200000": 200_000, "200,000": 200_000, "200k": 200_000, "1.5m": 1_500_000, " 64K ": 64_000} {
		if got, ok := parseTokens(in); !ok || got != want {
			t.Errorf("%q is %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"abc", "-5", "0"} {
		if _, ok := parseTokens(in); ok {
			t.Errorf("%q was taken", in)
		}
	}
	if got := groupedTokens(1_000_000); got != "1,000,000" {
		t.Errorf("grouped %q", got)
	}
}
