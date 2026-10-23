package llmusage

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

func TestKeyRows(t *testing.T) {
	chinese(t)
	s := sample(t)
	fresh := BuildKeyRow(s.Keys[1], sampleNow)
	if !fresh.Active || fresh.Problem || fresh.Key != "fk-fresh...0002" || len(fresh.Pools) != 2 {
		t.Fatalf("fresh row = %+v", fresh)
	}
	if std, core := fresh.Pools[0], fresh.Pools[1]; !std.HasValue || std.Percent != "90%" || std.Bar.Tone != ToneAccent || core.Percent != "100%" {
		t.Errorf("fresh pools = %+v", fresh.Pools)
	}
	if !strings.Contains(fresh.Help, "剩余 90%") || !strings.HasPrefix(fresh.Help, "Standard：") {
		t.Errorf("fresh help = %q", fresh.Help)
	}
	spent := BuildKeyRow(s.Keys[0], sampleNow)
	if spent.Pools[0].Percent != "0%" || spent.Pools[0].PercentTone != ToneDanger || !strings.HasSuffix(spent.Help, "额度更新于 30 分钟前") {
		t.Errorf("spent row = %+v", spent)
	}
	if dead := BuildKeyRow(s.Keys[2], sampleNow); !dead.Problem || dead.Badge.Label != "key 无效" || dead.Pools[0].HasValue || dead.Help != "暂无数据" {
		t.Errorf("dead row = %+v", dead)
	}
}

// monthlyKeys are keys whose monthly Standard windows have used these
// percents and end in ten days.
func monthlyKeys(now time.Time, used ...float64) []keys.KeyState {
	end := now.Add(10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	out := make([]keys.KeyState, len(used))
	for i, u := range used {
		out[i] = keys.KeyState{Index: i + 1, Status: "ok", Limits: &keys.KeyLimits{Standard: &keys.PoolLimits{Monthly: &keys.BucketLimit{UsedPercent: u, WindowEnd: end}}}}
	}
	return out
}

func TestMenuBarItem(t *testing.T) {
	chinese(t)
	now := sampleNow
	for _, c := range []struct {
		used  []float64
		title string
		icon  TrayIcon
	}{
		{[]float64{80, 60}, "30%", TrayOK},
		{[]float64{80, 70}, "25%", TrayWarning},
		{[]float64{95, 95}, "5%", TrayDanger},
		{nil, "--", TrayUnknown},
	} {
		v := BuildMenuBar(monthlyKeys(now, c.used...), ExpiringView{}, now)
		if v.Title != c.title || v.Icon != c.icon {
			t.Errorf("%v used: %+v, want %s %s", c.used, v, c.title, c.icon)
		}
	}
	if v := BuildMenuBar(monthlyKeys(now, 80, 60), ExpiringView{}, now); v.ToolTip != "Droi · Key 池：月额度还剩 30%" {
		t.Errorf("tooltip = %q", v.ToolTip)
	}
	// Quota about to be lost outranks all but a pool nearly spent.
	if v := BuildMenuBar(monthlyKeys(now, 10), ExpiringView{Alert: true}, now); v.Icon != TrayExpiring {
		t.Errorf("expiring: %+v", v)
	}
	if v := BuildMenuBar(monthlyKeys(now, 95), ExpiringView{Alert: true}, now); v.Icon != TrayDanger {
		t.Errorf("expiring but nearly spent: %+v", v)
	}
}

func TestEventNames(t *testing.T) {
	chinese(t)
	if got := eventName("prem_host_rewritten"); got != "模型请求改走 Key 池" {
		t.Errorf("prem event = %q", got)
	}
	if got := eventName("something_new"); got != "something_new" {
		t.Errorf("unknown event = %q", got)
	}
}

func TestBudget(t *testing.T) {
	chinese(t)
	quiet := BuildRunway(runwayKeys(3), &Spend{Today: 4, UsualByNow: 4, UsualDay: 9, Days: 7}, sampleNow).Budget
	if math.Round(quiet.Bar.Value) != 40 || quiet.Bar.Tone != ToneAccent || !quiet.HasUsual || math.Round(quiet.Usual) != 40 || !strings.HasPrefix(quiet.Note, "今天还能用 ") || quiet.NoteTone != ToneSecondary {
		t.Errorf("quiet budget = %+v", quiet)
	}
	busy := BuildRunway(runwayKeys(3), &Spend{Today: 30, UsualByNow: 10, UsualDay: 30, Days: 7}, sampleNow).Budget
	if busy.Bar.Tone != ToneDanger || !strings.HasPrefix(busy.Note, "今天已超出上限 ") || busy.NoteTone != ToneDanger {
		t.Errorf("busy budget = %+v", busy)
	}
	early := BuildRunway(runwayKeys(3), &Spend{Today: 2}, sampleNow).Budget
	if early.HasUsual {
		t.Errorf("a budget without a whole day of log = %+v", early)
	}
}
