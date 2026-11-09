package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// A Kernel conversation's speed under the composer is what its answers recorded: Pi writes each call's durationMs
// and usage.output on the answer, and 400 tokens in 5 s read as 80 tok/s.
func TestAKernelConversationsSpeedIsWhatItsAnswersRecorded(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Deployed."}}), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.openSession("Deploy")
	id := h.a.Route().SessionID
	at := float64(time.Now().Add(-time.Minute).UnixMilli())
	answer, err := json.Marshal([]map[string]any{{
		"role": "assistant", "api": "anthropic-messages", "provider": "factory-anthropic", "model": "claude-opus-5-5",
		"content":    []map[string]any{{"type": "text", "text": "Done."}},
		"usage":      map[string]any{"input": 10, "output": 400, "cacheRead": 0, "cacheWrite": 0, "totalTokens": 410},
		"stopReason": "stop", "timestamp": at, "durationMs": 5000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entries := []kernel.ImportEntry{{Kind: kernel.EntryAssistant, Model: answer}}
	if _, err := h.kernel.client(t).Import(ctx, kernel.ImportParams{ID: h.kernel.convs[id], Entries: entries}); err != nil {
		t.Fatal(err)
	}
	h.until("the recorded speed under the composer", func() bool { return h.hasText("80 tok/s") })
}
