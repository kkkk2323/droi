package sessionview

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// The model speed on the Kernel: a call this window watched stream is timed from its first token, as the Daemon's
// were; the figure under the composer follows the answer while it streams.

func liveWith(t *testing.T, started float64, text string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"run": map[string]any{"taskId": 1, "inputs": []int{}, "turns": 0},
		"generation": map[string]any{"message": map[string]any{
			"role": "assistant", "timestamp": started, "content": []map[string]any{{"type": "text", "text": text}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func answer(t *testing.T, id kernel.ID, started, durationMs, output float64) kernel.Entry {
	t.Helper()
	model, _ := json.Marshal([]map[string]any{{
		"role": "assistant", "timestamp": started, "durationMs": durationMs,
		"usage": map[string]any{"output": output}, "content": []map[string]any{{"type": "text", "text": "x"}},
	}})
	return kernel.Entry{ID: id, ConversationID: 7, Kind: kernel.EntryAssistant, Model: model}
}

func TestTheKernelSpeedIsTimedFromTheFirstTokenThisWindowSaw(t *testing.T) {
	clock := time.UnixMilli(10_000)
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	s := NewState(7)
	push := func(live json.RawMessage, entries ...kernel.Entry) {
		view := kernel.View{Conversation: kernel.Conversation{ID: 7}, Entries: entries, Docs: map[string]json.RawMessage{}}
		if live != nil {
			view.Docs["pi.live"] = live
		}
		s.Apply(kernel.ViewNotification{ID: 7, Kind: kernel.ViewKindValue, Value: &view})
	}
	user := entryOf(t, 1, kernel.EntryUser, "go", 9_000)
	// The request started at 10 s; nothing has come yet.
	push(liveWith(t, 10_000, ""), user)
	if rate, streaming := s.LiveRate(clock); rate != 0 || !streaming {
		t.Fatalf("before the first token: %v, streaming %v", rate, streaming)
	}
	// The first token two seconds in.
	clock = time.UnixMilli(12_000)
	push(liveWith(t, 10_000, "ab"), user)
	// Two seconds later the answer holds 400 characters: about 100 tokens in 2 s.
	clock = time.UnixMilli(14_000)
	push(liveWith(t, 10_000, string(make([]byte, 400))), user)
	if rate, _ := s.LiveRate(clock); rate < 49 || rate > 51 {
		t.Fatalf("the live rate is %v, want about 50", rate)
	}
	// Written: 6 s in all, 200 tokens; and an older answer this window did not watch.
	push(nil, answer(t, 0, 1_000, 4_000, 100), user, answer(t, 2, 10_000, 6_000, 200))
	if _, streaming := s.LiveRate(clock); streaming {
		t.Fatal("nothing streams once the answer is written")
	}
	st := s.Stats()
	if st.Calls != 2 || st.TTFTCalls != 1 || st.TTFTMs != 2_000 {
		t.Fatalf("stats %+v", st)
	}
	// The watched call decodes 200 tokens in 4 s; the other 100 in its whole 4 s.
	if got := st.Last.TokensPerSecond(); got != 50 {
		t.Errorf("the last call's speed is %v, want 50", got)
	}
	if got := st.TokensPerSecond(); got != 300.0/8 {
		t.Errorf("the average is %v, want %v", got, 300.0/8)
	}
	if st.ModelMs != 10_000 {
		t.Errorf("time in the model %v", st.ModelMs)
	}
}
