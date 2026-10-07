package brands

import "testing"

func TestEveryBrandWithAMarkParses(t *testing.T) {
	for _, b := range []string{"anthropic", "openai", "google", "xai", "zhipu", "moonshot", "deepseek", "minimax"} {
		if Mark(b) == nil {
			t.Errorf("%s has no mark", b)
		}
	}
	if Mark("other") != nil {
		t.Error("other should fall back to sparkles")
	}
}
