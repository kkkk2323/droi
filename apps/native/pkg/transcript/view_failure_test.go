package transcript_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

func viewWithLive(live string) kernel.View {
	question, _ := json.Marshal([]map[string]any{{"role": "user", "content": "hello", "timestamp": 1}})
	return kernel.View{
		Entries: []kernel.Entry{{ID: 1, Kind: kernel.EntryUser, Model: question}},
		Docs:    map[string]json.RawMessage{"pi.live": json.RawMessage(live)},
	}
}

// TestARunThatEndedWithoutAnAnswerSaysWhy: a model that failed, or no model at all, writes no answer; the row after
// the question says why, so the turn does not simply stop. A run the user stopped, one in progress, and one with
// nothing to answer say nothing.
func TestARunThatEndedWithoutAnAnswerSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		name, live, want string
	}{
		{"a model error", `{"runFailure":{"taskId":4,"reason":"model_error","detail":"429 rate limited"}}`, "429 rate limited"},
		{"a model error without words", `{"runFailure":{"taskId":4,"reason":"model_error"}}`, "The model could not answer this turn."},
		{"no model", `{"runFailure":{"taskId":4,"reason":"no_model","detail":"fake/none"}}`, "No model is set for this conversation."},
		{"the turn limit", `{"runFailure":{"taskId":4,"reason":"turn_limit","detail":"50 turns"}}`, "50 turns"},
		{"a fault", `{"runFailure":{"taskId":4,"reason":"faulted","detail":"storage rejected the commit"}}`, "storage rejected the commit"},
		{"stopped by the user", `{"runFailure":{"taskId":4,"reason":"aborted"}}`, ""},
		{"nothing to answer", `{"runFailure":{"taskId":4,"reason":"nothing_to_answer"}}`, ""},
		{"a run in progress", `{"run":{"taskId":5,"inputs":[2],"turns":0},"runFailure":{"taskId":4,"reason":"model_error","detail":"x"}}`, ""},
		{"no failure", `{}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := transcript.BuildView(viewWithLive(tc.live))
			last := entries[len(entries)-1]
			if tc.want == "" {
				if len(entries) != 1 {
					t.Fatalf("a row after the question: %+v", last)
				}
				return
			}
			if len(entries) != 2 || !last.IsError || !strings.Contains(last.Blocks[0].Text, tc.want) {
				t.Fatalf("the rows are %d, the last %+v; want an error row saying %q", len(entries), last, tc.want)
			}
		})
	}
}

// TestARealKernelModelFailureIsARow: the provider refuses the request; Kernel ends the run with no answer, and the
// window's transcript says so after the question.
func TestARealKernelModelFailureIsARow(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Failure: &fakekernel.Failure{Status: 400, Body: `{"error":{"message":"the request is not valid"}}`}},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.Submit(ctx, kernel.SubmitParams{ID: id, Input: "hello"}); err != nil {
		t.Fatal(err)
	}
	var entries []*transcript.Entry
	for {
		opened, err := c.Open(ctx, kernel.OpenParams{ID: id, Window: &fakekernel.DefaultWindow})
		if err != nil {
			t.Fatal(err)
		}
		entries = transcript.BuildView(opened.View)
		if n := len(entries); n > 0 && entries[n-1].IsError {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("no failure row: %s\n%s", describe(entries), k.Logs())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(entries) != 2 || !entries[0].User {
		t.Fatalf("the rows are %s, want the question and the failure", describe(entries))
	}
	if text := entries[1].Blocks[0].Text; text == "" {
		t.Error("the failure row says nothing")
	}
}
