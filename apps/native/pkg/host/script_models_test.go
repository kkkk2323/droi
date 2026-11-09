package host

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
)

func TestTheScriptModelListIsDroidsCacheLessWhatSettingsAdded(t *testing.T) {
	home := t.TempDir()
	h := New(Config{
		UserData:     t.TempDir(),
		Home:         home,
		Env:          func(string) string { return "" },
		ScriptModels: func() []string { return []string{"gemini-4-argon"} },
	})
	if got := h.ScriptModelList(); got.Known || len(got.Factory) != 0 || !reflect.DeepEqual(got.Added, []string{"gemini-4-argon"}) {
		t.Fatalf("without a cache: %+v", got)
	}
	cache := filepath.Join(h.FactoryHome(), "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	flags := `{"userId":"u","flags":{},"configs":{"script_tool_models":{"models":["claude-opus-5-5","gemini-4-argon"]},"other":1}}`
	if err := os.WriteFile(filepath.Join(cache, "feature-flags.json"), []byte(flags), 0o600); err != nil {
		t.Fatal(err)
	}
	want := gateway.ScriptModelList{Factory: []string{"claude-opus-5-5"}, Added: []string{"gemini-4-argon"}, Known: true}
	if got := h.ScriptModelList(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for model, allowed := range map[string]bool{"claude-opus-5-5": true, "gemini-4-argon": true, "claude-sonnet-5-5": false} {
		if got, known := want.Allows(model); got != allowed || !known {
			t.Errorf("Allows(%s) = %v, %v", model, got, known)
		}
	}
	for _, bad := range []string{`{"configs":{}}`, `not json`} {
		if err := os.WriteFile(filepath.Join(cache, "feature-flags.json"), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := h.ScriptModelList(); got.Known {
			t.Errorf("%s: %+v", bad, got)
		}
	}
}
