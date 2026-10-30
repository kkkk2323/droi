package kit

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// The selects' lists are menus: the keys move among the options and choose
// one, as in the system's menus.
func TestSelectsChooseWithTheKeys(t *testing.T) {
	k := New(theme.Light, "", "", 1)
	opts := []Option{{Value: "low", Label: "Low"}, {Value: "medium", Label: "Medium"}, {Value: "high", Label: "High"}}
	for name, sel := range map[string]func(c *ui.Context, value string) (string, bool){
		"quiet": func(c *ui.Context, value string) (string, bool) {
			return k.QuietSelect(c, "Autonomy", "shield-check", value, opts)
		},
		"field": func(c *ui.Context, value string) (string, bool) {
			return k.FieldSelect(c, "Autonomy", value, opts, false)
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := "low"
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(k.UITheme(c.Theme()))
				ui.Column(c).Fill().Padding(16).Children(func() {
					if next, ok := sel(c, value); ok {
						value = next
					}
				})
			}, 320, 240)
			tt.Frame()
			if err := tt.Click("Autonomy"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !tt.HasText("Medium") {
				t.Fatalf("the list did not open: %q", tt.Texts())
			}
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if value != "medium" {
				t.Fatalf("chose %q", value)
			}
			if tt.HasText("High") {
				t.Fatalf("the list stayed open: %q", tt.Texts())
			}
		})
	}
}
