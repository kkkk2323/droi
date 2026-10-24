package llmusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

func writeRequestLog(t *testing.T, records []diag.RequestRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	var b []byte
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b = append(append(b, line...), '\n')
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestObserveAddsUpThePeriod(t *testing.T) {
	chinese(t)
	now := time.Date(2026, 10, 23, 15, 30, 0, 0, time.Local)
	opus := func(at time.Time, in, read, out int64) diag.RequestRecord {
		return diag.RequestRecord{Time: at, Model: "claude-opus-5-5", Key: "fk-a...0001", Status: 200, DurationMs: 3000, TTFTMs: 1000,
			Usage: &diag.TokenUsage{Input: in, CacheRead: read, Output: out}}
	}
	records := []diag.RequestRecord{
		opus(now.Add(-time.Hour), 10000, 8000, 200),
		opus(now.Add(-2*time.Hour), 20000, 18000, 400),
		{Time: now.Add(-3 * time.Hour), Model: "glm-5.3-flash", Key: "fk-b...0002", Status: 200, DurationMs: 1000, TTFTMs: 500, SessionID: "s1",
			Usage: &diag.TokenUsage{Input: 5000, Output: 100}},
		{Time: now.Add(-4 * time.Hour), Model: "claude-opus-5-5", Key: "fk-a...0001", Status: 429, DurationMs: 400, Rotated: true, Attempts: 2},
		{Time: now.Add(-5 * time.Hour), Model: "claude-opus-5-5", Key: "fk-a...0001", Kind: "cache_warm", Status: 200, DurationMs: 900, TTFBMs: 900,
			Usage: &diag.TokenUsage{Input: 30000, CacheRead: 30000, Output: 1}},
		// Yesterday and last week: outside today, inside 7 and 30 days.
		opus(now.AddDate(0, 0, -1), 1000, 0, 10),
		opus(now.AddDate(0, 0, -10), 1000, 0, 10),
	}
	log := NewRequestLog(writeRequestLog(t, records))
	if err := log.Refresh(); err != nil {
		t.Fatal(err)
	}

	o := log.Observe(ObserveQuery{Period: PeriodToday}, now)
	if o.Totals.Calls != 5 || o.Totals.Failed != 1 || o.Totals.Switched != 1 || o.Records != 7 || !o.Hourly || len(o.Buckets) != 16 {
		t.Fatalf("today = %+v, %d buckets", o.Totals, len(o.Buckets))
	}
	// Opus: plain input × 1.6 + cache reads × 0.08 + output × 8.
	want := 1.6*2000 + 0.08*8000 + 8*200 + 1.6*2000 + 0.08*18000 + 8*400 + 0.08*30000 + 8*1
	if diff := o.Totals.Credits - want; diff > 0.01 || diff < -0.01 || o.Totals.Unpriced != 1 {
		t.Errorf("credits = %v, want %v; unpriced %d", o.Totals.Credits, want, o.Totals.Unpriced)
	}
	if o.Top[0] != "claude-opus-5-5" || o.Groups[ByKey][0].ID != "fk-a...0001" || len(o.Groups[BySession]) != 1 || len(o.Groups[ByKind]) != 2 {
		t.Errorf("groups = %+v / %+v", o.Top, o.Groups)
	}
	if got := o.Buckets[14].Total().Calls; got != 1 {
		t.Errorf("the 14:00 bucket holds %d calls", got)
	}
	day := (int(now.Weekday()) + 6) % 7
	if o.Heat[day][14].Calls != 1 || o.Heat[day][10].Calls != 1 {
		t.Errorf("heat = %+v", o.Heat[day])
	}
	if len(o.Listed) != 5 || !o.Listed[0].Time.After(o.Listed[1].Time) {
		t.Errorf("listed = %+v", o.Listed)
	}

	if o := log.Observe(ObserveQuery{Period: PeriodWeek}, now); o.Totals.Calls != 6 || len(o.Buckets) != 7 || o.Hourly {
		t.Errorf("week = %d calls, %d buckets", o.Totals.Calls, len(o.Buckets))
	}
	if o := log.Observe(ObserveQuery{Period: PeriodAll}, now); o.Totals.Calls != 7 || len(o.Buckets) != 11 {
		t.Errorf("all = %d calls, %d buckets", o.Totals.Calls, len(o.Buckets))
	}
	// The filters choose the list's rows only.
	f := log.Observe(ObserveQuery{Period: PeriodToday, Model: "claude-opus-5-5", FailedOnly: true}, now)
	if f.Totals.Calls != 5 || f.Matching != 1 || f.Listed[0].Status != 429 {
		t.Errorf("failed opus = %d listed of %d", f.Matching, f.Totals.Calls)
	}
	if k := log.Observe(ObserveQuery{Period: PeriodToday, Kind: "request"}, now); k.Matching != 4 {
		t.Errorf("model requests = %d", k.Matching)
	}

	v := BuildObserve(o, MetricTokens, BySession, func(id string) string { return map[string]string{"s1": "Fix the build"}[id] })
	if v.Summary[0].Value != "5" || v.Summary[0].Note != "1 次失败 · 1 次换 key" || v.Summary[3].Note != "1 次未计价" || v.Summary[4].Sub != "token/s" {
		t.Errorf("summary = %+v", v.Summary)
	}
	if len(v.Groups) != 1 || v.Groups[0].Name != "Fix the build" || v.Groups[0].Credits.Text != "未计价" {
		t.Errorf("sessions = %+v", v.Groups)
	}
	if len(v.Trend.Bars) != 16 || !v.Trend.Stacked || v.Trend.Legend[len(v.Trend.Legend)-1] != "其他" || !strings.HasPrefix(v.Trend.Peak, "峰值 ") {
		t.Errorf("trend = %+v", v.Trend)
	}
	levels := 0
	for _, c := range v.Heat.Rows[day].Cells {
		if c.Level > 0 {
			levels++
		}
	}
	if levels != 4 || v.Heat.Rows[day].Cells[10].Level != 4 || v.Heat.Rows[0].Label != "星期一" {
		t.Errorf("heat row = %+v", v.Heat.Rows[day])
	}
	if speed := BuildObserve(o, MetricSpeed, ByModel, func(string) string { return "" }).Trend; speed.Stacked || len(speed.Bars[14].Parts) != 1 {
		t.Errorf("speed trend = %+v", speed)
	}

	empty := BuildObserve(NewRequestLog(filepath.Join(t.TempDir(), "none")).Observe(ObserveQuery{}, now), MetricTokens, ByModel, func(string) string { return "" })
	if !empty.Empty || !strings.HasPrefix(empty.EmptyText, "今天还没有模型请求") {
		t.Errorf("empty = %+v", empty)
	}
}

func TestRequestDetailSaysEverything(t *testing.T) {
	chinese(t)
	r := diag.RequestRecord{Time: time.Now(), Model: "claude-opus-5-5", Provider: "anthropic", Upstream: "prem", Key: "fk-a...0001", Pool: "standard",
		RequestedPool: "standard", Status: 200, Stream: true, DurationMs: 3000, TTFBMs: 800, TTFTMs: 1000, SessionID: "s1", CallType: "session-title-generation",
		Attempts: 2, Rotated: true, RequestBytes: 2048, Bytes: 512, Usage: &diag.TokenUsage{Input: 1000, CacheRead: 600, CacheWrite: 100, Output: 50, Reasoning: 20}}
	lines := RequestDetail(r, func(string) string { return "Fix the build" })
	got := map[string]string{}
	for _, l := range lines {
		got[l.Label] = l.Value
	}
	for label, want := range map[string]string{
		"服务商": "anthropic · prem", "会话": "Fix the build · s1", "调用用途": "session-title-generation",
		"Token": "输入 1.0K（缓存读取 600 · 缓存写入 100）· 输出 50（推理 20）", "耗时": "共 3.0s · 首字节 800ms · 首 Token 1.0s",
		"换 key": "试了 2 把 key", "输出速度": "25 token/s",
	} {
		if got[label] != want {
			t.Errorf("%s = %q, want %q", label, got[label], want)
		}
	}
}

func TestTrendNamesAndDescribesItsBars(t *testing.T) {
	chinese(t)
	now := time.Date(2026, 10, 23, 15, 30, 0, 0, time.Local)
	records := []diag.RequestRecord{
		{Time: now.Add(-time.Hour), Model: "claude-opus-5-5", Key: "fk-a", Status: 200, DurationMs: 3000, TTFTMs: 1000,
			Usage: &diag.TokenUsage{Input: 10000, CacheRead: 8000, Output: 200}},
		{Time: now.Add(-2 * time.Hour), Model: "glm-5.3-flash", Key: "fk-a", Kind: "cache_warm", Status: 200, DurationMs: 1000,
			Usage: &diag.TokenUsage{Input: 5000, Output: 100}},
		{Time: now.AddDate(0, 0, -20), Model: "claude-opus-5-5", Key: "fk-a", Status: 200, Usage: &diag.TokenUsage{Input: 100, Output: 1}},
	}
	log := NewRequestLog(writeRequestLog(t, records))
	if err := log.Refresh(); err != nil {
		t.Fatal(err)
	}
	none := func(string) string { return "" }

	today := BuildObserve(log.Observe(ObserveQuery{Period: PeriodToday}, now), MetricTokens, ByModel, none)
	var named []string
	for _, b := range today.Trend.Bars {
		if b.Label != "" {
			named = append(named, b.Label)
		}
	}
	if strings.Join(named, " ") != "00:00 03:00 06:00 09:00 12:00 15:00" {
		t.Errorf("today's axis = %q", named)
	}
	bar := today.Trend.Bars[14]
	if bar.Title != "14:00–14:59" || bar.Total != "10.2K token · 1 次请求" || len(bar.Lines) != 1 || bar.Lines[0].Name != "claude-opus-5-5" || bar.Lines[0].Part != 0 {
		t.Errorf("the 14:00 bar = %+v", bar)
	}
	if today.Trend.Bars[0].Total != "没有请求" || today.Trend.About != "Token：输入加输出，含缓存" || today.Trend.Tick(2_000_000) != "2M" {
		t.Errorf("trend = %+v", today.Trend)
	}
	if about := MetricSpeed.About(); !strings.Contains(about, "token/s") {
		t.Errorf("speed is %q", about)
	}
	if MetricSpeed.Label() != "输出速度" {
		t.Errorf("speed is labeled %q", MetricSpeed.Label())
	}

	// A month names every fourth day back from today.
	month := BuildObserve(log.Observe(ObserveQuery{Period: PeriodMonth}, now), MetricCalls, ByModel, none)
	named = named[:0]
	for _, b := range month.Trend.Bars {
		if b.Label != "" {
			named = append(named, b.Label)
		}
	}
	if len(month.Trend.Bars) != 30 || len(named) != 8 || named[len(named)-1] != "10/23" || named[0] != "9/25" {
		t.Errorf("the month's axis = %q of %d bars", named, len(month.Trend.Bars))
	}

	cell := today.Heat.Rows[(int(now.Weekday())+6)%7].Cells[14]
	if cell.Title != "星期五 14:00–14:59" || cell.Value != "10.2K token · 1 次请求" || cell.Level == 0 {
		t.Errorf("heat cell = %+v", cell)
	}

	// A row filters by what the query holds, and a filter the period does
	// not see stays among the choices.
	if g := today.Groups; g[0].Filter != "claude-opus-5-5" {
		t.Errorf("model rows = %+v", g)
	}
	kinds := BuildObserve(log.Observe(ObserveQuery{Period: PeriodToday}, now), MetricTokens, ByKind, none).Groups
	if kinds[0].Filter != "request" || kinds[1].Filter != "cache_warm" {
		t.Errorf("type rows = %+v", kinds)
	}
	if keys := BuildObserve(log.Observe(ObserveQuery{Period: PeriodToday}, now), MetricTokens, ByKey, none).Groups; keys[0].Filter != "" {
		t.Errorf("key rows filter: %+v", keys)
	}
	gone := BuildObserve(log.Observe(ObserveQuery{Period: PeriodToday, Model: "gpt-6-astra", Kind: "key_open"}, now), MetricTokens, ByModel, none)
	if last := gone.Models[len(gone.Models)-1]; last.Value != "gpt-6-astra" {
		t.Errorf("models = %+v", gone.Models)
	}
	if last := gone.Kinds[len(gone.Kinds)-1]; last.Value != "key_open" || last.Label != "启用 key" {
		t.Errorf("kinds = %+v", gone.Kinds)
	}
}
