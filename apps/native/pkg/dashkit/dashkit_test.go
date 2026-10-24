package dashkit

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

func TestLookDrawsAStripAChartAndASegmentedControl(t *testing.T) {
	theme.RegisterFonts()
	k := kit.New(theme.Light, "", "", 1)
	selected, changed := 0, false
	tt := ui.NewTester(func(c *ui.Context) {
		u := New(k, c)
		ui.Column(c).Fill().Gap(16).Padding(16).Children(func() {
			u.MetricStrip(c, []MetricView{
				{Title: "Requests", Value: "42", Note: "All succeeded", Tone: TonePrimary},
				{Title: "Errors", Value: "3", Sub: "7%", Tone: ToneDanger},
			})
			u.Chart(c, ChartView{
				Label: "Last 2 days", Limit: 10, Top: 12, Scale: 1, First: "5/1", Last: "Today",
				Bars: []ChartBar{{Value: 4, Help: "5/1: 4"}, {Value: 11, Today: true, Over: true, Help: "Today: 11"}},
			}, 120)
			if u.Segmented(c, &selected, []string{"Day", "Week"}, "Period", "", false, false) {
				changed = true
			}
		})
	}, 600, 400)
	tt.Frame()

	for _, s := range []string{"Requests", "42", "All succeeded", "Errors", "7%", "Last 2 days", "5/1: 4", "Today: 11", "Today"} {
		if _, ok := tt.Find(s); !ok {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	if err := tt.Click("Week"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !changed || selected != 1 {
		t.Errorf("segmented: changed %v, selected %d", changed, selected)
	}
}

func TestNewPicksThePaletteOfTheTheme(t *testing.T) {
	k := kit.New(theme.Light, "", "", 1)
	var light, dark *Palette
	tt := ui.NewTester(func(c *ui.Context) {
		if c.Theme().Dark {
			dark = New(k, c).P
		} else {
			light = New(k, c).P
		}
	}, 100, 100)
	tt.Frame()
	tt.SetDark(true)
	tt.Frame()
	if light != &Light || dark != &Dark {
		t.Errorf("light %p, dark %p; want %p, %p", light, dark, &Light, &Dark)
	}
}
