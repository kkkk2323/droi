package llmusage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/proxy"
)

// The view models are checked in Chinese: their words are droid-proxy's,
// which the Usage page reproduces.
func chinese(t *testing.T) {
	t.Helper()
	l10n.Set(string(l10n.Chinese), "")
	t.Cleanup(func() { l10n.Set("", "") })
}

var sampleNow = time.Date(2026, 9, 16, 5, 30, 0, 0, time.UTC)

type sampleState struct {
	Keys     []keys.KeyState      `json:"keys"`
	Requests []diag.RequestRecord `json:"requests"`
	Events   []diag.Event         `json:"events"`
}

func sample(t *testing.T) sampleState {
	t.Helper()
	data, err := os.ReadFile("testdata/state.json")
	if err != nil {
		t.Fatal(err)
	}
	var s sampleState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPoolSummaries(t *testing.T) {
	chinese(t)
	pools := PoolSummaries(sample(t).Keys, sampleNow)
	std := pools[0]
	if std.Title != "Standard" || std.Note != "可用 2/3 把" || len(std.Windows) != 3 {
		t.Errorf("standard summary = %+v", std)
	}
	if w := std.Windows; w[0].Label != "5 小时" || w[0].Percent != "45%" || w[0].Bar.Tone != ToneAccent || w[1].Percent != "80%" || w[2].Label != "月" || w[2].Name != "月额度" || w[2].Percent != "94%" {
		t.Errorf("standard windows = %+v", w)
	}
	if std.Windows[0].Reset != "最早 1 小时 14 分后重置" {
		t.Errorf("standard 5h reset = %q", std.Windows[0].Reset)
	}
	if h := std.Windows[1].Help; !strings.Contains(h, "1 把有数据，1 把还没启用，按满额计算") || !strings.HasPrefix(h, "周额度平均剩余 80%") {
		t.Errorf("weekly help = %q", h)
	}
	core := pools[1]
	if core.Windows[0].Percent != "70%" || core.Windows[2].Percent != "100%" {
		t.Errorf("core summary = %+v", core)
	}
	if empty := BuildPoolSummary(Standard, nil, sampleNow); empty.Note != "可用 0/0 把" || empty.Windows[0].Percent != "—" {
		t.Errorf("empty summary = %+v", empty)
	}
	if checked := UsageCheckedAt(sample(t).Keys); checked == nil || !checked.Equal(time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("usage checked at = %v", checked)
	}
}

func TestKeyCards(t *testing.T) {
	chinese(t)
	s := sample(t)
	fresh := BuildKeyCard(s.Keys[1], sampleNow)
	if !fresh.Active || fresh.Badge.Label != "正常" || fresh.Badge.Tone != ToneGreen || fresh.Checked != "" || fresh.Cooldown != "" {
		t.Errorf("fresh card = %+v", fresh)
	}
	std := fresh.Pools[0]
	if !std.HasLimits || std.Available != "剩余 90%" || len(std.Rows) != 3 {
		t.Fatalf("fresh standard = %+v", std)
	}
	if r := std.Rows[0]; !r.Live || r.Percent != "90%" || r.Note != "2 小时 30 分后重置" || !r.HasPace {
		t.Errorf("fresh 5h = %+v", r)
	}
	if r := std.Rows[1]; r.Live || r.Note != "下次请求时启用" || r.Bar.Tone != ToneTrack || r.Percent != "—" {
		t.Errorf("fresh 1w = %+v", r)
	}
	if r := std.Rows[2]; r.Live || r.Note != "暂无数据" {
		t.Errorf("fresh 30d = %+v", r)
	}
	if core := fresh.Pools[1]; core.HasLimits || core.Available != "剩余 100%" {
		t.Errorf("fresh core = %+v", core)
	}

	spent := BuildKeyCard(s.Keys[0], sampleNow)
	if spent.Checked != "额度更新于 30 分钟前" || spent.Pools[0].Available != "剩余 0%" || spent.Pools[0].AvailableTone != ToneDanger {
		t.Errorf("spent card = %+v", spent)
	}
	if dead := BuildKeyCard(s.Keys[2], sampleNow); dead.Badge != (Badge{Label: "key 无效", Tone: ToneDanger}) {
		t.Errorf("dead card = %+v", dead)
	}

	unstarted := keys.KeyState{Index: 4, Key: "fk-new", Status: "ok", StandardAvailable: true,
		Limits: &keys.KeyLimits{Standard: &keys.PoolLimits{FiveHour: &keys.BucketLimit{}}}}
	until := sampleNow.Add(90 * time.Second)
	unstarted.CoreCooldownUntil = &until
	card := BuildKeyCard(unstarted, sampleNow)
	if card.Pools[0].Rows[0].Note != "未启用" || card.Cooldown != "Core 冷却中，1 分钟后恢复" || card.Badge.Label != "Core 冷却中" || card.Badge.Tone != ToneWarning {
		t.Errorf("unstarted card = %+v", card)
	}
}

func TestKeyLabels(t *testing.T) {
	chinese(t)
	for code, want := range map[string]string{"http_401": "key 无效", "fetch_error": "查询失败", "parse_error": "数据异常", "weird": "weird"} {
		if got := (KeyDisplay{Kind: KeyError, Code: code}).Label(); got != want {
			t.Errorf("label(%s) = %q, want %q", code, got, want)
		}
	}
	if got := (KeyDisplay{Kind: KeyCooldown, Pools: []Pool{Standard, Core}}).Label(); got != "Standard/Core 冷却中" {
		t.Errorf("both cooldown label = %q", got)
	}
	if got := (KeyDisplay{Kind: KeyNoData}).Label(); got != "暂无额度" {
		t.Errorf("no data label = %q", got)
	}
	if got := (KeyDisplay{Kind: KeyExhausted}).Label(); got != "额度用完" {
		t.Errorf("exhausted label = %q", got)
	}
}

func TestEventRows(t *testing.T) {
	events := sample(t).Events
	rows := EventRows(append(events, diag.Event{Seq: 9, Kind: "x"}))
	if rows[0].Kind != "x" || rows[0].Key != "—" || rows[0].Status != "—" || rows[0].StatusTone != ToneSecondary {
		t.Errorf("empty event, newest first = %+v", rows[0])
	}
	e := rows[1]
	if e.Kind != "key_exhausted" || e.Status != "402" || e.StatusTone != ToneDanger || e.Detail != "retrying another key" || e.Time != FormatClock(events[0].Time) {
		t.Errorf("event = %+v", e)
	}
}

func TestHistoryView(t *testing.T) {
	chinese(t)
	records := sample(t).Requests
	v := BuildHistory(HistoryState{Records: records, NextCursor: "older", PageNumber: 1, IsLatest: true})
	if v.PageText != "第 1 页 · 本页 2 条" || v.ModeText != "最新记录，自动刷新" || !v.HasNext || len(v.Records) != 2 {
		t.Errorf("pagination = %+v", v)
	}
	want := []MetricView{
		{Title: "本页请求", Value: "2", Note: "1 条失败", Tone: ToneDanger},
		{Title: "成功率", Value: "50%", Tone: ToneWarning},
		{Title: "输入 Token", Value: "12.0K", Note: "缓存命中 75%", Tone: TonePrimary},
		{Title: "输出 Token", Value: "350", Note: "1/2 条有用量数据", Tone: TonePrimary},
		{Title: "平均耗时", Value: "2.2s", Note: "首字节 490ms", Tone: TonePrimary},
	}
	for i, m := range want {
		if v.Summary[i] != m {
			t.Errorf("summary[%d] = %+v, want %+v", i, v.Summary[i], m)
		}
	}

	ok := v.Records[0]
	if ok.ProviderMark != "AN" || ok.ProviderClass != "anthropic" || ok.Model != "claude-fable-5.1" || ok.Status != "200" || ok.StatusTone != ToneGreen || !ok.Stream {
		t.Errorf("ok row = %+v", ok)
	}
	if ok.Input.Text != "12.0K" || ok.Input.Help != "输入 12000 token（含缓存） · 缓存读取 9000 · 缓存写入 200" || ok.Cache != (Cell{Text: "75%", Tone: ToneGreen, Help: "缓存读取占输入的比例"}) {
		t.Errorf("ok usage cells = %+v %+v", ok.Input, ok.Cache)
	}
	if ok.Output.Text != "350" || ok.Duration != "4.2s" || ok.TTFB != "首 700ms" || ok.TokensPerSec.Text != "100" || ok.Retries.Text != "—" || ok.Retries.Tone != ToneTertiary {
		t.Errorf("ok cells = %+v", ok)
	}
	if ok.Time != records[0].Time.Local().Format("01/02 15:04:05") || !strings.Contains(ok.TimeHelp, "年") {
		t.Errorf("time = %q %q", ok.Time, ok.TimeHelp)
	}

	failed := v.Records[1]
	if failed.StatusTone != ToneWarning || failed.Input.Text != "—" || failed.Input.Help != "上游没有返回用量" || failed.Retries != (Cell{Text: "×2", Tone: ToneWarning, Help: "额度用完时换 key：这次请求一共试了几把"}) {
		t.Errorf("failed row = %+v", failed)
	}
	if failed.StatusHelp != "key fk-spent...0001\n{\"detail\":\"limit reached\"}\n请求体 900B · 响应体 60B" {
		t.Errorf("status help = %q", failed.StatusHelp)
	}
	if ok.StatusHelp != "key fk-fresh...0002\n请求体 17.6K · 响应体 5.0K · 流式" {
		t.Errorf("stream status help = %q", ok.StatusHelp)
	}

	historic := BuildHistory(HistoryState{PageNumber: 3, Error: "read failed"})
	if historic.ModeText != "正在查看历史记录，已暂停自动刷新" || historic.EmptyTitle != "无法读取请求记录" || historic.Summary != nil || historic.HasNext {
		t.Errorf("historic = %+v", historic)
	}
	if BuildHistory(HistoryState{PageNumber: 1, IsLatest: true}).EmptyTitle != "还没有请求记录" {
		t.Error("empty title")
	}
}

func TestProviderMarks(t *testing.T) {
	chinese(t)
	cases := map[string][3]string{
		"anthropic": {"AN", "anthropic", "anthropic"},
		"openai":    {"OA", "openai", "openai"},
		"gemini":    {"GG", "google", "gemini"},
		"snowflake": {"SN", "other", "snowflake"},
		"":          {"—", "other", "未知供应商"},
	}
	for provider, want := range cases {
		mark, class, help := providerMark(provider)
		if [3]string{mark, class, help} != want {
			t.Errorf("%q: %q %q %q", provider, mark, class, help)
		}
	}
}

func TestRemainingTones(t *testing.T) {
	if RemainingTone(5) != ToneDanger || RemainingTone(20) != ToneWarning || RemainingTone(50) != ToneAccent {
		t.Error("bar tones")
	}
	if RemainingTextTone(5) != ToneDanger || RemainingTextTone(20) != ToneWarning || RemainingTextTone(50) != ToneSecondary {
		t.Error("text tones")
	}
}

func TestExpiring(t *testing.T) {
	chinese(t)
	now := sampleNow
	if v := BuildExpiring(sample(t).Keys, now, nil); len(v.Pools) != 0 || v.Summary != "" || v.Empty == "" {
		t.Fatalf("the sample month is far from its end; got %+v", v)
	}

	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	std := func(p keys.PoolLimits) *keys.KeyLimits { return &keys.KeyLimits{Standard: &p} }
	month := func(used float64, end time.Duration) *keys.KeyLimits {
		return std(keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: used, WindowEnd: at(end)}})
	}
	week := float64(WeeklyStandardCredits) / MonthlyStandardCredits * 100
	pool := []keys.KeyState{
		// The current week outlasts the month: one week of quota is all that is left.
		{Index: 1, Key: "a", Status: "ok", Limits: std(keys.PoolLimits{
			Monthly: &keys.BucketLimit{UsedPercent: 20, WindowEnd: at(70 * time.Hour)},
			Weekly:  &keys.BucketLimit{UsedPercent: 0, WindowEnd: at(120 * time.Hour)},
		})},
		{Index: 2, Key: "b", Status: "ok", Limits: month(70, 2*time.Hour)},
		{Index: 3, Key: "c", Status: "ok", Limits: month(50, 70*time.Hour)},
		{Index: 4, Key: "d", Status: "http_403", Limits: month(0, time.Hour)},
		// Used up: the Key Pool no longer routes it.
		{Index: 5, Key: "e", Status: "ok", Limits: month(99, time.Hour)},
		// Ten minutes after b: same group, more left first.
		{Index: 6, Key: "f", Status: "ok", Limits: month(69, 2*time.Hour+10*time.Minute)},
		// Beyond the horizon, and one week covers it.
		{Index: 7, Key: "g", Status: "ok", Limits: month(60, 8*24*time.Hour)},
	}
	v := BuildExpiring(pool, now, nil)
	if len(v.Pools) != 1 || v.Pools[0].Note != "4 把 key" || v.Alert {
		t.Fatalf("pools = %+v", v.Pools)
	}
	p := v.Pools[0]
	if len(p.Groups) != 2 || len(p.Groups[0].Keys) != 2 || p.Groups[0].Keys[0].Index != 6 || p.Groups[0].Keys[1].Index != 2 {
		t.Fatalf("groups = %+v", p.Groups)
	}
	soon := p.Groups[0]
	if soon.Countdown != "2 小时后" || soon.Tone != ToneDanger || soon.Note != "2 把 key · "+FormatCredits(MonthlyStandardCredits*0.61) || soon.Keys[1].Percent != FormatPercent(30) {
		t.Errorf("soon group = %+v", soon)
	}
	later := p.Groups[1]
	if later.Tone != ToneWarning || later.At != "2 天 22 小时后作废" || len(later.Keys) != 2 || later.Keys[0].Index != 1 || later.Keys[0].Credits != "≈ "+FormatCredits(0.8*MonthlyStandardCredits) || !strings.Contains(later.Keys[0].Tokens, "Opus 5.5") {
		t.Errorf("later group = %+v", later)
	}
	if a, c := later.Keys[0], later.Keys[1]; a.Note != "会作废 "+FormatCredits(MonthlyStandardCredits*(80-week)/100) || a.NoteTone != ToneDanger || c.Note != "" {
		t.Errorf("notes = %q %q", a.Note, c.Note)
	}
	if b := p.Buckets; b[0].Value != FormatCredits(MonthlyStandardCredits*0.61) || !strings.HasPrefix(b[0].Note, "2 把 key · 会作废 ") || !strings.HasSuffix(b[0].Note, " · 最早 2 小时后作废") || b[1].Value != FormatCredits(MonthlyStandardCredits*1.3) || b[1].Note != "2 把 key · "+later.Keys[0].Note+" · 最早 "+FormatShortDate(now.Add(70*time.Hour))+" 作废" || b[2].Value != "—" || b[2].Note != "无" {
		t.Errorf("buckets = %+v", b)
	}
	if v.Credits != FormatCredits(MonthlyStandardCredits*1.91) || !strings.HasPrefix(v.Loss, "会作废 ") {
		t.Errorf("credits = %q, loss = %q", v.Credits, v.Loss)
	}
	if v.SummaryTone != ToneDanger || v.Summary != "4 把 key · 最早 2 小时后作废" {
		t.Errorf("summary = %q", v.Summary)
	}
}

func TestExpiringAlertAndPace(t *testing.T) {
	chinese(t)
	now := sampleNow
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	month := func(used float64, end time.Duration) *keys.KeyLimits {
		return &keys.KeyLimits{Standard: &keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: used, WindowEnd: at(end)}}}
	}
	pool := []keys.KeyState{
		// Eight days and 90% left: two weeks fit now, one after two idle days.
		{Index: 1, Key: "a", Status: "ok", Limits: month(10, 8*24*time.Hour)},
		// Ends within the two days: idle, all of it is lost.
		{Index: 2, Key: "b", Status: "ok", Limits: month(80, 24*time.Hour)},
	}
	spend := &Spend{Recent: 15, HasRecent: true}
	v := BuildExpiring(pool, now, spend)
	if !v.Alert || v.Summary != "2 把 key 要在 2 天内开始用" || v.SummaryTone != ToneDanger {
		t.Fatalf("alert = %v %q", v.Alert, v.Summary)
	}
	p := v.Pools[0]
	chip := p.Groups[1].Keys[0]
	if chip.Index != 1 || chip.Note != "2 天内要开始用" || chip.NoteTone != ToneWarning || !strings.Contains(chip.Help, "2 天内不开始用，会作废") {
		t.Errorf("chip = %+v", chip)
	}
	// b needs 20% within a day; a and b together 110% within 8 days.
	if p.Pace != "每天至少用 "+FormatCredits(MonthlyStandardCredits*0.2)+" 才不作废" || p.Recent != "近 7 天平均每天 "+FormatCredits(MonthlyStandardCredits*0.15) || p.PaceTone != ToneDanger {
		t.Errorf("pace = %q %q %v", p.Pace, p.Recent, p.PaceTone)
	}
	spend.Recent = 30
	if p := BuildExpiring(pool, now, spend).Pools[0]; p.PaceTone != ToneSecondary {
		t.Errorf("a pace the recent use covers stays quiet: %v", p.PaceTone)
	}
	if p := BuildExpiring(pool, now, nil).Pools[0]; p.Recent != "" || p.PaceTone != ToneSecondary {
		t.Errorf("no log: %q %v", p.Recent, p.PaceTone)
	}
}

func TestMonthlyOutlook(t *testing.T) {
	now := time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC)
	at := func(h float64) string { return now.Add(time.Duration(h * float64(time.Hour))).Format(time.RFC3339) }
	week := float64(WeeklyStandardCredits) / MonthlyStandardCredits * 100
	near := func(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

	// Five days left: one more week is all that fits.
	o, ok := MonthlyOutlookOf(&keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: 10, WindowEnd: at(120)}}, now, 48*time.Hour)
	if !ok || o.Left != 90 || !near(o.Usable, week) || !near(o.UsableAfterRest, week) {
		t.Errorf("five days: %+v", o)
	}
	// Eight days left: two weeks fit now, one after resting two days.
	o, _ = MonthlyOutlookOf(&keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: 10, WindowEnd: at(192)}}, now, 48*time.Hour)
	if o.Usable != 90 || !near(o.UsableAfterRest, week) {
		t.Errorf("eight days: %+v", o)
	}
	// The current week has 40% left and resets before the month ends.
	o, _ = MonthlyOutlookOf(&keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: 10, WindowEnd: at(120)}, Weekly: &keys.BucketLimit{UsedPercent: 60, WindowEnd: at(24)}}, now, 48*time.Hour)
	if !near(o.Usable, 0.4*week+week) || !near(o.UsableAfterRest, week) {
		t.Errorf("live week: %+v", o)
	}
	// Resting past the end leaves nothing.
	o, _ = MonthlyOutlookOf(&keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: 50, WindowEnd: at(24)}}, now, 48*time.Hour)
	if o.Usable != 50 || o.UsableAfterRest != 0 {
		t.Errorf("rest past the end: %+v", o)
	}
	if _, ok := MonthlyOutlookOf(&keys.PoolLimits{Monthly: &keys.BucketLimit{}}, now, 0); ok {
		t.Error("a month that has not started has no outlook")
	}
}

// runwayKeys is n Standard months that have not started: 100% each.
func runwayKeys(n int) []keys.KeyState {
	out := make([]keys.KeyState, n)
	for i := range out {
		out[i] = keys.KeyState{Index: i + 1, Status: "ok", Limits: &keys.KeyLimits{Standard: &keys.PoolLimits{Monthly: &keys.BucketLimit{}}}}
	}
	return out
}

func TestRunwayNeedsALogAndKeys(t *testing.T) {
	if v := BuildRunway(runwayKeys(3), nil, sampleNow); v.Available || v.Empty == "" {
		t.Errorf("no log: %+v", v)
	}
	if v := BuildRunway(nil, &Spend{}, sampleNow); v.Available {
		t.Errorf("no keys: %+v", v)
	}
}

func TestRunwayOnAQuietDay(t *testing.T) {
	chinese(t)
	now := sampleNow
	// Three months waiting: 10% of a month a day lasts the 30 days.
	spend := &Spend{Today: 4, UsualByNow: 4, UsualDay: 9, Days: 7, Recent: 8, HasRecent: true,
		Daily: []DaySpend{{Day: now.Add(-24 * time.Hour), Spent: 9}, {Day: now, Spent: 4}}}
	v := BuildRunway(runwayKeys(3), spend, now)
	if !v.Available || len(v.Metrics) != 4 {
		t.Fatalf("runway = %+v", v)
	}
	today, whole, left, lasts := v.Metrics[0], v.Metrics[1], v.Metrics[2], v.Metrics[3]
	// The pool's month is three keys' months: 4% of one is 1.3% of it.
	if today.Value != FormatCredits(0.04*MonthlyStandardCredits) || today.Sub != "1.3%" || today.Note != "平时这时 "+today.Value+"（1.3%）" || today.Tone != TonePrimary {
		t.Errorf("today = %+v", today)
	}
	if whole.Value != "约 "+FormatCredits(0.09*MonthlyStandardCredits) || whole.Tone != TonePrimary {
		t.Errorf("whole day = %+v", whole)
	}
	if left.Title != "今天还能用" || left.Sub != "2.0%" || left.Note != "每天上限 "+FormatCredits(0.1*MonthlyStandardCredits)+"（3.3%）" || left.Tone != TonePrimary {
		t.Errorf("left = %+v", left)
	}
	// 300% less 9% a day for 30 days.
	if lasts.Title != "按今天的量" || lasts.Value != "够用" || lasts.Note != "30 天后还剩 "+FormatCredits(0.3*MonthlyStandardCredits)+"（10%）" || lasts.Tone != ToneGreen {
		t.Errorf("lasts = %+v", lasts)
	}
	if len(v.Lines) != 2 || v.Lines[0].Value != lasts.Note || v.Lines[0].Note != "每天还能多用 "+FormatCredits(0.01*MonthlyStandardCredits)+"（0.3%）" || v.Lines[0].Label != "按今天的量（每天 "+FormatCredits(0.09*MonthlyStandardCredits)+" · 3.0%）" || !strings.HasPrefix(v.Lines[1].Label, "按近 7 天平均（每天 ") {
		t.Errorf("lines = %+v", v.Lines)
	}
	if c := v.Chart; len(c.Bars) != 2 || !c.Bars[1].Today || c.Bars[0].Over || c.Last != "今天" || !strings.Contains(c.Bars[0].Help, "星期") {
		t.Errorf("chart = %+v", c)
	}
	if !strings.Contains(v.Help, "3 把 key 的月额度加起来是 100%") {
		t.Errorf("help = %q", v.Help)
	}
}

func TestRunwayOnABusyDay(t *testing.T) {
	chinese(t)
	now := sampleNow
	// 30% by now where 10% is usual, and the day heads for 50%: three
	// months run out in six days.
	spend := &Spend{Today: 30, UsualByNow: 10, UsualDay: 30, Days: 7}
	v := BuildRunway(runwayKeys(3), spend, now)
	today, whole, left, lasts := v.Metrics[0], v.Metrics[1], v.Metrics[2], v.Metrics[3]
	if today.Tone != ToneWarning || !strings.HasPrefix(today.Note, "比平时这时多 ") {
		t.Errorf("today = %+v", today)
	}
	if whole.Tone != ToneWarning {
		t.Errorf("whole day = %+v", whole)
	}
	if left.Title != "今天已超出" || left.Tone != ToneDanger {
		t.Errorf("left = %+v", left)
	}
	if want := now.Add(6 * 24 * time.Hour).Local().Format("1/2"); lasts.Value != want || lasts.Tone != ToneDanger {
		t.Errorf("lasts = %+v, want %s", lasts, want)
	}
	if len(v.Lines) != 1 || !strings.HasSuffix(v.Lines[0].Value, " 前后用完") || v.Lines[0].Note != "每天少用 "+FormatCredits(0.4*MonthlyStandardCredits)+"（13%）才能用满 30 天" {
		t.Errorf("lines = %+v", v.Lines)
	}
}

func TestRunwayWithoutAWholeDay(t *testing.T) {
	chinese(t)
	v := BuildRunway(runwayKeys(3), &Spend{Today: 2}, sampleNow)
	if v.Metrics[1].Value != "—" || v.Metrics[3].Title != "按今天已用的量" {
		t.Errorf("runway = %+v", v)
	}
}

func TestCacheWarmingView(t *testing.T) {
	chinese(t)
	now := sampleNow
	if v := BuildCacheWarming(nil, now); v.Available || v.Summary != "不可用" {
		t.Fatalf("without cache warming: %+v", v)
	}
	cw := &proxy.CacheWarmingStatus{
		Mode: proxy.CacheWarmingDryRun,
		Sessions: []proxy.CacheWarmingSession{{
			SessionID: "b558a03e-40a4-4d50", Model: "claude-opus-5-5", PromptTokens: 63752,
			Refreshes: 2, NextWarmAt: now.Add(90 * time.Second), State: "scheduled",
		}},
		Totals: proxy.CacheWarmingTotals{Refreshes: 2, Failures: 1, CacheReadTokens: 127000, SpentUnits: 6350, SavedUnits: 76500},
	}
	v := BuildCacheWarming(cw, now)
	if !v.Available || v.Summary != "1 个会话 · 续期 2 次 · 失败 1 次" {
		t.Fatalf("summary = %q (available %v)", v.Summary, v.Available)
	}
	if len(v.Options) != 5 {
		t.Fatalf("options = %+v", v.Options)
	}
	for _, o := range v.Options {
		if o.Selected != (o.Value == proxy.CacheWarmingDryRun) || o.Label == "" {
			t.Errorf("option %s = %+v", o.Value, o)
		}
	}
	row := v.Sessions[0]
	if row.Session != "b558a03e" || row.Context != "63.8K" || row.Next != "1 分钟" || row.State != "等待中" {
		t.Errorf("session row = %+v", row)
	}
	if v.Metrics[1].Tone != ToneDanger || v.Metrics[3].Value != "76.5K" {
		t.Errorf("metrics = %+v", v.Metrics)
	}

	cw = &proxy.CacheWarmingStatus{Mode: proxy.CacheWarmingSmart}
	if v := BuildCacheWarming(cw, now); !strings.Contains(v.Help, "正在读取") {
		t.Errorf("smart help before the model = %q", v.Help)
	}
	cw.Smart = &proxy.CacheWarmingSmartModel{Waits: 1187, BuiltAt: now.Add(-10 * time.Minute)}
	if v := BuildCacheWarming(cw, now); !strings.Contains(v.Help, "已分析 1187 次等待") {
		t.Errorf("smart help = %q", v.Help)
	}
	if v := BuildCacheWarming(&proxy.CacheWarmingStatus{Mode: proxy.CacheWarmingOff}, now); v.Summary != "已关闭" || len(v.Sessions) != 0 || v.Sessions == nil {
		t.Errorf("off = %+v", v)
	}
}

func TestCreditsInWords(t *testing.T) {
	chinese(t)
	for v, want := range map[float64]string{0: "0", 950: "950", 8000: "8K", 12_345: "12.3K", 38_500_000: "38.5M", 155_600_000: "156M", 1_155_000_000: "1.16B", 2_000_000_000: "2B"} {
		if got := FormatCredits(v); got != want {
			t.Errorf("FormatCredits(%v) = %q, want %q", v, got, want)
		}
	}
	h := TokenEquivalents(1_600_000)
	if !strings.Contains(h, "Opus 5.5：1M 输入 token，或 200K 输出 token") || !strings.Contains(h, "Fable 5.1：400K 输入 token") {
		t.Errorf("help = %q", h)
	}
	now := sampleNow
	cases := []struct{ got, want string }{
		{FormatCountdown(now.Add(45*time.Second), now), "45 秒"},
		{FormatCountdown(now.Add(3*time.Hour+5*time.Minute), now), "3 小时 5 分"},
		{FormatCountdown(now.Add(-time.Second), now), "已到期"},
		{FormatReset(now.Add(90*time.Minute), now), "1 小时 30 分后重置"},
		{FormatReset(now.Add(-time.Minute), now), "已重置"},
		{FormatRelative(now.Add(-600*time.Second), now), "10 分钟前"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	beyond := now.Add(30 * time.Hour)
	if got, want := FormatReset(beyond, now), beyond.Local().Format("1/2 15:04")+" 重置"; got != want {
		t.Errorf("reset beyond a day = %q, want %q", got, want)
	}
}

func TestRequestHistoryNavigationAndRefresh(t *testing.T) {
	records := sample(t).Requests
	var calls []string
	h := NewHistory(func(_ context.Context, cursor string) (diag.RequestPage, error) {
		calls = append(calls, cursor)
		switch cursor {
		case "":
			return diag.RequestPage{Requests: records[:1], NextCursor: "page-2"}, nil
		case "page-2":
			return diag.RequestPage{Requests: records[1:], NextCursor: "page-3"}, nil
		}
		return diag.RequestPage{}, nil
	})
	ctx := context.Background()

	h.RefreshLatest(ctx)
	s := h.State()
	if s.PageNumber != 1 || RequestID(s.Records[0]) != RequestID(records[0]) {
		t.Fatalf("first page = %+v", s)
	}
	h.Next(ctx)
	s = h.State()
	if s.PageNumber != 2 || s.IsLatest || RequestID(s.Records[0]) != RequestID(records[1]) {
		t.Fatalf("second page = %+v", s)
	}

	h.RefreshLatest(ctx)
	if len(calls) != 2 {
		t.Errorf("older pages must not refresh by themselves; calls = %q", calls)
	}
	h.Next(ctx)
	if s = h.State(); s.PageNumber != 3 || s.NextCursor != "" {
		t.Errorf("third page = %+v", s)
	}
	h.Next(ctx)
	if len(calls) != 3 {
		t.Errorf("next past the end must not load; calls = %q", calls)
	}
	h.Previous(ctx)
	if s = h.State(); s.PageNumber != 2 || calls[len(calls)-1] != "page-2" {
		t.Errorf("previous = %+v, calls = %q", s, calls)
	}
	h.Latest(ctx)
	if s = h.State(); !s.IsLatest || RequestID(s.Records[0]) != RequestID(records[0]) {
		t.Errorf("latest = %+v", s)
	}
	h.Previous(ctx)
	if len(calls) != 5 {
		t.Errorf("previous on the latest page must not load; calls = %q", calls)
	}
	if s = h.State(); s.IsLoading || s.Error != "" {
		t.Errorf("final state = %+v", s)
	}
}

func TestRequestHistoryFailureKeepsThePageShown(t *testing.T) {
	records := sample(t).Requests
	fail := false
	h := NewHistory(func(_ context.Context, cursor string) (diag.RequestPage, error) {
		if fail {
			return diag.RequestPage{}, errors.New("read failed")
		}
		if cursor == "" {
			return diag.RequestPage{Requests: records[:1], NextCursor: "older"}, nil
		}
		return diag.RequestPage{Requests: records[1:]}, nil
	})
	ctx := context.Background()
	h.Latest(ctx)
	fail = true
	h.Next(ctx)
	s := h.State()
	if s.PageNumber != 1 || RequestID(s.Records[0]) != RequestID(records[0]) || s.NextCursor != "older" || s.Error != "read failed" || s.IsLoading {
		t.Errorf("after failure = %+v", s)
	}
	fail = false
	h.Next(ctx)
	if s = h.State(); s.PageNumber != 2 || s.Error != "" {
		t.Errorf("after recovery = %+v", s)
	}
}

func TestRequestHistoryDropsOverlappingLoads(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	h := NewHistory(func(context.Context, string) (diag.RequestPage, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-release
		return diag.RequestPage{}, nil
	})
	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		h.Latest(ctx)
		close(done)
	}()
	<-started
	if !h.State().IsLoading {
		t.Errorf("should be loading")
	}
	h.Reload(ctx)
	h.Latest(ctx)
	mu.Lock()
	if calls != 1 {
		t.Errorf("overlapping loads ran: calls = %d", calls)
	}
	mu.Unlock()
	close(release)
	<-done
	if h.State().IsLoading {
		t.Errorf("still loading after completion")
	}
}

func TestRequestHistoryIgnoresCancelledLoads(t *testing.T) {
	h := NewHistory(func(context.Context, string) (diag.RequestPage, error) {
		return diag.RequestPage{}, errors.New("context canceled")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Latest(ctx)
	if s := h.State(); s.Error != "" || s.IsLoading {
		t.Errorf("a cancelled load should leave no error: %+v", s)
	}
}
