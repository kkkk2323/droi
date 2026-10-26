package app

import (
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

type fakeMemory struct {
	mu           sync.Mutex
	ov           memorywork.Overview
	consolidated []string
	reset        int
	fail         error
}

func (m *fakeMemory) Overview() (memorywork.Overview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ov, nil
}
func (m *fakeMemory) Consolidate(w string) (memorywork.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consolidated = append(m.consolidated, w)
	if m.fail != nil {
		return memorywork.Result{}, m.fail
	}
	return memorywork.Result{Applied: 2, Rejected: 1}, nil
}
func (m *fakeMemory) ResetPrompts() error { m.reset++; return nil }
func (m *fakeMemory) Folder() string      { return "/tmp/memory" }

func memoryHarness(t *testing.T, m *fakeMemory, on bool) (*harness, *host.Host) {
	t.Helper()
	h0 := testHost(t)
	// A key, or the window is the sign-in page.
	_ = h0.Settings.Update(func(s *host.Settings) { s.APIKey = host.OptString("fk-test"); s.MemoryEnabled = on })
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0; cfg.Memory = m })
	return h, h0
}

func TestSettingsMemoryListsEachMemory(t *testing.T) {
	m := &fakeMemory{ov: memorywork.Overview{LoggedSince: "2026-09-01T10:00:00Z", Rows: []memorywork.Row{
		{Workspace: "", Entries: 3, Chars: 420, SoftLimit: 40000},
		{Workspace: "/Users/dev/acme-web", Entries: 12, Chars: 210000, SoftLimit: 200000, OverSoftLimit: true, Searches: 5, EmptySearches: 2, NeverFound: 4},
	}}}
	h, _ := memoryHarness(t, m, true)
	h.a.Go(Route{Name: "settings", Tab: "memory"})
	h.settle()
	for _, want := range []string{"Global Memory", "3 entries · 420 of 40k characters · never consolidated", "/Users/dev/acme-web",
		"12 entries · 210k of 200k characters", "5 searches since", "2 found nothing", "4 entries never found",
		"Over the size limit. Consolidate to continue saving new entries."} {
		if !h.hasText(want) {
			t.Errorf("missing %q in %q", want, h.tt.Texts())
		}
	}
	h.click("Consolidate /Users/dev/acme-web")
	h.until("the consolidation's result", func() bool {
		return h.hasText("Consolidated 2; 1 kept unchanged because the model’s output failed validation.")
	})
	if len(m.consolidated) != 1 || m.consolidated[0] != "/Users/dev/acme-web" {
		t.Errorf("consolidated %q", m.consolidated)
	}
	h.tt.Scroll(refW/2, refH/2, 0, 2000)
	h.settle()
	h.click("Reset prompts to default")
	h.frame()
	if m.reset != 1 || !h.hasText("back to Droi’s") {
		t.Error("Reset prompts did nothing")
	}
}

func clickMemorySwitch(h *harness, asked func() bool) {
	h.t.Helper()
	clickSwitchBeside(h, []string{"Off: Droid starts", "On: Droid can record"}, asked)
}

// clickSwitchBeside clicks a settings switch named like its section in the
// list on the left: it sits at the right end of the row whose description
// starts with one of descs.
func clickSwitchBeside(h *harness, descs []string, done func() bool) {
	h.t.Helper()
	var d ui.Rect
	for _, s := range descs {
		for _, text := range h.tt.Texts() {
			if strings.HasPrefix(text, s) {
				d, _ = h.tt.Find(text)
			}
		}
	}
	if d.W == 0 {
		h.t.Fatalf("no row described %q", descs)
	}
	for y := d.Y - 22; y < d.Y+d.H; y += 4 {
		h.tt.ClickAt(d.X+d.W+16+18, y)
		h.frame()
		if done() {
			return
		}
	}
	h.t.Fatal("the switch did not answer")
}

func TestTheMemorySwitchAsksBeforeRestarting(t *testing.T) {
	h, h0 := memoryHarness(t, &fakeMemory{}, false)
	h.a.Go(Route{Name: "settings", Tab: "memory"})
	h.settle()
	if !h.hasText("Nothing remembered yet") {
		t.Errorf("no empty state: %q", h.tt.Texts())
	}
	clickMemorySwitch(h, func() bool { return h.hasText("Turn Memory on? This restarts the Daemon") })
	if h0.Settings.Get().MemoryEnabled {
		t.Fatal("the switch did not ask first")
	}
	h.click("Cancel")
	h.frame()
	if h.hasText("Turn Memory on?") {
		t.Fatal("Cancel kept the question")
	}
	clickMemorySwitch(h, func() bool { return h.hasText("Turn Memory on?") })
	h.click("Restart Daemon")
	h.frame()
	if !h0.Settings.Get().MemoryEnabled {
		t.Fatal("Restart Daemon did not turn Memory on")
	}
}

func TestALargeProjectMemoryIsAnnouncedOnce(t *testing.T) {
	m := &fakeMemory{ov: memorywork.Overview{Rows: []memorywork.Row{
		{Workspace: "/Users/dev/acme-web", Entries: 12, Chars: 210000, SoftLimit: 200000, OverSoftLimit: true},
	}}}
	h, _ := memoryHarness(t, m, true)
	h.until("the card", func() bool { return h.hasText("acme-web’s Memory is large") })
	h.click("Open Memory settings")
	if r := h.a.Route(); r.Name != "settings" || r.Tab != "memory" {
		t.Fatalf("went to %+v", r)
	}
	h.a.Go(Route{Name: "new"})
	h.settle()
	if h.hasText("Memory is large") {
		t.Fatal("announced again")
	}
}

func TestSettingsMemoryShowsTheGlobalMemorysCoreAndTopics(t *testing.T) {
	m := &fakeMemory{ov: memorywork.Overview{Rows: []memorywork.Row{
		{Workspace: "", Entries: 134, Chars: 39000, SoftLimit: 40000, Layers: &memory.Layers{
			CoreEntries: 52, CoreChars: 14000, CoreCorrectionChars: 2600, Filed: 82,
			Topics: []memory.TopicSummary{{Topic: "Anlan cloud", Entries: 18, Chars: 7000}, {Topic: "Home network", Entries: 15, Chars: 5000}, {Topic: "Fortune telling", Entries: 6, Chars: 1500}, {Topic: "Writing", Entries: 5, Chars: 900}, {Topic: "Games", Entries: 2, Chars: 400}},
		}},
	}}}
	h, _ := memoryHarness(t, m, true)
	h.a.Go(Route{Name: "settings", Tab: "memory"})
	h.settle()
	want := "Core: 52 entries, corrections 2.6k of 3k characters · Topics: Anlan cloud 18, Home network 15, Fortune telling 6, Writing 5 and 1 more"
	if !h.hasText(want) {
		t.Errorf("missing %q in %q", want, h.tt.Texts())
	}
	h.click("Consolidate Global Memory")
	h.until("the consolidation's result", func() bool {
		return h.hasText("Consolidated 2 groups; 1 kept unchanged because the model’s output failed validation.")
	})
}

func TestTheSortIntoTopicsIsAnnouncedOnce(t *testing.T) {
	m := &fakeMemory{ov: memorywork.Overview{Rows: []memorywork.Row{
		{Workspace: "", Entries: 134, Chars: 39000, SoftLimit: 40000, Layers: &memory.Layers{CoreEntries: 52, Filed: 82, SortedAt: "2026-10-26T12:00:00Z",
			Topics: []memory.TopicSummary{{Topic: "Anlan cloud", Entries: 18, Chars: 7000}, {Topic: "Home network", Entries: 15, Chars: 5000}}}},
	}}}
	h, _ := memoryHarness(t, m, true)
	h.until("the card", func() bool { return h.hasText("Global Memory is sorted into topics") })
	if !h.hasText("52 entries stay in the core; 82 entries went to 2 topics. A topic’s entries are found by search instead of shown in every Session.") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	h.click("Open Memory settings")
	if r := h.a.Route(); r.Name != "settings" || r.Tab != "memory" {
		t.Fatalf("went to %+v", r)
	}
	h.a.Go(Route{Name: "new"})
	h.settle()
	if h.hasText("Global Memory is sorted into topics") {
		t.Fatal("announced again")
	}
}

func TestASortThatFiledNothingIsNotAnnounced(t *testing.T) {
	m := &fakeMemory{ov: memorywork.Overview{Rows: []memorywork.Row{
		{Workspace: "", Entries: 3, Chars: 400, SoftLimit: 40000, Layers: &memory.Layers{CoreEntries: 3, SortedAt: "2026-10-26T12:00:00Z", Topics: []memory.TopicSummary{}}},
	}}}
	h, _ := memoryHarness(t, m, true)
	h.settle()
	if h.hasText("Global Memory is sorted into topics") {
		t.Fatal("announced a sort that filed nothing")
	}
}
