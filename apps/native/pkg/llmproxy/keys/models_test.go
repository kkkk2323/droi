package keys

import "testing"

func TestModelBillingPool(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"claude-fable-5.1", PoolStandard},
		{"claude-opus-5", PoolStandard},
		{"gpt-6-astra", PoolStandard},
		{"kimi-k3", PoolCore},
		{"glm-5.3-flash", PoolCore},
		{"deepseek-v4-flash-0731", PoolCore},
		// Custom-model aliases unwrap to the built-in id.
		{"custom:kimi-k3-0", PoolCore},
		{"custom:glm-5.3-flash-1", PoolCore},
		{"custom:claude-opus-5", PoolStandard},
		// Unknown / missing models default to Standard-only (the safe default).
		{"", PoolAny},
		{"some-unknown-model", PoolStandard},
	}
	for _, tt := range tests {
		if got := ModelBillingPool(tt.model); got != tt.want {
			t.Errorf("ModelBillingPool(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

// droid's registry replaces the built-in list: a model it adds to Core
// routes there, and one it leaves out is Standard-only.
func TestUseCoreModels(t *testing.T) {
	old := droidCore.Load()
	t.Cleanup(func() { droidCore.Store(old) })
	UseCoreModels(nil)
	if droidCore.Load() != old {
		t.Fatal("an empty list replaced the Core models")
	}
	UseCoreModels([]string{"Kimi-K3.1-Pro", "kimi-k3"})
	for model, want := range map[string]string{
		"kimi-k3.1-pro":      PoolCore,
		"custom:kimi-k3-0":   PoolCore,
		"garnet-07-15":       PoolStandard,
		"claude-opus-5-5":    PoolStandard,
		"minimax-m3.1-flash": PoolStandard,
	} {
		if got := ModelBillingPool(model); got != want {
			t.Errorf("ModelBillingPool(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestIsCoreModel(t *testing.T) {
	if !IsCoreModel("kimi-k3") {
		t.Fatal("kimi-k3 should be a core model")
	}
	if IsCoreModel("claude-fable-5.1") {
		t.Fatal("claude-fable-5.1 should not be a core model")
	}
}
