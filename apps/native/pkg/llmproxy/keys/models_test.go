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

func TestIsCoreModel(t *testing.T) {
	if !IsCoreModel("kimi-k3") {
		t.Fatal("kimi-k3 should be a core model")
	}
	if IsCoreModel("claude-fable-5.1") {
		t.Fatal("claude-fable-5.1 should not be a core model")
	}
}
