package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/dashkit"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/proxy"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// keyPoolTestHost is a Host that runs Droi's droid, with a stand-in Factory
// billing endpoint, where the first key added has nearly spent its week and
// every later one is fresh, and stand-in droid releases whose newest is
// 1.0.0. It has already checked for that newest droid: the update row and
// card it brings would otherwise appear while a test clicks or shoots.
func keyPoolTestHost(t *testing.T) *host.Host {
	t.Helper()
	home, data := t.TempDir(), t.TempDir()
	ends := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	monthEnds := time.Now().Add(12 * 24 * time.Hour).UTC().Format(time.RFC3339)
	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		five, week := 5, 12
		if strings.HasSuffix(r.Header.Get("Authorization"), "1111") {
			five, week = 88, 94
		}
		fmt.Fprintf(w, `{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":%d,"windowEnd":%q},"weekly":{"usedPercent":%d,"windowEnd":%q},"monthly":{"usedPercent":30,"windowEnd":%q}}}}`,
			five, ends, week, ends, monthEnds)
	}))
	t.Cleanup(factory.Close)
	token := testJWT(map[string]any{"sub": "ua", "email": "a@x.dev", "org_id": "org_acme", "external_org_id": "org_acme"})
	if err := host.WriteCliLogin(filepath.Join(home, ".factory"), token, "r"); err != nil {
		t.Fatal(err)
	}
	settings := `{"droidKind":"droi","droiDroidVersion":"0.247.0"}`
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/LATEST" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "1.0.0")
	}))
	t.Cleanup(releases.Close)
	h := host.New(host.Config{UserData: data, Home: home, Env: func(string) string { return "" }, KeyPoolUpstream: factory.URL, DroidDownloads: releases.URL + "/releases"})
	t.Cleanup(h.Stop)
	t.Cleanup(h.KeyPool().Stop)
	h.CheckDroiDroid()
	for deadline := time.Now().Add(5 * time.Second); !h.DroiDroidState().Checked; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the check for a newer droid did not end: %+v", h.DroiDroidState())
		}
	}
	return h
}

func (h *harness) openUsageTab() {
	h.t.Helper()
	h.until("the session list", func() bool { return h.hasText("One") })
	h.click("Settings")
	h.until("the Usage tab", func() bool { _, ok := h.tt.Find("Usage"); return ok })
	h.click("Usage")
	h.until("the Usage tab's pages", func() bool { return h.hasText("Keys and quota") })
}

// scrollUsage scrolls the Usage tab's page to its end, where the keys are.
func (h *harness) scrollUsage() {
	h.tt.Scroll(refW-200, refH/2, 0, 4000)
	h.frame()
}

func (h *harness) addKey(key string) {
	h.t.Helper()
	h.click("Add key")
	h.until("the sheet", func() bool { return h.hasText("Droi checks the key's quota") })
	h.click("Factory API key to add")
	h.tt.Type(key)
	h.tt.Key(0, ui.KeyEnter)
	h.frame()
	masked := keys.MaskKey(key)
	h.until("the key "+masked, func() bool { return h.hasText(masked) && !h.hasText("Droi checks the key's quota") })
}

// Settings → Usage is the Key Pool's, drawn as droid-proxy's window: it is
// a tab once Droi's droid runs the Daemon, keys are added and switched
// on it, and each key shows what its windows have left.
func TestTheUsageTabHoldsTheKeyPool(t *testing.T) {
	h0 := keyPoolTestHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.openUsageTab()
	h.until("the empty pool", func() bool { return h.hasText("The Key Pool has no key yet") })
	if !h.hasText("The Key Pool starts once it has a key.") {
		t.Fatalf("the proxy's state is not shown: %q", h.tt.Texts())
	}

	first := "fk-" + strings.Repeat("a", 20) + "1111"
	h.addKey(first)
	// The first key has 6% of its week left, and droid holds it.
	h.until("the key's quota", func() bool { return h.hasText("6%") && h.hasText("Current") })
	for _, want := range []string{"Standard", "Core", "No monthly quota is lost in the next 7 days.", "No quota log yet"} {
		if !h.hasText(want) {
			t.Fatalf("no %q in %q", want, h.tt.Texts())
		}
	}
	second := "fk-" + strings.Repeat("b", 20) + "2222"
	h.addKey(second)
	h.until("the second key's quota", func() bool { return h.hasText("88%") })
	h.shoot("usage-keys")

	masked := keys.MaskKey(second)
	h.scrollUsage()
	h.click(masked)
	h.until("the key in full", func() bool { return h.hasText("Use this key") })
	h.scrollUsage()
	h.shoot("usage-key-open")
	h.click("Use " + masked)
	h.until("the switch", func() bool { return currentMasked(h0) == masked })
	// The window keeps the switch disabled until it has heard the action end.
	h.until("the action to end", func() bool { return h.a.usage.acting == "" })

	// Manual keeps the key the user chose; the status bar says so.
	h.click("Manual")
	h.until("manual switching", func() bool { return h0.KeyPool().State().Mode == keypool.Manual })
	h.until("the status bar", func() bool { return h.hasText("Keeps the key you chose") })
	h.until("the action to end", func() bool { return h.a.usage.acting == "" })

	// Scheduled opening is off until it is turned on here.
	if h0.KeyPool().State().ScheduledOpening {
		t.Fatal("scheduled opening is on in a new pool")
	}
	h.scrollUsage()
	h.clickSwitch("Start on a schedule", func() bool { return h0.KeyPool().State().ScheduledOpening })
	h.until("the action to end", func() bool { return h.a.usage.acting == "" })

	// The other pages read the proxy's records; it is not running, so they
	// say so instead of failing. The request log is read all the same.
	h.click("Observability")
	h.until("the request log", func() bool { return h.hasText("Nothing in this period") })
	h.click("Event log")
	h.until("the event log", func() bool { return h.hasText("No events yet") })
	h.click("Cache warming")
	h.until("cache warming", func() bool { return h.hasText("Cache warming is not running") })

	h.click("Keys and quota")
	h.until("the key again", func() bool { return h.hasText("Remove") })
	h.scrollUsage()
	h.click("Remove " + masked)
	h.until("the key to go", func() bool { return len(h0.KeyPool().State().Keys) == 1 })
}

func currentMasked(h *host.Host) string {
	for _, key := range h.KeyPool().State().Keys {
		if key.Current {
			return key.Masked
		}
	}
	return ""
}

// The Observe page answers the pointer: a bar or a cell shows what it
// holds as the pointer comes, a breakdown row lists only its requests and
// a second click lists them all again, whatever grouping comes between,
// and a request's details open again once they were closed.
func TestObservePageAnswersThePointer(t *testing.T) {
	h0 := keyPoolTestHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the session list", func() bool { return h.hasText("One") })
	h.click("Settings")
	h.click("Usage")
	h.until("the tab's own reads", func() bool { return !h.a.usage.refreshing && !h.a.usage.spendBusy })
	usageSample(h.a, time.Now())
	h.click("Observability")
	h.click("7 days")
	o := &h.a.usage.observe
	loaded := func(what string, ok func(q llmusage.ObserveQuery) bool) {
		t.Helper()
		h.until(what, func() bool { return o.loaded && !o.busy && o.data.Query == o.query && ok(o.query) })
	}
	loaded("the week", func(q llmusage.ObserveQuery) bool { return q.Period == llmusage.PeriodWeek })
	shows := func(s string) bool { return slices.Contains(h.tt.Texts(), s) }
	// reveal scrolls the page until s is in the middle of the window.
	reveal := func(s string) ui.Rect {
		t.Helper()
		for range 8 {
			r, ok := h.tt.Find(s)
			if !ok {
				t.Fatalf("no %q in %q", s, h.tt.Texts())
			}
			mid := r.Y + r.H/2
			if mid > 80 && mid < refH-80 {
				return r
			}
			h.tt.Scroll(refW/2, refH/2, 0, mid-refH/2)
			h.frame()
		}
		t.Fatalf("%q does not come into view", s)
		return ui.Rect{}
	}

	v := h.a.observeView()
	bar := v.Trend.Bars[0]
	r := reveal(bar.Label)
	if shows(bar.Total) {
		t.Fatal("a bar's card shows before the pointer comes")
	}
	h.tt.Move(r.X+r.W/2, r.Y-40)
	h.frame()
	if !shows(bar.Title) || !shows(bar.Total) {
		t.Fatalf("no card for the bar of %s: %q", bar.Title, h.tt.Texts())
	}
	h.tt.Move(refW-1, refH-1)
	h.frame()
	if shows(bar.Total) {
		t.Fatal("the bar's card stays once the pointer leaves")
	}

	var cell dashkit.HeatCell
	for _, row := range v.Heat.Rows {
		for _, c := range row.Cells {
			if c.Level > 0 && cell.Help == "" {
				cell = c
			}
		}
	}
	r = reveal(cell.Help)
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	h.frame()
	if !shows(cell.Title) || !shows(cell.Value) {
		t.Fatalf("no card for %s: %q", cell.Help, h.tt.Texts())
	}

	// Output speed's bars are not split by model, so the legend does not
	// name the models above the breakdown.
	o.metric = llmusage.MetricSpeed
	h.frame()
	const model = "claude-sonnet-5-5"
	reveal(model)
	h.click(model)
	loaded("the model's requests", func(q llmusage.ObserveQuery) bool { return q.Model == model })
	for _, row := range h.a.observeView().Rows {
		if row.Model != model {
			t.Fatalf("the list holds %s", row.Model)
		}
	}
	// Another grouping keeps the model; back on the models, its row lets
	// it go.
	h.click("Type")
	if o.query.Model != model {
		t.Fatalf("grouping by type dropped the model: %+v", o.query)
	}
	h.click("Model")
	reveal(model)
	h.click(model)
	loaded("every request", func(q llmusage.ObserveQuery) bool { return q.Model == "" })

	h.scrollUsage()
	// Scrolling over the list scrolls it too: a request it shows.
	var first llmusage.RequestRowView
	for _, row := range h.a.observeView().Rows {
		if first.Time == "" && shows(row.Time) {
			first = row
		}
	}
	h.click(first.Time)
	h.until("the request's details", func() bool { return h.hasText("Request details") })
	h.click("Close")
	h.until("the details to close", func() bool { return !h.hasText("Request details") })
	if o.reqSel != -1 {
		t.Fatalf("the request stays chosen: %d", o.reqSel)
	}
	h.click(first.Time)
	h.until("the request's details again", func() bool { return h.hasText("Request details") })
}

// Settings → Advanced offers Droi's droid and says where the Key Pool
// stands; Open Usage goes to the Usage tab.
func TestSettingsOffersDroisDroid(t *testing.T) {
	h0 := keyPoolTestHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the session list", func() bool { return h.hasText("One") })
	h.click("Settings")
	h.click("Advanced")
	h.until("the Key Pool", func() bool { return h.hasText("Use the Key Pool") })
	h.shoot("settings-droi-droid")
	if !h.hasText("Keys") || !h.hasText("The Key Pool has no key yet") {
		t.Fatalf("the Key Pool is not in Settings: %q", h.tt.Texts())
	}
	if h.hasText("Model requests use the Factory API keys") {
		t.Fatalf("the Key Pool group still has its footer: %q", h.tt.Texts())
	}
	h.click("Open Usage")
	h.until("the Usage tab", func() bool { return h.hasText("Keys and quota") })
}

// About has a row for Droi's droid while the Key Pool is on: its version and
// Check for updates, or Update once Factory offers a newer droid.
func TestAboutOffersDroisDroidUpdates(t *testing.T) {
	h0 := keyPoolTestHost(t)
	_ = h0.Settings.Update(func(s *host.Settings) { s.DroiDroidVersion = "9.9.9" })
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the session list", func() bool { return h.hasText("One") })
	h.click("Settings")
	h.click("About")
	h.until("Droi's droid's row", func() bool { return h.hasText("Droi's droid 9.9.9") })
	if _, ok := h.tt.Find("Check for Droi's droid updates"); !ok {
		t.Fatalf("no Check for updates for Droi's droid: %q", h.tt.Texts())
	}
	_ = h0.Settings.Update(func(s *host.Settings) { s.DroiDroidVersion = "0.1.0" })
	h0.Changed()
	h.until("the update", func() bool { _, ok := h.tt.Find("Update Droi's droid"); return ok })
	if !h.hasText("Updating restarts the Daemon, which interrupts running sessions.") {
		t.Fatalf("the row does not say what updating does: %q", h.tt.Texts())
	}
	h.shoot("settings-about-droi-droid")
}

// usageSample fills the Usage tab with what a running Key Pool reports, so
// its pages can be seen with data in them.
func usageSample(a *App, now time.Time) {
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	pct := func(v float64) *float64 { return &v }
	checked := now.Add(-2 * time.Minute)
	cool := now.Add(3 * time.Minute)
	std := func(five, week, month float64, monthEnd time.Duration) *keys.KeyLimits {
		return &keys.KeyLimits{
			Standard: &keys.PoolLimits{
				FiveHour: &keys.BucketLimit{UsedPercent: five, WindowEnd: at(2*time.Hour + 14*time.Minute)},
				Weekly:   &keys.BucketLimit{UsedPercent: week, WindowEnd: at(3*24*time.Hour + 5*time.Hour)},
				Monthly:  &keys.BucketLimit{UsedPercent: month, WindowEnd: at(monthEnd)},
			},
			Core: &keys.PoolLimits{FiveHour: &keys.BucketLimit{UsedPercent: five / 2, WindowEnd: at(2 * time.Hour)}},
		}
	}
	mk := func(i int, key string, current bool, five, week, month float64, end time.Duration) keypool.Key {
		ks := keys.KeyState{Index: i, Key: key, Status: "ok", StandardPercent: pct(max(five, week, month)), CorePercent: pct(five / 2),
			StandardAvailable: true, CoreAvailable: true, Limits: std(five, week, month, end), UsageCheckedAt: &checked, Active: current}
		return keypool.Key{ID: fmt.Sprintf("k%d", i), Masked: key, Current: current, State: ks}
	}
	cooling := mk(3, "fk-cool9...7c21", false, 100, 64, 40, 20*time.Hour)
	cooling.State.StandardCooldownUntil = &cool
	cooling.State.StandardAvailable = false
	st := keypool.State{
		Running: true,
		Mode:    keypool.Auto,
		Keys: []keypool.Key{
			mk(1, "fk-4f2a1...91be", true, 42, 58, 35, 9*24*time.Hour),
			mk(2, "fk-b7d03...0a4e", false, 12, 20, 12, 2*24*time.Hour+6*time.Hour),
			cooling,
			{ID: "k4", Masked: "fk-dead0...0003", State: keys.KeyState{Index: 4, Key: "fk-dead0...0003", Status: "http_401"}},
		},
		ScheduledOpening: true,
		Opening:          &keys.OpeningStatus{DailyOpens: 2, OpenedToday: 1, Scheduled: true, Queue: []int{3}},
		CacheWarming: &proxy.CacheWarmingStatus{
			Mode: proxy.CacheWarmingIdle,
			Sessions: []proxy.CacheWarmingSession{
				{SessionID: "b558a03e-40a4-4d50", Model: "claude-opus-5-5", PromptTokens: 63752, Refreshes: 2, NextWarmAt: now.Add(90 * time.Second), State: "scheduled"},
				{SessionID: "7f00c1d2-9e8a-4b11", Model: "gpt-6-astra", PromptTokens: 118400, Refreshes: 5, State: "refreshing"},
			},
			Totals: proxy.CacheWarmingTotals{Refreshes: 7, Failures: 1, CacheReadTokens: 512000, CacheWriteTokens: 9000, SpentUnits: 51200, SavedUnits: 640000},
		},
		Events: []diag.Event{
			{Seq: 1, Time: now.Add(-40 * time.Minute), Kind: "key_cooldown", Key: "fk-cool9...7c21", Pool: "standard", Model: "claude-opus-5-5", Status: 429, Detail: "rate limited"},
			{Seq: 2, Time: now.Add(-12 * time.Minute), Kind: "key_switched", Key: "fk-4f2a1...91be", Detail: "the current key cannot serve"},
		},
	}
	s := &a.usage
	far := now.Add(time.Hour)
	s.pool, s.poolAt = st, far
	var daily []llmusage.DaySpend
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for k := 13; k >= 0; k-- {
		daily = append(daily, llmusage.DaySpend{Day: midnight.AddDate(0, 0, -k), Spent: float64(8 + (k*7)%9)})
	}
	s.spend = llmusage.Spend{Today: 6, UsualByNow: 5, UsualDay: 11, Days: 7, Recent: 10.5, HasRecent: true, Daily: daily}
	s.hasSpend, s.spendAt = true, far
	var records []diag.RequestRecord
	for i := range 24 {
		r := diag.RequestRecord{Seq: 100 - i, Time: now.Add(-time.Duration(i) * 47 * time.Second), Model: "claude-opus-5-5", Provider: "anthropic", Key: "fk-4f2a1...91be",
			Pool: "standard", RequestedPool: "standard", Status: 200, Stream: true, DurationMs: int64(1800 + i*730), TTFBMs: int64(420 + i*13),
			RequestBytes: 180000, Bytes: 5100, Usage: &diag.TokenUsage{Input: int64(62000 + i*900), Output: int64(350 + i*40), CacheRead: int64(58000 + i*800)}, Attempts: 1}
		switch i % 6 {
		case 2:
			r.Model, r.Provider = "gpt-6-astra", "openai"
		case 4:
			r.Status, r.Usage, r.Attempts, r.Rotated, r.ErrorDetail = 402, nil, 2, true, `{"detail":"limit reached"}`
		}
		records = append(records, r)
	}
	// A week of calls for the Observe page: models, kinds and sessions
	// spread over the days and the working hours.
	session := ""
	if len(a.listed) > 0 {
		session = a.listed[0].SessionID
	}
	models := []string{"claude-opus-5-5", "claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5.1", "gpt-6-astra", "glm-5.3-flash", "claude-haiku-4-5-20251001"}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for i := range 420 {
		at := day.AddDate(0, 0, -(i % 7)).Add(time.Duration(9+(i*7)%11)*time.Hour + time.Duration(i%60)*time.Minute)
		if !at.Before(now) {
			continue
		}
		in := int64(40000 + (i*7919)%90000)
		r := diag.RequestRecord{Seq: i, Time: at, Model: models[(i/7)%len(models)], Provider: "anthropic", Key: fmt.Sprintf("fk-%05d...%04d", i%3, i%3), Pool: "standard", Status: 200, Stream: true,
			DurationMs: int64(2000 + (i*331)%9000), TTFBMs: int64(600 + (i*97)%2400), TTFTMs: int64(900 + (i*97)%2400), Attempts: 1,
			Usage: &diag.TokenUsage{Input: in, CacheRead: in * 9 / 10, CacheWrite: 1200, Output: int64(200 + (i*37)%1500)}}
		switch {
		case i%23 == 0:
			r.Status, r.Usage, r.ErrorDetail = 429, nil, "rate limited"
		case i%11 == 0:
			r.Kind = "cache_warm"
		case i%5 == 0:
			r.SessionID = session
		}
		records = append(records, r)
	}
	var b []byte
	for _, r := range records {
		line, _ := json.Marshal(r)
		b = append(append(b, line...), '\n')
	}
	if pool := a.keyPool(); pool != nil {
		_ = os.MkdirAll(filepath.Dir(pool.RequestLog()), 0o700)
		_ = os.WriteFile(pool.RequestLog(), b, 0o600)
	}
	s.observe = observeState{reqSel: -1, inited: true}
	s.refreshing = false
	s.model.built = false
}

// The Usage tab's pages with data in them, in Chinese, as droid-proxy's
// window shows them.
func TestUsageTabPages(t *testing.T) {
	h0 := keyPoolTestHost(t)
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			h := newHarnessWith(t, oneSession(), theme, func(cfg *Config) {
				cfg.Host = h0
				prefs.Language.Set(cfg.Prefs, string(l10n.Chinese))
			})
			t.Cleanup(func() { l10n.Set("", "") })
			h.until("the session list", func() bool { return h.hasText("One") })
			h.click(l10n.T("Settings"))
			h.click(l10n.T("Usage"))
			// What the tab reads on entering would replace the sample.
			h.until("the tab's own reads", func() bool { return !h.a.usage.refreshing && !h.a.usage.spendBusy })
			usageSample(h.a, time.Now())
			h.until("the sample", func() bool { return h.hasText("fk-4f2a1...91be") })
			for _, want := range []string{"今天已用", "近 14 天", "即将作废", "可用 3/4 把", "额度更新于", "今天已启用 1/2 把 · 还有 1 把等待"} {
				if !h.hasText(want) {
					t.Errorf("no %q in %q", want, h.tt.Texts())
				}
			}
			h.shoot("usage-sample-keys-" + theme)
			h.scrollUsage()
			h.shoot("usage-sample-keys-end-" + theme)
			h.click("fk-b7d03...0a4e")
			h.until("the key in full", func() bool { return h.hasText("使用这把 key") })
			h.scrollUsage()
			h.shoot("usage-sample-key-open-" + theme)
			for _, page := range []struct{ name, label, want string }{
				{"observe", "观测", "活跃时段"},
				{"events", "事件日志", "已切换 key"},
				{"warming", "缓存预热", "续期次数"},
			} {
				h.click(page.label)
				h.until(page.name, func() bool { return h.hasText(page.want) })
				h.shoot("usage-sample-" + page.name + "-" + theme)
			}
			// A week of the Observe page, then one request's details.
			h.click("观测")
			h.click("7 天")
			h.until("the week", func() bool {
				o := &h.a.usage.observe
				return o.data.Query.Period == llmusage.PeriodWeek && !o.busy
			})
			for _, want := range []string{"趋势", "活跃时段", "分组", "Credits"} {
				if !h.hasText(want) {
					t.Errorf("no %q in %q", want, h.tt.Texts())
				}
			}
			h.shoot("usage-sample-observe-week-" + theme)
			h.tt.Scroll(260, refH/2, 0, 520)
			h.frame()
			h.shoot("usage-sample-observe-groups-" + theme)
			h.scrollUsage()
			h.a.usage.observe.reqSel, h.a.usage.observe.detailOpen = 1, true
			h.frame()
			h.until("a request's details", func() bool { return h.hasText("请求详情") })
			h.shoot("usage-sample-observe-list-" + theme)
		})
	}
}

// While the Key Pool runs an older droid than Factory's newest, a corner card
// offers the update; closing it hides it.
func TestANewerDroisDroidShowsACard(t *testing.T) {
	h0 := keyPoolTestHost(t)
	p := h0.DroiDroidState()
	if !p.UpdateAvailable() {
		t.Fatalf("no newer droid: %+v", p)
	}
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the card", func() bool { return h.hasText("Droi's droid " + p.Latest + " is available") })
	if !h.hasText("You have " + p.Version + ".") {
		t.Fatalf("the card does not say which version runs: %q", h.tt.Texts())
	}
	if _, ok := h.tt.Find("Update Droi's droid"); !ok {
		t.Fatalf("no update button: %q", h.tt.Texts())
	}
	h.click("Dismiss")
	h.until("the card to close", func() bool { return !h.hasText("Droi's droid " + p.Latest + " is available") })
}
