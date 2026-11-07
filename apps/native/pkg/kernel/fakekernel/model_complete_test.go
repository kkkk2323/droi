package fakekernel

import (
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// TestModelCompleteSendsItsHeader proves the Kernel marks a `model.complete` request, which is the only
// way a scripted model can tell it from a conversation's: pi-ai sends both as a stream.
//
// The header is not part of the contract — the Host's LLM proxy strips every `X-Droi-*` header before a
// request leaves the machine — so this test is about the Kernel saying what it is doing, not about what
// Factory receives.
func TestModelCompleteSendsItsHeader(t *testing.T) {
	k := Start(t, Scenario{Steps: []Step{{Match: "name this conversation", Text: "A name"}}})
	ctx, cancel := k.context(t, 60*time.Second)
	defer cancel()
	c := k.Dial(t)

	answer, err := c.ModelComplete(ctx, kernel.ModelCompleteParams{
		Model: kernel.ModelRef{Provider: ProviderID, ModelID: ModelID},
		Input: "name this conversation",
	})
	if err != nil {
		t.Fatalf("model.complete: %v\n%s", err, k.Logs())
	}
	if answer.Text != "A name" {
		t.Fatalf("the answer is %q, not the scripted one", answer.Text)
	}

	requests := k.Requests()
	if len(requests) != 1 {
		t.Fatalf("the Kernel made %d model requests, not one", len(requests))
	}
	if got := requests[0].Header("X-Droi-Call"); got != "model.complete" {
		t.Errorf("the request carries X-Droi-Call %q, not model.complete", got)
	}
	if unmatched := k.Unmatched(); unmatched != 0 {
		t.Errorf("%d model requests matched no step", unmatched)
	}
}
