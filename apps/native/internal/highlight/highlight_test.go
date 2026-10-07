package highlight

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Shiki's github-light colors for this line: `function` red, `login`
// purple, `user` orange.
func TestMatchesShiki(t *testing.T) {
	spans := Sync("ts", "function login(user: string) {}", "github-light")
	color := map[string]ui.Color{}
	var text string
	for _, s := range spans {
		text += s.Text
		color[s.Text] = s.Color
	}
	if text != "function login(user: string) {}" {
		t.Fatalf("text %q", text)
	}
	for word, hex := range map[string]string{"function": "#D73A49", "login": "#6F42C1", "user": "#E36209"} {
		if color[word] != ui.Hex(hex) {
			t.Errorf("%s: %v, want %s (%v)", word, color[word], hex, spans)
		}
	}
}

func TestForeground(t *testing.T) {
	if Foreground("github-light") != ui.Hex("#24292e") {
		t.Fatal(Foreground("github-light"))
	}
}

func TestUnknownLanguageIsPlain(t *testing.T) {
	if s := Sync("no-such-lang", "a\nb", "github-dark"); len(s) != 1 || s[0].Text != "a\nb" {
		t.Fatal(s)
	}
	if s := Sync("go", "x := 1", "solarized-light-plus"); len(s) < 2 {
		t.Fatal("solarized theme loads", s)
	}
}
