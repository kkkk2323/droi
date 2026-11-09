package app

import (
	"context"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/internal/updates"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/prefs"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

type testRelease struct{}

func (testRelease) Version() string { return "9.9.9" }
func (testRelease) Notes() string   { return "- Copy a reply as Markdown" }
func (testRelease) Install(_ context.Context, p func(int64, int64)) error {
	p(1, 1)
	return nil
}

type testSource struct{ rel updates.Release }

func (s testSource) Check(context.Context) (updates.Release, error) { return s.rel, nil }

func updateHarness(t *testing.T, focused *bool, relaunched *int) (*harness, *updates.Updater) {
	t.Helper()
	u := updates.New(testSource{rel: testRelease{}})
	h := newHarnessWith(t, fakedaemon.Scenario{}, "", func(cfg *Config) {
		cfg.Updater = u
		cfg.Relaunch = func() { *relaunched++ }
		cfg.Focused = func() bool { return *focused }
	})
	return h, u
}

func TestTheAboutRowWalksTheUpdate(t *testing.T) {
	focused, relaunched := true, 0
	h, u := updateHarness(t, &focused, &relaunched)
	h.a.Go(Route{Name: "settings", Tab: "about"})
	h.settle()
	h.click("Check for updates")
	h.until("an update is found", func() bool { return u.State().Status == updates.Available })
	h.settle()
	if !h.hasText("9.9.9 is available") {
		t.Fatalf("the About row does not say an update is available: %q", h.tt.Texts())
	}
	h.click("Update to 9.9.9")
	h.until("the update is installed", func() bool { return u.State().Status == updates.Ready })
	h.settle()
	if !h.hasText("9.9.9 is installed") {
		t.Fatal("the About row does not say the update is installed")
	}
	h.click("Restart to update")
	if relaunched != 1 {
		t.Fatalf("Restart relaunched %d times", relaunched)
	}
}

func TestTheCornerCardOffersTheUpdate(t *testing.T) {
	focused, relaunched := true, 0
	h, u := updateHarness(t, &focused, &relaunched)
	u.Check(context.Background())
	h.settle()
	if _, ok := h.tt.Find("Update"); !ok || !h.hasText("Droi 9.9.9 is available") {
		t.Fatal("no update card")
	}
	u.Install(context.Background())
	h.settle()
	if !h.hasText("Droi 9.9.9 is ready") {
		t.Fatal("the card does not say the update is ready")
	}
	h.click("Dismiss")
	h.settle()
	if h.hasText("Droi 9.9.9 is ready") {
		t.Fatal("Dismiss kept the card")
	}
}

func TestAnInstalledUpdateRestartsOnlyWhenIdle(t *testing.T) {
	focused, relaunched := true, 0
	h, u := updateHarness(t, &focused, &relaunched)
	u.SetAutomatic(true)
	if h.a.RelaunchIfIdle() {
		t.Fatal("relaunched with nothing installed")
	}
	u.Check(context.Background())
	u.Install(context.Background())
	if h.a.RelaunchIfIdle() {
		t.Fatal("relaunched while the window has the focus")
	}
	focused = false
	if !h.a.RelaunchIfIdle() || relaunched != 1 {
		t.Fatal("did not relaunch an idle app in the background")
	}
}

// Updates are not automatic until About's switch turns them on: a
// background check leaves the release on the card for Update or Skip, and
// an installed one waits for Restart now.
func TestUpdatesWaitForTheUserUntilTheSwitchIsOn(t *testing.T) {
	focused, relaunched := false, 0
	h, u := updateHarness(t, &focused, &relaunched)
	if u.Automatic() {
		t.Fatal("updates start automatic")
	}
	if s := u.BackgroundCheck(context.Background()); s.Status != updates.Available {
		t.Fatalf("the background check went to %+v", s)
	}
	h.settle()
	if !h.hasText("Droi 9.9.9 is available") || !h.hasButton("Skip this version") {
		t.Fatalf("no card asking: %q", h.tt.Texts())
	}
	u.Install(context.Background())
	if h.a.RelaunchIfIdle() || relaunched != 0 {
		t.Fatal("relaunched without asking")
	}
	h.a.Go(Route{Name: "settings", Tab: "about"})
	h.settle()
	clickSwitchBeside(h, []string{"When off, Droi asks before it updates"}, func() bool { return prefs.AutoUpdate.Get(h.a.prefs) })
	if !u.Automatic() {
		t.Fatal("the switch did not turn automatic updates on")
	}
	if !h.a.RelaunchIfIdle() || relaunched != 1 {
		t.Fatal("did not relaunch once updates are automatic")
	}
}

// An app's Presets can turn automatic updates on, here and after an
// update, until the user turns them off: then they stay off.
func TestPresetsTurnAutomaticUpdatesOnUntilTheUserTurnsThemOff(t *testing.T) {
	focused, relaunched := false, 0
	u := updates.New(testSource{rel: testRelease{}})
	presets := &defaults.Presets{AutoUpdate: true}
	h := newHarnessWith(t, fakedaemon.Scenario{}, "", func(cfg *Config) {
		cfg.Updater = u
		cfg.Relaunch = func() { relaunched++ }
		cfg.Focused = func() bool { return focused }
		cfg.Presets = presets
	})
	if !u.Automatic() {
		t.Fatal("the presets did not turn automatic updates on")
	}
	if _, ok := h.cfg.Prefs.Get(prefs.AutoUpdate.Key); ok {
		t.Fatal("the presets were saved as the user's choice")
	}
	h.a.Go(Route{Name: "settings", Tab: "about"})
	h.settle()
	clickSwitchBeside(h, []string{"When off, Droi asks before it updates"}, func() bool { return !u.Automatic() })
	if prefs.AutoUpdate.Get(h.cfg.Prefs) {
		t.Fatal("the switch did not keep automatic updates off")
	}
	next := updates.New(testSource{rel: testRelease{}})
	New(Config{Prefs: h.cfg.Prefs, Updater: next, Presets: presets})
	if next.Automatic() {
		t.Fatal("the presets overrode the user's choice")
	}
}

// The About row shows what the release changes, and Skip this version
// on the card hides it and keeps the background check from installing it,
// after a restart too.
func TestReleaseNotesAndSkipThisVersion(t *testing.T) {
	focused, relaunched := true, 0
	h, u := updateHarness(t, &focused, &relaunched)
	u.Check(context.Background())
	h.a.Go(Route{Name: "settings", Tab: "about"})
	h.settle()
	if !h.hasText("What’s new in 9.9.9") || !h.hasText("Copy a reply as Markdown") {
		t.Fatalf("no release notes: %q", h.tt.Texts())
	}
	h.a.Go(Route{Name: "home"})
	h.settle()
	h.click("Skip this version")
	h.settle()
	if h.hasText("Droi 9.9.9 is available") {
		t.Fatal("Skip this version kept the card")
	}
	if got := prefs.SkippedUpdate.Get(h.a.prefs); got != "9.9.9" {
		t.Fatalf("skipped %q", got)
	}
	next := updates.New(testSource{rel: testRelease{}})
	New(Config{Prefs: h.a.prefs, Updater: next})
	next.SetAutomatic(true)
	if s := next.BackgroundCheck(context.Background()); s.Status == updates.Ready {
		t.Fatal("the next launch installed the skipped version")
	}
}

// On a Kernel an installed update that has waited long enough for the Sessions restarts anyway, in the background:
// the Kernel resumes the runs it interrupts (ADR 0038). On the Daemon it keeps waiting.
func TestAnUpdateStopsWaitingForTheSessionsOnAKernel(t *testing.T) {
	for _, onKernel := range []bool{true, false} {
		t.Run(map[bool]string{true: "kernel", false: "daemon"}[onKernel], func(t *testing.T) {
			if !onKernel {
				keepOnDaemon(t, "the Daemon is what this case checks")
			}
			focused, relaunched := false, 0
			now := time.Now()
			u := updates.New(testSource{rel: testRelease{}})
			h := newHarnessWith(t, oneSession(), "", func(cfg *Config) {
				cfg.Updater = u
				cfg.Relaunch = func() { relaunched++ }
				cfg.Focused = func() bool { return focused }
				cfg.Now = func() time.Time { return now }
			})
			if onKernel && h.kernel == nil {
				t.Skip("no Kernel for the window")
			}
			u.SetAutomatic(true)
			u.Check(context.Background())
			u.Install(context.Background())
			h.a.mu.Lock()
			if h.a.busy == nil {
				h.a.busy = map[string]string{}
			}
			h.a.busy[h.d.Sessions[0].SessionID] = string(protocol.DroidWorkingStateStreamingAssistantMessage)
			h.a.mu.Unlock()
			if h.a.RelaunchIfIdle() {
				t.Fatal("relaunched at once while a Session works")
			}
			now = now.Add(updateWaitLimit - time.Minute)
			if h.a.RelaunchIfIdle() {
				t.Fatal("relaunched before the wait was over")
			}
			now = now.Add(time.Minute)
			focused = true
			if h.a.RelaunchIfIdle() {
				t.Fatal("relaunched with the window in front")
			}
			focused = false
			if got := h.a.RelaunchIfIdle(); got != onKernel || relaunched != map[bool]int{true: 1, false: 0}[onKernel] {
				t.Fatalf("after the wait: relaunched %v (%d times)", got, relaunched)
			}
		})
	}
}
