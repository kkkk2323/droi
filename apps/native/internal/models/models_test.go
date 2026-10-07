package models

import (
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
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
	} {
		if got := BrandOf(c.id, c.provider); got != c.want {
			t.Errorf("BrandOf(%q, %q) = %q, want %q", c.id, c.provider, got, c.want)
		}
	}
}

func TestBrandsOf(t *testing.T) {
	want := []Brand{Anthropic, OpenAI, Google, Zhipu, Other}
	if got := BrandsOf(list); !reflect.DeepEqual(got, want) {
		t.Errorf("BrandsOf = %v, want %v", got, want)
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
