package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/app/testsupport"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// kernelMeter is the meter's figures for Session id as Kernel's conversation.usage has them now.
func (h *harness) kernelMeter(id string) string {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u, err := h.kernel.client(h.t).Usage(ctx, h.kernel.convs[id])
	if err != nil {
		h.t.Fatal(err)
	}
	var window struct {
		Used  float64 `json:"used"`
		Limit float64 `json:"limit"`
	}
	if err := json.Unmarshal(u.Context, &window); err != nil || window.Limit <= 0 {
		h.t.Fatalf("Kernel's context %s (%v)", u.Context, err)
	}
	return contextLabel(window.Used, window.Limit)
}

// A Kernel conversation's meter is conversation.usage's context, and it is read again once a model call is counted.
func TestAKernelConversationsMeterIsKernelsUsage(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Deployed to staging, all checks green."}}), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.openSession("Deploy")
	id := h.a.Route().SessionID
	before := h.kernelMeter(id)
	h.until("Kernel's figures "+before, func() bool { return h.hasText(before) })
	h.send("Ship it to staging and tell me how it went in a few words")
	h.until("the reply", func() bool { return h.hasText("Deployed to staging, all checks green.") })
	h.until("the figures after the call", func() bool {
		now := h.kernelMeter(id)
		return now != before && h.hasText(now)
	})
}

// The meter follows what the conversation holds, not only the model calls: an entry written while it rests (as a
// compaction writes one) changes how full the window is, and no call is counted for it.
func TestAKernelConversationsMeterFollowsWhatItHoldsAtRest(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Deployed."}}), "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	h.openSession("Deploy")
	id := h.a.Route().SessionID
	before := h.kernelMeter(id)
	h.until("Kernel's figures "+before, func() bool { return h.hasText(before) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	long := strings.Repeat("Here is the whole deploy log from last night, line by line. ", 400)
	entries, err := testsupport.History([]fakedaemon.Message{{Role: "user", Text: long}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.kernel.client(t).Import(ctx, kernel.ImportParams{ID: h.kernel.convs[id], Entries: entries}); err != nil {
		t.Fatal(err)
	}
	after := h.kernelMeter(id)
	if after == before {
		t.Fatalf("Kernel's figures did not move: %s", after)
	}
	h.until("the figures with the new entry "+after, func() bool { return h.hasText(after) })
}
