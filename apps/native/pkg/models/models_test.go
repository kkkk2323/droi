package models

import (
	"reflect"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

func model(id, provider, label string) Choice {
	return Choice{ID: id, Label: label, Provider: provider}
}

var list = []Choice{
	model("auto", "", "Auto Model"),
	model("claude-opus-4-1", "anthropic", "Claude Opus 4.1"),
	model("gpt-5", "openai", "GPT-5"),
	model("glm-5.3-flash", "anthropic", "GLM 5.3 Flash"),
	model("gemini-3-pro", "generic-chat-completion-api", "Gemini 3 Pro"),
}

func ids(rows []Row) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestBrandOf(t *testing.T) {
	// The model id comes before the wire provider.
	for _, c := range []struct {
		id, provider string
		want         Brand
	}{
		{"glm-5.3-flash", "anthropic", Zhipu},
		{"gemini-3-pro", "generic-chat-completion-api", Google},
		{"claude-opus-4-1", "anthropic", Anthropic},
		{"my-fine-tune", "openai", OpenAI},
		{"auto", "", Other},
		{"mystery", "generic-chat-completion-api", Other},
		// A gateway such as Magpie names a model "provider/model".
		{"workbuddy/glm-5.3-flash", "generic-chat-completion-api", Zhipu},
		{"codex/gpt-6.1-sol", "generic-chat-completion-api", OpenAI},
		{"grok/grok-4.7", "generic-chat-completion-api", XAI},
		{"openrouter/anthropic/claude-opus-5.5", "generic-chat-completion-api", Anthropic},
		{"raccoon/sn-sensenova-6-8-flash", "generic-chat-completion-api", Other},
		// What the picker really gets for a Magpie model: Droi's own id, which
		// names the gateway's provider after "custom:magpie-".
		{"custom:magpie-codex-gpt-6-1-sol-0a1b2c", "generic-chat-completion-api", OpenAI},
		{"custom:magpie-workbuddy-glm-5-3-4d5e6f", "generic-chat-completion-api", Zhipu},
		{"custom:magpie-grok-grok-4-7-abcdef", "generic-chat-completion-api", XAI},
		{"custom:magpie-workbuddy-kimi-k2-7-123456", "generic-chat-completion-api", Moonshot},
		{"custom:magpie-raccoon-sn-sensenova-6-8-flash-999999", "generic-chat-completion-api", Other},
	} {
		if got := BrandOf(c.id, c.provider); got != c.want {
			t.Errorf("BrandOf(%q, %q) = %q, want %q", c.id, c.provider, got, c.want)
		}
	}
}

// A custom model's id that names no company takes the brand of the built-in
// model droid 1.1 says it behaves like; an id that does name one keeps it.
func TestABehavesLikeModelGivesTheBrand(t *testing.T) {
	custom := true
	choices := ToChoices([]protocol.ModelMetadata{
		{ID: "custom:magpie-raccoon-sn-sensenova-6-8-flash-999999", ModelProvider: "generic-chat-completion-api", IsCustom: &custom, BehavesLikeModelID: "glm-5.3-flash"},
		{ID: "custom:my-coder", ModelProvider: "anthropic", IsCustom: &custom, BehavesLikeModelID: "kimi-k3"},
		{ID: "custom:kimi-k3-0", ModelProvider: "generic-chat-completion-api", IsCustom: &custom, BehavesLikeModelID: "glm-5"},
		{ID: "custom:my-coder-2", ModelProvider: "openai", IsCustom: &custom},
	})
	var got []Brand
	for _, c := range choices {
		got = append(got, c.Brand())
	}
	if want := []Brand{Zhipu, Moonshot, Moonshot, OpenAI}; !reflect.DeepEqual(got, want) {
		t.Errorf("brands %v, want %v", got, want)
	}
	if choices[0].BehavesLike != "glm-5.3-flash" || choices[0].Multiplier != nil {
		t.Errorf("choice %+v", choices[0])
	}
}

func TestBrandsOf(t *testing.T) {
	want := []Brand{Anthropic, OpenAI, Google, Zhipu, Other}
	if got := BrandsOf(list); !reflect.DeepEqual(got, want) {
		t.Errorf("BrandsOf = %v, want %v", got, want)
	}
}

// A Magpie model is no brand tab's: the gateway serves models named like the
// Daemon's own, so its models get a tab of their own instead.
func TestMagpieModelsHaveTheirOwnTab(t *testing.T) {
	withMagpie := append(append([]Choice(nil), list...),
		model("custom:magpie-codex-gpt-6-1-sol-0a1b2c", "generic-chat-completion-api", "Magpie · GPT-6.1-Sol"),
		model("custom:magpie-workbuddy-glm-5-3-4d5e6f", "generic-chat-completion-api", "Magpie · GLM 5.3"),
	)
	if got, want := BrandsOf(withMagpie), BrandsOf(list); !reflect.DeepEqual(got, want) {
		t.Errorf("BrandsOf with Magpie = %v, want %v", got, want)
	}
	if !HasMagpie(withMagpie) || HasMagpie(list) {
		t.Errorf("HasMagpie: %v, %v", HasMagpie(withMagpie), HasMagpie(list))
	}
	// Everything but the Magpie tab leaves the gateway's models out; a model
	// starred by hand stays in the favorites, where the user put it.
	for _, filter := range []PickerFilter{FilterAll, PickerFilter(Anthropic)} {
		for _, row := range VisibleModels(withMagpie, nil, filter, "") {
			if IsMagpie(row.ID) {
				t.Errorf("%s lists the Magpie model %q", filter, row.ID)
			}
		}
	}
	if got := ids(VisibleModels(withMagpie, []string{withMagpie[5].ID}, FilterFavorites, "")); !reflect.DeepEqual(got,
		[]string{withMagpie[5].ID}) {
		t.Errorf("the favorites = %v", got)
	}
	wantMagpie := []string{withMagpie[5].ID, withMagpie[6].ID}
	if got := ids(VisibleModels(withMagpie, nil, FilterMagpie, "")); !reflect.DeepEqual(got, wantMagpie) {
		t.Errorf("the Magpie tab = %v, want %v", got, wantMagpie)
	}
	// The search reaches both lists, whichever tab is open.
	if got := ids(VisibleModels(withMagpie, nil, FilterMagpie, "gpt")); !reflect.DeepEqual(got,
		[]string{"gpt-5", withMagpie[5].ID}) {
		t.Errorf("the search = %v", got)
	}
}

func TestVisibleModels(t *testing.T) {
	all := []string{"auto", "claude-opus-4-1", "gpt-5", "glm-5.3-flash", "gemini-3-pro"}
	for _, c := range []struct {
		name      string
		favorites []string
		filter    PickerFilter
		query     string
		want      []string
	}{
		{"everything by default", nil, FilterAll, "", all},
		{"by brand", nil, PickerFilter(Zhipu), "", []string{"glm-5.3-flash"}},
		{"favorites in starred order", []string{"gpt-5", "auto"}, FilterFavorites, "", []string{"gpt-5", "auto"}},
		{"search label ignores the filter", nil, PickerFilter(OpenAI), "flash", []string{"glm-5.3-flash"}},
		{"search brand ignores the filter", nil, FilterFavorites, "zhipu", []string{"glm-5.3-flash"}},
		{"search every word", nil, FilterAll, "opus 4.1", []string{"claude-opus-4-1"}},
	} {
		if got := ids(VisibleModels(list, c.favorites, c.filter, c.query)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMultipliers(t *testing.T) {
	yes := true
	one, six := 1.0, 1.6
	base := protocol.ModelMetadata{DisplayName: "m", ModelProvider: "anthropic"}
	opus, mine, unpriced := base, base, base
	opus.ID, opus.TokenMultiplier = "opus", &six
	mine.ID, mine.IsCustom, mine.TokenMultiplier = "mine", &yes, &one
	unpriced.ID = "unpriced"
	var got []*float64
	for _, c := range ToChoices([]protocol.ModelMetadata{opus, mine, unpriced}) {
		got = append(got, c.Multiplier)
	}
	if len(got) != 3 || got[0] == nil || *got[0] != 1.6 || got[1] != nil || got[2] != nil {
		t.Errorf("multipliers = %v, want [1.6 nil nil]", got)
	}
}

func TestFormatMultiplier(t *testing.T) {
	for in, want := range map[float64]string{1.6: "1.6×", 0.04: "0.04×", 1: "1×", 0.333333: "0.33×"} {
		if got := FormatMultiplier(in); got != want {
			t.Errorf("FormatMultiplier(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeChoices(t *testing.T) {
	got, err := DecodeChoices([]byte(`[
		{"id":"auto","displayName":"Auto Model","modelProvider":"factory","supportedReasoningEfforts":["none"],"kind":"router"},
		{"id":"gpt-5","displayName":"GPT-5","modelProvider":"openai","supportedReasoningEfforts":["low","high"],"disabled":true}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Provider != "" || got[1].Provider != "openai" || got[0].Disabled || !got[1].Disabled ||
		!reflect.DeepEqual(got[1].ReasoningEfforts, []string{"low", "high"}) {
		t.Errorf("DecodeChoices = %+v", got)
	}
}
