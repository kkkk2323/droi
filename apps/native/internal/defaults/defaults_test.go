package defaults

import (
	"reflect"
	"slices"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

var modelList = []any{
	map[string]any{"id": "auto", "displayName": "Auto Model", "modelProvider": "factory",
		"supportedReasoningEfforts": []any{"none"}, "isCustom": false, "kind": "router"},
	map[string]any{"id": "gpt-5", "displayName": "GPT-5", "modelProvider": "openai",
		"supportedReasoningEfforts": []any{"low", "medium", "high"}, "isCustom": false},
}

func ptr[T any](v T) *T { return &v }

func TestReadsTheDaemonsAnswer(t *testing.T) {
	v := ToSessionDefaults(map[string]any{
		"reasoningEffort": "high",
		"availableModels": modelList,
		"specSavePresets": map[string]any{"userFactoryDir": "/Users/dev/.factory"},
		"management": map[string]any{
			"modelId":         map[string]any{"disabled": true, "source": "org"},
			"reasoningEffort": map[string]any{"disabled": false, "source": "builtin"},
			"subagent":        map[string]any{"heavyModel": map[string]any{"disabled": true, "source": "org"}},
		},
	})
	if v.ModelID != "" || v.ReasoningEffort != "high" || v.InteractionMode != "auto" ||
		v.CompactionTokenLimit != 0 || len(v.CompactionTokenLimitPerModel) != 0 ||
		v.CompactionModel != "current-model" || !v.CompactionThresholdCheckEnabled ||
		v.SubagentAutonomyLevel != "inherit" || len(v.SubagentModelSettings) != 0 ||
		!reflect.DeepEqual(v.AvailableAutonomyLevels, []string{"off", "low", "medium", "high"}) ||
		v.UserFactoryDir != "/Users/dev/.factory" {
		t.Errorf("view = %+v", v)
	}
	if len(v.Models) != 2 || v.Models[0].ID != "auto" || v.Models[1].ID != "gpt-5" || v.Models[0].Provider != "" {
		t.Errorf("models = %+v", v.Models)
	}
	var locked []string
	for k := range v.Locked {
		locked = append(locked, k)
	}
	slices.Sort(locked)
	if !reflect.DeepEqual(locked, []string{"modelId", "subagent.heavyModel"}) {
		t.Errorf("locked = %v", locked)
	}
}

func TestPatchShowsAtOnce(t *testing.T) {
	view := ToSessionDefaults(map[string]any{"specModeModelId": "gpt-5", "compactionTokenLimit": 300_000.0})
	next := ApplyPatch(view, Patch{SpecModeModelID: ptr(""), CompactionTokenLimit: ptr(500_000)})
	if next.SpecModeModelID != "" || next.CompactionTokenLimit != 500_000 {
		t.Errorf("next = %+v", next)
	}
	if view.SpecModeModelID != "gpt-5" {
		t.Error("the patch changed the view it started from")
	}
}

func TestPatchParams(t *testing.T) {
	got := Patch{SpecModeModelID: ptr(""), ModelID: ptr("gpt-5"), CompactionTokenLimit: ptr(500_000)}.Params()
	want := map[string]any{"specModeModelId": nil, "modelId": "gpt-5", "compactionTokenLimit": 500_000}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Params = %v, want %v", got, want)
	}
}

// The worktree settings read from the Daemon and go back as its fields;
// "" puts the Daemon's default folder back.
func TestWorktreeSettings(t *testing.T) {
	view := ToSessionDefaults(map[string]any{"worktreeDirectory": "/trees", "worktreeAutoDeleteLimit": 20.0})
	if view.WorktreeDirectory != "/trees" || view.WorktreeAutoDeleteLimit != 20 {
		t.Fatalf("view = %+v", view)
	}
	p := Patch{WorktreeDirectory: ptr(""), WorktreeAutoDeleteLimit: ptr(5)}
	if got, want := p.Params(), map[string]any{"worktreeDirectory": nil, "worktreeAutoDeleteLimit": 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("Params = %v, want %v", got, want)
	}
	if next := ApplyPatch(view, p); next.WorktreeDirectory != "" || next.WorktreeAutoDeleteLimit != 5 {
		t.Errorf("next = %+v", next)
	}
}

func TestReasoningChoices(t *testing.T) {
	list := ToSessionDefaults(map[string]any{"availableModels": modelList}).Models
	for _, c := range []struct {
		model, current string
		want           []string
	}{
		{"gpt-5", "high", []string{"low", "medium", "high"}},
		{"gone", "xhigh", []string{"xhigh"}},
		{"", "", []string{"medium"}},
	} {
		if got := ReasoningChoices(list, c.model, c.current); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ReasoningChoices(%q, %q) = %v, want %v", c.model, c.current, got, c.want)
		}
	}
}

func TestPickableModels(t *testing.T) {
	list := ToSessionDefaults(map[string]any{"availableModels": modelList}).Models
	if got := len(PickableModels(list, true)); got != 2 {
		t.Errorf("with routers: %d", got)
	}
	if got := PickableModels(list, false); len(got) != 1 || got[0].ID != "gpt-5" {
		t.Errorf("without routers: %v", got)
	}
}

func TestSubagentTier(t *testing.T) {
	settings := SubagentModelSettings{"lightModel": "gpt-5", "lightReasoningEffort": "low", "heavyModel": "opus"}
	with := func(extra SubagentModelSettings) SubagentModelSettings {
		out := SubagentModelSettings{}
		for k, v := range settings {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	for _, c := range []struct {
		name      string
		got, want SubagentModelSettings
	}{
		{"model of another tier", WithSubagentTierModel(settings, "medium", "gpt-5"), with(SubagentModelSettings{"mediumModel": "gpt-5"})},
		{"reasoning", WithSubagentTierReasoning(settings, "light", "high"), with(SubagentModelSettings{"lightReasoningEffort": "high"})},
		{"inherit drops model and level", WithSubagentTierModel(settings, "light", ""), SubagentModelSettings{"heavyModel": "opus"}},
		{"no level", WithSubagentTierReasoning(settings, "light", ""), SubagentModelSettings{"lightModel": "gpt-5", "heavyModel": "opus"}},
		{"a new model starts from its own default level", WithSubagentTierModel(settings, "light", "opus"), SubagentModelSettings{"lightModel": "opus", "heavyModel": "opus"}},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if settings["lightModel"] != "gpt-5" || len(settings) != 3 {
		t.Error("a tier change edited the settings it started from")
	}
}

func TestSpecFolders(t *testing.T) {
	paths := SpecSavePaths("/Users/dev/.factory")
	if paths != (SpecPaths{User: "~/.factory/docs", Project: ".factory/docs"}) {
		t.Errorf("paths = %+v", paths)
	}
	if SpecSavePaths("") != paths {
		t.Error("nothing stored should read as the home folder")
	}
	for stored, want := range map[string]SpecSaveChoice{
		"": SpecUser, "~/.factory/docs": SpecUser, ".factory/docs": SpecProject, "/Users/dev/specs": SpecCustom,
	} {
		if got := SpecSaveChoiceOf(stored, paths); got != want {
			t.Errorf("SpecSaveChoiceOf(%q) = %q, want %q", stored, got, want)
		}
	}
}

func TestTokenLimitLabel(t *testing.T) {
	if got := TokenLimitLabel(250_000); got != "250K" {
		t.Errorf("got %q", got)
	}
	if got := TokenLimitLabel(1_000_000); got != "1M" {
		t.Errorf("got %q", got)
	}
}

func TestNewSessionToolMode(t *testing.T) {
	for _, c := range []struct {
		name                     string
		choice, fallback, draft  ToolMode
		wantShown, wantRequested ToolMode
	}{
		{"droid decides, the draft shows", "", "", ScriptOnly, ScriptOnly, ""},
		{"device default before the draft answers", "", ScriptOnly, "", ScriptOnly, ScriptOnly},
		{"page choice wins", DirectOnly, ScriptOnly, ScriptOnly, DirectOnly, DirectOnly},
		{"nothing yet", "", "", "", "", ""},
	} {
		shown, requested := NewSessionToolMode(c.choice, c.fallback, c.draft)
		if shown != c.wantShown || requested != c.wantRequested {
			t.Errorf("%s: got (%q, %q)", c.name, shown, requested)
		}
	}
}

func TestStoredToolMode(t *testing.T) {
	s := prefs.Memory()
	s.Set("droi.toolExecutionMode", "script_and_direct")
	if !IsToolMode("direct_and_script") || IsToolMode("script_and_direct") {
		t.Error("IsToolMode")
	}
	if got := DefaultToolMode(s); got != "" {
		t.Errorf("an unknown mode read as %q", got)
	}
	SetDefaultToolMode(s, ScriptOnly)
	if v, _ := s.Get("droi.toolExecutionMode"); v != "script_only" {
		t.Errorf("stored %q", v)
	}
	SetDefaultToolMode(s, "")
	if v, _ := s.Get("droi.toolExecutionMode"); v != "" {
		t.Errorf("stored %q", v)
	}
	if DefaultToolMode(s) != "" {
		t.Error("cleared default should read as no choice")
	}
}
