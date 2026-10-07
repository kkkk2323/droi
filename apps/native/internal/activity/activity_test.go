package activity

import (
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

func TestCountBusy(t *testing.T) {
	var list []sessions.Summary
	for _, id := range []string{"a", "b", "c", "d"} {
		list = append(list, sessions.Summary{SessionID: id})
	}

	t.Run("counts working (compacting and running subagents too) apart from waiting", func(t *testing.T) {
		got, ok := CountBusy(list, map[string]Activity{"a": Working, "b": NeedsInput, "c": Compacting}, map[string]int{"d": 2})
		if !ok || got != (Busy{Working: 3, NeedsInput: 1}) {
			t.Errorf("got %+v, %v", got, ok)
		}
	})

	t.Run("is nothing when nothing is going on", func(t *testing.T) {
		if _, ok := CountBusy(list, nil, map[string]int{"a": 0}); ok {
			t.Error("expected no busy Sessions")
		}
	})
}

func TestOf(t *testing.T) {
	for state, want := range map[string]Activity{
		"compacting_conversation":       Compacting,
		"executing_tool":                Working,
		"waiting_for_tool_confirmation": NeedsInput,
		"idle":                          "",
		"":                              "",
	} {
		if got := Of(state); got != want {
			t.Errorf("Of(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestSnapshot(t *testing.T) {
	got := Snapshot(
		map[string]string{"a": "thinking", "b": "waiting_for_tool_confirmation", "c": "idle", "d": "thinking"},
		map[string]string{"a": "idle", "c": "executing_tool"},
		map[string]bool{"d": true, "e": true},
	)
	want := map[string]Activity{"b": NeedsInput, "c": Working, "d": Compacting, "e": Compacting}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
