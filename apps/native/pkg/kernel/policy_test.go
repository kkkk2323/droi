package kernel

import (
	"encoding/json"
	"testing"
)

// policy.set is a change: a nil Rules keeps the list, and an empty one clears
// it. The two must not marshal alike, or a Host can never clear the list.
func TestAPolicyChangeTellsKeepFromClear(t *testing.T) {
	for _, tc := range []struct {
		change PolicyChange
		want   string
	}{
		{PolicyChange{Autonomy: "edits"}, `{"autonomy":"edits"}`},
		{PolicyChange{Rules: &[]PolicyRule{}}, `{"rules":[]}`},
		{PolicyChange{Rules: &[]PolicyRule{{Kind: "tool", Tool: "bash"}}}, `{"rules":[{"kind":"tool","tool":"bash"}]}`},
	} {
		got, err := json.Marshal(tc.change)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Fatalf("marshal %+v = %s, want %s", tc.change, got, tc.want)
		}
	}
	// A snapshot always says both: an empty list is a list.
	got, _ := json.Marshal(Policy{Autonomy: "ask"})
	if string(got) != `{"autonomy":"ask","rules":null}` && string(got) != `{"autonomy":"ask","rules":[]}` {
		t.Fatalf("snapshot = %s", got)
	}
}
