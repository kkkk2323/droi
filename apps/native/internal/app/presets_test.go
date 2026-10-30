package app

import (
	"path/filepath"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

const updateDefaults = "daemon.update_session_defaults"

func anlanPresets() *defaults.Presets {
	model, effort, limit := "gpt-6.1-sol", "medium", 400_000
	return &defaults.Presets{
		Version:  1,
		ToolMode: defaults.ScriptOnly,
		Session:  defaults.Patch{ModelID: &model, ReasoningEffort: &effort, CompactionTokenLimit: &limit},
	}
}

// An app's Presets reach a new install once, when the window first
// reaches this computer's Daemon: the Session Defaults and this device's
// tool mode. The version is kept, so a window opened later leaves what the
// user changed since.
func TestPresetsApplyOnceToANewInstall(t *testing.T) {
	h := newHarnessWith(t, sessionsScenario(), "", func(c *Config) { c.Presets = anlanPresets() })
	p := h.sent(updateDefaults, func(map[string]any) bool { return true })
	if p["modelId"] != "gpt-6.1-sol" || p["reasoningEffort"] != "medium" || p["compactionTokenLimit"] != float64(400_000) {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.until("the presets kept", func() bool {
		return prefs.DefaultToolMode.Get(h.cfg.Prefs) == string(defaults.ScriptOnly) && prefs.PresetsVersion.Get(h.cfg.Prefs) == 1
	})

	h.switchTo(nil)
	h.until("the new window's list", func() bool { return h.hasText("Fix the login race") })
	h.settle()
	if n := h.count(updateDefaults); n != 1 {
		t.Fatalf("the presets were sent %d times", n)
	}
}

// Presets of a newer version reach an install that has the older ones.
func TestNewerPresetsApplyAgain(t *testing.T) {
	presets := anlanPresets()
	presets.Version = 2
	h := newHarnessWith(t, sessionsScenario(), "", func(c *Config) {
		prefs.PresetsVersion.Set(c.Prefs, 1)
		c.Presets = presets
	})
	h.sent(updateDefaults, func(map[string]any) bool { return true })
	h.until("the newer version kept", func() bool { return prefs.PresetsVersion.Get(h.cfg.Prefs) == 2 })
}

// Presets are this computer's: a window that shows another computer
// leaves its Daemon's defaults alone.
func TestPresetsLeaveAnotherComputerAlone(t *testing.T) {
	o := startOtherComputer(t, oneSession(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, func(c *Config) { c.Presets = anlanPresets() })
	h.until("the other computer's list", func() bool { return h.hasText("One") })
	h.settle()
	reqs, _ := o.d.Requests()
	for _, r := range reqs {
		if r.Method == updateDefaults {
			t.Fatalf("the other computer got the presets: %s", r.Params)
		}
	}
}

// With no Workspace at all, as on a new install, the New session page
// starts without a project instead of waiting for one to be chosen.
func TestWithNoWorkspaceTheNewSessionStartsWithoutAProject(t *testing.T) {
	made := filepath.Join(t.TempDir(), "chats", "2026-10-30-0a1b2c")
	sc := fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"On it."}}, ValidDirectories: []string{made}}
	h := startHarnessGateway(t, sc, "", "", func(o *gateway.Options) { o.Scratch = madeFolder(made) }, nil)
	h.until("the question without a project", func() bool { return h.hasText("What should we work on?") })
	h.typeInto("Plan the release")
	h.click("Start session")
	h.until("the new Session", func() bool { return h.a.Route().Name == "session" })
}
