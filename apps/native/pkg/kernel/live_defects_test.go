package kernel

import (
	"testing"
	"time"
)

// TestLiveKernelConfigDefaultAgent is the contract's promise that the config
// file's agent is what a conversation runs with when the request names none.
//
// It failed against the kernel of PR #227 until 6cfd5c6c: that build read
// `agent` from the config file but never applied it, so a turn ended with
// `runFailure.reason: "no_model"` and no model request was made.
func TestLiveKernelConfigDefaultAgent(t *testing.T) {
	provider := startFakeProvider(t)
	k := startLiveKernel(t, provider)
	ctx := t.Context()

	// No agent in the request: the kernel's own default has to carry it.
	created, err := k.Client.Create(ctx, CreateParams{Kind: "chat", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("create: %v\n%s", err, k.Log.String())
	}
	id := created.ConversationID
	// The run's own notifications, from the submit on: the idle state that
	// follows an open is not the end of a turn.
	runs := make(chan BusyNotification, 16)
	submitted := make(chan struct{})
	go func() {
		<-submitted
		for n := range k.Client.Notifications() {
			if n.Method != NotifyConversationBusy {
				continue
			}
			var b BusyNotification
			if n.Decode(&b) == nil && b.ID == id {
				select {
				case runs <- b:
				default:
				}
			}
		}
	}()
	if _, err := k.Client.Open(ctx, OpenParams{ID: id}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := k.Client.Submit(ctx, SubmitParams{ID: id, Input: "hello with the default agent"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	close(submitted)

	// Wait for the run to start, then for it to settle: a turn that never
	// starts is a failure of its own, not an answer.
	deadline := time.After(60 * time.Second)
	started := false
	for {
		select {
		case b := <-runs:
			if b.Busy() {
				started = true
				continue
			}
			if !started {
				continue
			}
			if len(b.RunFailure) > 0 {
				t.Fatalf("a conversation that named no agent could not run: %s\n"+
					"the config file's agent was not applied (kernel defect)", b.RunFailure)
			}
			if provider.calls.Load() == 0 {
				t.Fatal("the turn settled without asking the model")
			}
			return
		case <-deadline:
			t.Fatalf("the turn never settled (started: %v):\n%s", started, k.Log.String())
		}
	}
}

// TestLiveKernelForkNeedsAnEntry pins how a fork without `at` answers: the
// contract makes `at` required, so a request without it is invalid params, not
// an internal error. The kernel of PR #227 answered -32603 until 6cfd5c6c
// (GitSHA 9becba05) fixed it.
func TestLiveKernelForkNeedsAnEntry(t *testing.T) {
	provider := startFakeProvider(t)
	k := startLiveKernel(t, provider)
	ctx := t.Context()
	model := ModelRef{Provider: "fake", ModelID: "fake-1"}
	created, err := k.Client.Create(ctx, CreateParams{Kind: "chat", Cwd: t.TempDir(), Agent: &AgentChange{Model: Some(model)}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := k.Client.Fork(ctx, ForkParams{ID: created.ConversationID}); err == nil {
		t.Fatal("a fork with no entry succeeded")
	} else if !Is(err, CodeInvalidParams) {
		t.Fatalf("a fork with no entry answered %v, want -32602 invalid params", err)
	}
}
