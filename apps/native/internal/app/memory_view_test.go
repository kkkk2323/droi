package app

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

type fakeMemory struct {
	mu      sync.Mutex
	status  memorywork.Status
	updates int
	reset   int
	fail    error
}

func (m *fakeMemory) Status() (memorywork.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status, nil
}
func (m *fakeMemory) UpdateNow() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates++
	return m.fail
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

func TestSettingsMemoryShowsTheMemorySummaryAndUpdatesNow(t *testing.T) {
	m := &fakeMemory{status: memorywork.Status{SummaryBytes: 6_240, SummaryUpdatedAt: "2026-10-27T09:00:00Z", Summaries: 42, Waiting: 3, Notes: 1}}
	h, h0 := memoryHarness(t, m, true)
	_ = h0.Settings.Update(func(s *host.Settings) { s.MemoryModel = host.OptString("gemini-3.8-flash") })
	h.a.Go(Route{Name: "settings", Tab: "memory"})
	h.settle()
	for _, want := range []string{"Session summary model", "Memory Summary model", "Memory Summary", "6.2 KB of 10 KB · updated " + localTime("2026-10-27T09:00:00Z"),
		"42 Session summaries · 3 Sessions waiting · 1 Memory Note", "No Memory Sessions yet"} {
		if !h.hasText(want) {
			t.Errorf("missing %q in %q", want, h.tt.Texts())
		}
	}
	h.click("Update Memory now")
	h.until("the update's result", func() bool { return h.hasText("Memory is up to date.") })
	if m.updates != 1 {
		t.Errorf("%d updates", m.updates)
	}
	m.fail = errors.New("The Daemon is not running.")
	h.click("Update Memory now")
	h.until("the failure", func() bool { return h.hasText("The Daemon is not running.") })
	h.tt.Scroll(refW/2, refH/2, 0, 2000)
	h.settle()
	h.click("Reset prompts to default")
	h.frame()
	if m.reset != 1 || !h.hasText("The prompts are back to Droi’s.") {
		t.Error("Reset prompts did nothing")
	}
}

func TestSettingsMemorySaysWhyThereIsNoSummaryAndWhenAnUpdateFailed(t *testing.T) {
	m := &fakeMemory{status: memorywork.Status{LastError: "The Memory Summary must start with the line v1.", LastErrorAt: "2026-10-27T09:00:00Z"}}
	h, _ := memoryHarness(t, m, true)
	h.a.Go(Route{Name: "settings", Tab: "memory"})
	h.settle()
	if !h.hasText("Not written yet. Droi writes it from Sessions once they have been left for a while; Update now takes the ones left for ten minutes.") {
		t.Errorf("no empty state: %q", h.tt.Texts())
	}
	if !h.hasText("The last update failed (" + localTime("2026-10-27T09:00:00Z") + "): The Memory Summary must start with the line v1. Droi tries again within the hour.") {
		t.Errorf("no failure: %q", h.tt.Texts())
	}
}

func TestAMemorySessionOpensFromSettings(t *testing.T) {
	m := &fakeMemory{status: memorywork.Status{SummaryBytes: 100, Runs: []memory.RunRecord{
		{SessionID: "11111111-1111-4111-8111-111111111111", Title: "Memory: update the Memory Summary", Kind: memory.RunMemorySummary, Model: "gemini-3.8-flash", StartedAt: "2026-10-27T09:00:00Z", EndedAt: "2026-10-27T09:01:00Z"},
		{SessionID: "22222222-2222-4222-8222-222222222222", Title: "Memory: summarize Fix login", Kind: memory.RunSessionSummary, Model: "gemini-3.8-flash", StartedAt: "2026-10-27T08:59:00Z", EndedAt: "2026-10-27T09:00:00Z", Error: "The reply did not match the schema."},
	}}}
	h, _ := memoryHarness(t, m, true)
	h.a.Go(Route{Name: "settings", Tab: "memory"})
	h.settle()
	h.tt.Scroll(refW/2, refH/2, 0, 2000)
	h.settle()
	for _, want := range []string{"Memory Sessions", "Memory: summarize Fix login", "gemini-3.8-flash · " + localTime("2026-10-27T08:59:00Z"), "Failed: The reply did not match the schema."} {
		if !h.hasText(want) {
			t.Errorf("missing %q in %q", want, h.tt.Texts())
		}
	}
	h.click("Open Memory: summarize Fix login")
	if r := h.a.Route(); r.Name != "session" || r.SessionID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("went to %+v", r)
	}
}

func TestTheSessionListLeavesOutMemorySessions(t *testing.T) {
	got := summariesOf([]protocol.DaemonAvailableSessionInfo{
		{SessionID: "work", Title: "Fix login"},
		{SessionID: "memory", Title: "Memory: summarize Fix login", Tags: []protocol.SessionTag{{Name: sessions.MemoryTag}}},
	})
	if len(got) != 1 || got[0].SessionID != "work" {
		t.Fatalf("listed %+v", got)
	}
}

func clickMemorySwitch(h *harness, asked func() bool) {
	h.t.Helper()
	clickSwitchBeside(h, []string{"Off: Droid starts", "On: Droi summarizes"}, asked)
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

func TestTheMemorySummaryModelIsTheSessionSummaryModelUntilPicked(t *testing.T) {
	st := host.Settings{MemoryModel: host.OptString("gemini-3.8-flash")}
	if st.MemorySummaryModelOrDefault() != "gemini-3.8-flash" {
		t.Fatal(st.MemorySummaryModelOrDefault())
	}
	st.MemorySummaryModel = host.OptString("claude-haiku-5.5")
	if st.MemorySummaryModelOrDefault() != "claude-haiku-5.5" || st.MemoryModelOrDefault() != "gemini-3.8-flash" {
		t.Fatal(st)
	}
}
