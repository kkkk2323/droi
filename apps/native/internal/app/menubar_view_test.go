package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// Advanced's switch puts the Key Pool in the menu bar, whose panel shows
// the pools and the keys, and opens the Usage tab.
func TestTheMenuBarItemShowsTheKeyPool(t *testing.T) {
	h0 := keyPoolTestHost(t)
	var fitted float32
	opened := 0
	bar := &MenuBar{
		Fit:       func(_, height float32) { fitted = height },
		OpenUsage: func() { opened++ },
	}
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) {
		cfg.Host = h0
		cfg.MenuBar = bar
	})
	h.until("the session list", func() bool { return h.hasText("One") })
	h.click("Settings")
	h.click("Advanced")
	h.until("the switch", func() bool { _, ok := h.tt.Find("Show usage in the menu bar"); return ok })
	if h.a.MenuBarShown() {
		t.Fatal("the item shows before the switch is on")
	}
	h.clickSwitch("Show usage in the menu bar", func() bool { return prefs.MenuBarUsage.Get(h.a.prefs) })
	if !h.a.MenuBarShown() {
		t.Fatal("the switch did not turn the item on")
	}
	if item := h.a.MenuBarItem(); item.Title != "--" {
		t.Fatalf("an empty pool's item = %+v", item)
	}

	panel := ui.NewTester(h.a.MenuBarPanel, MenuBarPanelWidth, 900)
	panel.Frame()
	if !hasAny(panel.Texts(), "The Key Pool has no key yet. Open Usage to add one.") {
		t.Fatalf("the empty panel: %q", panel.Texts())
	}

	first := "fk-" + strings.Repeat("a", 20) + "1111"
	second := "fk-" + strings.Repeat("b", 20) + "2222"
	for _, k := range []string{first, second} {
		if err := h0.AddPoolKey(context.Background(), k); err != nil {
			t.Fatal(err)
		}
	}
	h.a.refreshPool()
	h.until("the item's quota", func() bool { return h.a.MenuBarItem().Title == "70%" })
	panel.Frame()
	panel.Frame()
	for _, want := range []string{"Key Pool", keys.MaskKey(first), keys.MaskKey(second), "Standard", "Core", "2 keys"} {
		if !hasAny(panel.Texts(), want) {
			t.Fatalf("no %q in the panel: %q", want, panel.Texts())
		}
	}
	if fitted <= 0 {
		t.Fatal("the panel was not fitted to its content")
	}
	if err := panel.Click("Open Usage"); err != nil {
		t.Fatal(err)
	}
	panel.Frame()
	if opened != 1 {
		t.Fatalf("Open Usage opened %d times", opened)
	}
	h.a.OpenUsage()
	h.until("the Usage tab", func() bool { return h.hasText("Keys and quota") })

	// Turning the Key Pool off takes the item away with it.
	h0.DisableDroiDroid()
	h.until("the item to go", func() bool { return !h.a.MenuBarShown() })
}

// clickSwitch clicks the switch on the row titled title: the row's title
// shows the same words as the switch's name, so it is found by moving along
// the row from the right until on says it turned. A switch whose action runs
// in the background may turn only after the last click: the clicks never
// yield, so on one CPU that action waits for the scheduler to preempt them.
func (h *harness) clickSwitch(title string, on func() bool) {
	h.t.Helper()
	r, ok := h.tt.Find(title)
	for i := 0; ok && r.Y+r.H > refH-8 && i < 20; i++ {
		h.tt.Scroll(refW/2, refH/2, 0, 200)
		h.frame()
		r, ok = h.tt.Find(title)
	}
	if !ok {
		h.t.Fatalf("no row %q", title)
	}
	// The switch is centred on the title and the description under it; a
	// click is heard on the frame after it.
	for _, dy := range []float32{r.H / 2, r.H, r.H * 1.5, r.H * 2} {
		// Clear of the scroll bar, whose track pages the settings.
		for x := float32(refW - 24); x > r.X+r.W; x -= 4 {
			h.tt.ClickAt(x, r.Y+dy)
			h.frame()
			h.frame()
			if on() {
				return
			}
		}
	}
	h.until("the switch on the row "+title+" to turn", on)
}

// The panel with a running Key Pool in it, in Chinese, as droid-proxy's
// drop-down shows it.
func TestTheMenuBarPanelWithData(t *testing.T) {
	h0 := keyPoolTestHost(t)
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			h := newHarnessWith(t, oneSession(), theme, func(cfg *Config) {
				cfg.Host = h0
				cfg.MenuBar = &MenuBar{}
				prefs.Language.Set(cfg.Prefs, string(l10n.Chinese))
			})
			t.Cleanup(func() { l10n.Set("", "") })
			h.until("the session list", func() bool { return h.hasText("One") })
			usageSample(h.a, time.Now())
			panel := ui.NewTester(h.a.MenuBarPanel, MenuBarPanelWidth, 900)
			for range 5 {
				panel.Frame()
			}
			for _, want := range []string{"Key 池", "今天已用", "每天上限", "即将作废", "fk-4f2a1...91be", "4 把 key · 2 把需处理", "缓存预热", "额度更新于"} {
				if !hasAny(panel.Texts(), want) {
					t.Errorf("no %q in the panel: %q", want, panel.Texts())
				}
			}
			if item := h.a.MenuBarItem(); item.Title == "--" || item.Icon == llmusage.TrayUnknown {
				t.Errorf("the item = %+v", item)
			}
			if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				writePNG(filepath.Join(dir, "menubar-panel-"+theme+".png"), panel.Image())
			}
		})
	}
}

func hasAny(texts []string, sub string) bool {
	for _, s := range texts {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
