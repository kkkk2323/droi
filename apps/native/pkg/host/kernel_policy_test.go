package host

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// TestConversationPoliciesSurviveARestart: a rule an "always allow" answer added outlives the Kernel and the app,
// and an emptied policy leaves nothing behind.
func TestConversationPoliciesSurviveARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	h := &Host{Settings: OpenSettings(file, func(string) string { return "" })}
	rule := kernel.PolicyRule{Kind: "file", Path: "/w/notes.md"}
	if err := h.SetConversationPolicy(7, ConversationPolicy{Rules: []kernel.PolicyRule{rule}}); err != nil {
		t.Fatal(err)
	}
	if err := h.SetConversationPolicy(9, ConversationPolicy{Autonomy: "all"}); err != nil {
		t.Fatal(err)
	}
	held := h.ConversationPolicies()

	again := &Host{Settings: OpenSettings(file, func(string) string { return "" })}
	want := map[kernel.ID]ConversationPolicy{7: {Rules: []kernel.PolicyRule{rule}}, 9: {Autonomy: "all"}}
	if got := again.ConversationPolicies(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after a restart = %+v, want %+v", got, want)
	}
	if err := again.SetConversationPolicy(9, ConversationPolicy{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Settings.Get().ConversationPolicies["9"]; ok {
		t.Fatal("an emptied policy is still stored")
	}
	// What was read before is a copy: the change above did not reach it.
	if held[9].Autonomy != "all" {
		t.Fatalf("a policy read before the change = %+v", held[9])
	}
}
