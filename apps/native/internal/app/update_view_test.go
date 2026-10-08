package app

import (
	"context"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/updates"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
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
	if s := next.CheckAndInstall(context.Background()); s.Status == updates.Ready {
		t.Fatal("the next launch installed the skipped version")
	}
}
