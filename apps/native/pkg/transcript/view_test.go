package transcript_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// The tests in this file cover the Kernel's transcript: Pi entries in, the blocks the window draws out.
// They are written against a View this file builds, so they need no Kernel, no model and no history
// constructor: BuildView is a pure function over what conversation.open answers.

// viewOf makes a view over entries, as conversation.open answers it.
func viewOf(entries ...kernel.Entry) kernel.View {
	return kernel.View{Entries: entries}
}

// entry makes one Pi entry: its kind, its model messages (as JSON, or nil) and its data.
func entry(t *testing.T, id int64, kind string, model, data any) kernel.Entry {
	t.Helper()
	e := kernel.Entry{ID: kernel.ID(id), Kind: kind}
	if model != nil {
		raw, err := json.Marshal(model)
		if err != nil {
			t.Fatalf("entry %d's model: %v", id, err)
		}
		e.Model = raw
	}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("entry %d's data: %v", id, err)
		}
		e.Data = raw
	}
	return e
}

// userMessage is a pi.user entry's model: one user message with these blocks (or with plain text when text is
// given and blocks is nil).
func userMessage(t *testing.T, at float64, blocks ...map[string]any) []map[string]any {
	t.Helper()
	return []map[string]any{{"role": "user", "content": blocks, "timestamp": at}}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func assistantMessage(t *testing.T, at float64, stop string, blocks ...map[string]any) []map[string]any {
	t.Helper()
	m := map[string]any{"role": "assistant", "content": blocks, "timestamp": at}
	if stop != "" {
		m["stopReason"] = stop
	}
	return []map[string]any{m}
}

func toolResultMessage(t *testing.T, at float64, callID string, isError bool, blocks ...map[string]any) []map[string]any {
	t.Helper()
	return []map[string]any{{
		"role": "toolResult", "toolCallId": callID, "toolName": "read",
		"content": blocks, "isError": isError != false && isError, "timestamp": at,
	}}
}

// TestBuildViewMapsOneTurn is the plain shape every window shows: what the user said, what the assistant
// thought and said, and the tool it asked for. One user entry and one answer entry come out, with the blocks
// in the order the model produced them.
func TestBuildViewMapsOneTurn(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("add a test")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
			map[string]any{"type": "thinking", "thinking": "the test file is next to it"},
			text("let me read it"),
			map[string]any{"type": "toolCall", "id": "call_1", "name": "read", "arguments": map[string]any{"path": "src/a.test.ts"}},
		), nil),
	)

	got := transcript.BuildView(v)
	if len(got) != 2 {
		t.Fatalf("%d entries, want 2: %+v", len(got), got)
	}
	if !got[0].User || got[0].ID != "1" {
		t.Errorf("the first entry is %+v, want the user's own", got[0])
	}
	if len(got[0].Blocks) != 1 || got[0].Blocks[0].Kind != transcript.Text || got[0].Blocks[0].Text != "add a test" {
		t.Errorf("the user's blocks are %+v", got[0].Blocks)
	}
	if got[0].CreatedAt != 1000 {
		t.Errorf("the user entry's time is %v, want 1000", got[0].CreatedAt)
	}

	reply := got[1]
	if reply.User || reply.ID != "2" {
		t.Errorf("the second entry is %+v, want the answer", reply)
	}
	var kinds []transcript.BlockKind
	for _, b := range reply.Blocks {
		kinds = append(kinds, b.Kind)
	}
	want := []transcript.BlockKind{transcript.Thinking, transcript.Text, transcript.Tools}
	if len(kinds) != len(want) {
		t.Fatalf("blocks %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("blocks %v, want %v", kinds, want)
		}
	}
	call := reply.Blocks[2].Calls[0]
	if call.Use.ID != "call_1" || call.Use.Name != "read" {
		t.Errorf("the call is %+v", call.Use)
	}
	if call.Input["path"] != "src/a.test.ts" {
		t.Errorf("the call's input is %+v", call.Input)
	}
	if string(call.RawInput) != `{"path":"src/a.test.ts"}` {
		t.Errorf("the call's raw input is %s", call.RawInput)
	}
	if len(call.Keys) != 1 || call.Keys[0] != "path" {
		t.Errorf("the call's keys are %v, want [path] in the model's order", call.Keys)
	}
	if call.ResultView != nil {
		t.Errorf("the call has a result before its tool-result entry: %+v", call.ResultView)
	}
}

// TestBuildViewFoldsConsecutiveAnswers: Pi writes one entry per model message, and one answer can span
// several (a tool round hands over to a new generation). The window shows a turn as one entry, so the
// answer's blocks join up and the entry's time is the last message's.
func TestBuildViewFoldsConsecutiveAnswers(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
			text("first"), map[string]any{"type": "toolCall", "id": "c1", "name": "read", "arguments": map[string]any{}}), nil),
		entry(t, 3, kernel.EntryAssistant, assistantMessage(t, 3000, "stop",
			text("second"), map[string]any{"type": "toolCall", "id": "c2", "name": "bash", "arguments": map[string]any{}}), nil),
	)

	got := transcript.BuildView(v)
	if len(got) != 2 {
		t.Fatalf("%d entries, want 2 (the answer folds): %+v", len(got), got)
	}
	reply := got[1]
	if reply.ID != "2" {
		t.Errorf("the folded entry's id is %q, want the first answer entry's", reply.ID)
	}
	if reply.CreatedAt != 3000 {
		t.Errorf("the folded entry's time is %v, want the last message's 3000", reply.CreatedAt)
	}
	// The two messages' blocks join in order: text, its cluster, text, its cluster. Adjacent clusters would
	// have joined into one — the window draws a run of calls as one cluster — but a text between them keeps
	// them apart.
	if len(reply.Blocks) != 4 {
		t.Fatalf("blocks %+v, want 4", reply.Blocks)
	}
	// Two adjacent tool clusters join into one, as the window draws them as one cluster.
	if reply.Blocks[0].Kind != transcript.Text || reply.Blocks[0].Text != "first" {
		t.Errorf("block 0 is %+v", reply.Blocks[0])
	}
	if reply.Blocks[1].Kind != transcript.Tools || len(reply.Blocks[1].Calls) != 1 || reply.Blocks[1].Calls[0].Use.ID != "c1" {
		t.Errorf("block 1 is %+v, want c1's cluster", reply.Blocks[1])
	}
	if reply.Blocks[2].Kind != transcript.Text || reply.Blocks[2].Text != "second" {
		t.Errorf("block 2 is %+v", reply.Blocks[2])
	}
	if reply.Blocks[3].Kind != transcript.Tools || len(reply.Blocks[3].Calls) != 1 || reply.Blocks[3].Calls[0].Use.ID != "c2" {
		t.Errorf("block 3 is %+v, want c2's cluster", reply.Blocks[3])
	}
}

// TestBuildViewAttachesALaterToolResult: Pi writes a tool's result as its own entry, after the answer that
// asked for it. The call keeps the result, in the text and pictures the row renders, and an error is marked.
func TestBuildViewAttachesALaterToolResult(t *testing.T) {
	image := map[string]any{"type": "image", "data": "aGk=", "mimeType": "image/png"}
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("look")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c1", "name": "read", "arguments": map[string]any{"path": "a.png"}}), nil),
		entry(t, 3, kernel.EntryToolResult, toolResultMessage(t, 3000, "c1", false, text("one"), image), map[string]any{"diagnostics": []any{}}),
		entry(t, 4, kernel.EntryAssistant, assistantMessage(t, 4000, "stop", text("it is a cat")), nil),
	)

	got := transcript.BuildView(v)
	if len(got) != 2 {
		t.Fatalf("%d entries, want the tool round folded into one answer: %+v", len(got), got)
	}
	reply := got[1]
	if len(reply.Blocks) != 2 || reply.Blocks[0].Kind != transcript.Tools {
		t.Fatalf("blocks %+v, want the cluster then the text", reply.Blocks)
	}
	call := reply.Blocks[0].Calls[0]
	if call.ResultView == nil {
		t.Fatal("the call kept no result")
	}
	if call.ResultView.Pending {
		t.Error("the result is marked pending")
	}
	if call.ResultView.Text != "one" {
		t.Errorf("the result's text is %q, want %q", call.ResultView.Text, "one")
	}
	if len(call.ResultView.Images) != 1 || call.ResultView.Images[0].MediaType != "image/png" || call.ResultView.Images[0].Data != "aGk=" {
		t.Errorf("the result's pictures are %+v", call.ResultView.Images)
	}
	// ReadResult is what the rows call: it must render the new path's result without knowing which path made it.
	shown := transcript.ReadResult(call)
	if shown.Text != "one" || shown.Pending || shown.IsError {
		t.Errorf("ReadResult reads %+v", shown)
	}

	// The same call with an error result.
	failed := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("look")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c2", "name": "bash", "arguments": map[string]any{}}), nil),
		entry(t, 3, kernel.EntryToolResult, toolResultMessage(t, 3000, "c2", true, text("command not found")), nil),
	)
	got = transcript.BuildView(failed)
	call = got[1].Blocks[0].Calls[0]
	if call.ResultView == nil || !call.ResultView.IsError {
		t.Fatalf("the failing result reads %+v", call.ResultView)
	}
}

// TestBuildViewKeepsAWindowInOrder: a forty-entry window comes out in the order the conversation happened,
// one entry per turn, with the ids the rows key on.
func TestBuildViewKeepsAWindowInOrder(t *testing.T) {
	var entries []kernel.Entry
	var id int64
	for i := 0; i < 20; i++ {
		id++
		entries = append(entries, entry(t, id, kernel.EntryUser, userMessage(t, float64(1000+i*2), text(fmt.Sprintf("question %d", i))), nil))
		id++
		entries = append(entries, entry(t, id, kernel.EntryAssistant, assistantMessage(t, float64(1001+i*2), "stop", text(fmt.Sprintf("answer %d", i))), nil))
	}

	got := transcript.BuildView(viewOf(entries...))
	if len(got) != 40 {
		t.Fatalf("%d entries, want 40", len(got))
	}
	for i, e := range got {
		wantID := fmt.Sprintf("%d", i+1)
		if e.ID != wantID {
			t.Fatalf("entry %d has id %q, want %q", i, e.ID, wantID)
		}
		wantUser := i%2 == 0
		if e.User != wantUser {
			t.Fatalf("entry %d is user=%v, want %v", i, e.User, wantUser)
		}
		// The whole row, not its first block: an extra block, a wrong kind or a moved time is a change the
		// window shows and this test must catch.
		want := fmt.Sprintf("answer %d", i/2)
		wantAt := float64(1001 + (i/2)*2)
		if wantUser {
			want, wantAt = fmt.Sprintf("question %d", i/2), float64(1000+(i/2)*2)
		}
		if len(e.Blocks) != 1 {
			t.Fatalf("entry %d has %d blocks, want 1: %+v", i, len(e.Blocks), e.Blocks)
		}
		block := e.Blocks[0]
		if block.Kind != transcript.Text || block.Text != want {
			t.Fatalf("entry %d's block is %+v, want a text block of %q", i, block, want)
		}
		if block.ID != fmt.Sprintf("%d:0:0", i+1) {
			t.Errorf("entry %d's block id is %q", i, block.ID)
		}
		if e.CreatedAt != wantAt {
			t.Fatalf("entry %d's time is %v, want %v", i, e.CreatedAt, wantAt)
		}
		if e.IsError {
			t.Fatalf("entry %d is marked failed", i)
		}
	}
}

// TestBuildViewSkipsBookkeeping: the Kernel's own entries and Pi's positional system entries are not
// conversation. A window full of them draws nothing, and one that also holds a turn draws only the turn.
func TestBuildViewSkipsBookkeeping(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntrySystem, []map[string]any{{"role": "system", "content": "", "timestamp": 1}}, nil),
		entry(t, 2, "droi.meta", nil, map[string]any{"createdAt": 1}),
		entry(t, 3, "droi.request", nil, map[string]any{"requestId": "r1", "fingerprint": "f", "submissionId": 9}),
		entry(t, 4, "droi.import", nil, map[string]any{"key": "k"}),
		entry(t, 5, kernel.EntryUser, userMessage(t, 1000, text("hello")), nil),
	)
	got := transcript.BuildView(v)
	if len(got) != 1 {
		t.Fatalf("%d entries, want only the turn: %+v", len(got), got)
	}
	if got[0].ID != "5" || got[0].Blocks[0].Text != "hello" {
		t.Errorf("the one entry is %+v", got[0])
	}

	if none := transcript.BuildView(viewOf(v.Entries[:4]...)); len(none) != 0 {
		t.Fatalf("bookkeeping drew %d entries", len(none))
	}
}

// TestBuildViewMarksAFailedAnswer: an answer that ended in an error is marked, which the row draws in the
// destructive color.
func TestBuildViewMarksAFailedAnswer(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil),
		entry(t, 2, kernel.EntryAssistant, []map[string]any{{
			"role": "assistant", "content": []map[string]any{text("the provider refused")},
			"timestamp": 2000, "stopReason": "error", "errorMessage": "rate limited",
		}}, nil),
	)
	got := transcript.BuildView(v)
	if len(got) != 2 {
		t.Fatalf("%d entries, want 2", len(got))
	}
	if !got[1].IsError {
		t.Errorf("the failed answer is not marked: %+v", got[1])
	}
}

// TestBuildViewShowsAPictureTheUserSent: an attached picture is a block of its own, with the media type and
// base64 data the row decodes.
func TestBuildViewShowsAPictureTheUserSent(t *testing.T) {
	v := viewOf(entry(t, 1, kernel.EntryUser, userMessage(t, 1000,
		map[string]any{"type": "image", "data": "aGk=", "mimeType": "image/jpeg"},
		text("what is this?"),
	), nil))
	got := transcript.BuildView(v)
	if len(got) != 1 || len(got[0].Blocks) != 2 {
		t.Fatalf("the user's entry is %+v", got)
	}
	pic := got[0].Blocks[0]
	if pic.Kind != transcript.Picture || pic.Image.MediaType != "image/jpeg" || pic.Image.Data != "aGk=" {
		t.Errorf("the picture is %+v", pic)
	}
	if got[0].Blocks[1].Kind != transcript.Text || got[0].Blocks[1].Text != "what is this?" {
		t.Errorf("the text after the picture is %+v", got[0].Blocks[1])
	}
}

// TestBuildViewDoesNotReadToolText: the old transcript recognized a background command by the sentence droid
// wrote into its result. Nothing in the Kernel's path does that: the same text is a result's text and nothing
// more, and no job or nested row appears from it.
func TestBuildViewDoesNotReadToolText(t *testing.T) {
	droidText := "Background process started (PID: 48213)\nCommand: npm run dev\nOutput: /tmp/out"
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("run it")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c1", "name": "bash", "arguments": map[string]any{"command": "npm run dev"}}), nil),
		entry(t, 3, kernel.EntryToolResult, toolResultMessage(t, 3000, "c1", false, text(droidText)), nil),
	)

	got := transcript.BuildView(v)
	call := got[1].Blocks[0].Calls[0]
	if call.ResultView == nil || call.ResultView.Text != droidText {
		t.Fatalf("the result is %+v, want the text as it came", call.ResultView)
	}
	if len(call.Nested) != 0 {
		t.Fatalf("a result with droid's text made %d nested calls", len(call.Nested))
	}
}

// The tests below answer review round 1 of this file's PR (shared/reviews/pr240-241-round1.md): two shapes the
// real Kernel produces that the first mapping dropped, a cluster that never stopped saying it was running, block
// identities that collided across one entry's messages, and a boundary the answers must not fold across.

// TestBuildViewKeepsAUsersPlainString: a pi.user entry's message holds its content as a string when the Host
// submitted `input:"hello"` — that is the ordinary shape, not an edge case — and it must draw the same row a list
// of text blocks does. Dropping it also broke the folding: the answer would have opened the turn on its own.
func TestBuildViewKeepsAUsersPlainString(t *testing.T) {
	v := viewOf(entry(t, 1, kernel.EntryUser,
		[]map[string]any{{"role": "user", "content": "hello", "timestamp": 1234}}, nil))

	got := transcript.BuildView(v)
	if len(got) != 1 || !got[0].User {
		t.Fatalf("a plain-string user message disappeared: %+v", got)
	}
	if len(got[0].Blocks) != 1 || got[0].Blocks[0].Kind != transcript.Text || got[0].Blocks[0].Text != "hello" {
		t.Fatalf("its blocks are %+v, want one text block", got[0].Blocks)
	}
	if got[0].CreatedAt != 1234 {
		t.Errorf("its time is %v, want the message's 1234", got[0].CreatedAt)
	}
}

// TestBuildViewShowsAFailedAnswerThatSaidNothing: a provider that refuses the request ends the answer with
// `content: []`, `stopReason: "error"` and `errorMessage`. There is no block to draw and there is a great deal to
// say: skipping the answer left the window showing the question and nothing else, as if the turn never happened.
func TestBuildViewShowsAFailedAnswerThatSaidNothing(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil),
		entry(t, 2, kernel.EntryAssistant, []map[string]any{{
			"role": "assistant", "content": []any{}, "timestamp": 2000,
			"stopReason": "error", "errorMessage": "401 invalid API key",
		}}, nil),
	)

	got := transcript.BuildView(v)
	if len(got) != 2 {
		t.Fatalf("%d rows, want the failed answer as one: %+v", len(got), got)
	}
	if !got[1].IsError {
		t.Error("the failed answer is not marked")
	}
	if len(got[1].Blocks) != 1 || got[1].Blocks[0].Kind != transcript.Text ||
		!strings.Contains(got[1].Blocks[0].Text, "401 invalid API key") {
		t.Fatalf("the failure reads %+v, want a text block carrying the provider's sentence", got[1].Blocks)
	}
	if got[1].CreatedAt != 2000 {
		t.Errorf("the failed answer's time is %v, want 2000", got[1].CreatedAt)
	}
}

// TestBuildViewShowsAStoppedAnswerThatSaidNothing: cancelling a run can also leave the answer with no content —
// a request that never streamed. The row says the turn stopped, and it is not an error: the user asked for it.
func TestBuildViewShowsAStoppedAnswerThatSaidNothing(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("go")), nil),
		entry(t, 2, kernel.EntryAssistant, []map[string]any{{
			"role": "assistant", "content": []any{}, "timestamp": 2000, "stopReason": "aborted",
		}}, nil),
	)

	got := transcript.BuildView(v)
	if len(got) != 2 {
		t.Fatalf("%d rows, want the stopped answer as one: %+v", len(got), got)
	}
	if got[1].IsError {
		t.Error("a cancelled run is drawn as a failure")
	}
	if len(got[1].Blocks) != 1 || got[1].Blocks[0].Kind != transcript.Text || got[1].Blocks[0].Text == "" {
		t.Fatalf("the stopped answer reads %+v, want a sentence", got[1].Blocks)
	}
}

// TestTheClusterOfFinishedCallsIsNotRunning: the cluster's header reads the calls' results, and the Kernel's
// calls carry theirs in ResultView — Pi writes a result as its own entry. A cluster whose results arrived said
// "Running read" for ever, which is a window that never stops working.
func TestTheClusterOfFinishedCallsIsNotRunning(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 500, text("read it")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 1000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c1", "name": "read", "arguments": map[string]any{"path": "a.ts"}}), nil),
		entry(t, 3, kernel.EntryToolResult, toolResultMessage(t, 2000, "c1", false, text("file body")), nil),
	)

	calls := transcript.BuildView(v)[1].Blocks[0].Calls
	shown := transcript.ReadResult(calls[0])
	if shown.Pending {
		t.Fatal("the result itself reads as pending although it arrived")
	}
	label, pending := transcript.ClusterLabel(calls)
	if pending {
		t.Fatalf("a finished call still reads as running: %q", label)
	}
	if !strings.Contains(label, "read") {
		t.Errorf("the cluster's label is %q, want the tool's name", label)
	}

	// And the other half, so the fix is not "never pending": a call whose result never came keeps saying so.
	open := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 500, text("read it")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 1000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c2", "name": "read", "arguments": map[string]any{}}), nil),
	)
	waiting := transcript.BuildView(open)[1].Blocks[0].Calls
	if label, pending := transcript.ClusterLabel(waiting); !pending {
		t.Fatalf("a call with no result reads as finished: %q", label)
	}
}

// TestBuildViewGivesEachMessageItsOwnBlockIDs: an entry may hold several messages, and the rows key a picture's
// decoded bitmap by its block ID. Numbering each message from zero gave two pictures in one entry the same ID,
// and the second one drew the first one's pixels.
func TestBuildViewGivesEachMessageItsOwnBlockIDs(t *testing.T) {
	first := userMessage(t, 1000, map[string]any{"type": "image", "data": "YQ==", "mimeType": "image/png"})
	second := userMessage(t, 2000, map[string]any{"type": "image", "data": "Yg==", "mimeType": "image/png"})

	got := transcript.BuildView(viewOf(entry(t, 1, kernel.EntryUser, append(first, second...), nil)))
	if len(got) != 1 || len(got[0].Blocks) != 2 {
		t.Fatalf("%+v, want one row of two pictures", got)
	}
	if got[0].Blocks[0].ID == got[0].Blocks[1].ID {
		t.Fatalf("two pictures share the identity %q", got[0].Blocks[0].ID)
	}
}

// TestBuildViewStartsANewRowAfterTheContextMoves: a reset and a compaction are the context's own boundaries — the
// summary Pi writes is a message, and the answer after it belongs to a new context. Folding across either joined
// two answers into one row, which the plan's fold rule 3 forbids.
func TestBuildViewStartsANewRowAfterTheContextMoves(t *testing.T) {
	for _, kind := range []string{kernel.EntryReset, kernel.EntryCompaction} {
		t.Run(kind, func(t *testing.T) {
			v := viewOf(
				entry(t, 1, kernel.EntryAssistant, assistantMessage(t, 1000, "stop", text("before")), nil),
				entry(t, 2, kind, userMessage(t, 2000, text("summary or handoff")), map[string]any{"reason": "manual"}),
				entry(t, 3, kernel.EntryAssistant, assistantMessage(t, 3000, "stop", text("after")), nil),
			)
			got := transcript.BuildView(v)
			// A compaction is a note of its own between the two, which says the context moved; a reset draws none.
			if kind == kernel.EntryCompaction {
				if len(got) != 3 || !got[1].Note || !strings.Contains(got[1].Blocks[0].Text, "Context compacted") {
					t.Fatalf("%d rows, no compaction note between them: %+v", len(got), got)
				}
				got = []*transcript.Entry{got[0], got[2]}
			}
			if len(got) != 2 {
				t.Fatalf("%d rows: the answers folded across a %s", len(got), kind)
			}
			if got[0].Blocks[0].Text != "before" || got[1].Blocks[0].Text != "after" {
				t.Fatalf("the rows read %+v / %+v", got[0].Blocks, got[1].Blocks)
			}
		})
	}
}

// TestBuildViewShowsADroiNote: a note is something to read, written where it happened. It carries no messages, so
// nothing else in this mapping would draw it.
func TestBuildViewShowsADroiNote(t *testing.T) {
	got := transcript.BuildView(viewOf(entry(t, 1, kernel.EntryNote, nil, map[string]any{"text": "conversation note"})))
	if len(got) != 1 || len(got[0].Blocks) != 1 || got[0].Blocks[0].Text != "conversation note" {
		t.Fatalf("the note reads %+v", got)
	}
}

// TestAnInterruptedCallIsNotStillRunning: a turn the user stopped leaves its calls without results — they never
// ran to an answer. The row must not promise one, and must not claim a failure either.
func TestAnInterruptedCallIsNotStillRunning(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 500, text("read three files")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 1000, "aborted",
			map[string]any{"type": "toolCall", "id": "c1", "name": "read", "arguments": map[string]any{"path": "a"}}), nil),
	)
	entries := transcript.BuildView(v)
	if len(entries) != 2 {
		t.Fatalf("%d rows: %+v", len(entries), entries)
	}
	if entries[1].IsError {
		t.Error("a stopped turn is drawn as a failure")
	}
	call := entries[1].Blocks[0].Calls[0]
	shown := transcript.ReadResult(call)
	if shown.Pending {
		t.Error("the call still reads as running after its turn was stopped")
	}
	if !shown.Interrupted {
		t.Error("the call does not read as interrupted")
	}
	if shown.IsError {
		t.Error("an interrupted call reads as failed")
	}
	if label, pending := transcript.ClusterLabel([]*transcript.ToolCall{call}); pending {
		t.Errorf("the cluster says %q after the turn was stopped", label)
	}
}

// The three shapes review round 2 of this file's PR found in the wild. Each is something a user does every day:
// pressing Stop while a command runs, reading a note, and cancelling a turn.

// abortedResult is a tool result the harness wrote when a run was stopped: isError, and a diagnostics entry whose
// code says why. That is what the Kernel really stores — the result-less shape TestAnInterruptedCallIsNotStillRunning
// covers is the rarer one.
func abortedResult(t *testing.T, at float64, callID, tool string) kernel.Entry {
	t.Helper()
	return entry(t, 99, kernel.EntryToolResult, []map[string]any{{
		"role": "toolResult", "toolCallId": callID, "toolName": tool,
		"content": []map[string]any{{"type": "text", "text": "<harness>\n[error] Tool " + tool + " was aborted\n</harness>"}},
		"isError": true, "timestamp": at,
	}}, map[string]any{"diagnostics": []map[string]any{{"severity": "error", "code": "aborted",
		"message": "Tool " + tool + " was aborted"}}})
}

// TestAStoppedToolResultReadsAsInterrupted: pressing Stop while `bash sleep 60` runs leaves exactly this: an
// isError result whose diagnostics say "aborted". A window that drew it as a failure in red tells the user their
// command broke, when they were the one who stopped it.
func TestAStoppedToolResultReadsAsInterrupted(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 500, text("run the tests")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 1000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c1", "name": "bash", "arguments": map[string]any{"command": "sleep 60"}}), nil),
		abortedResult(t, 2000, "c1", "bash"),
	)

	entries := transcript.BuildView(v)
	if len(entries) != 2 {
		t.Fatalf("%d rows: %+v", len(entries), entries)
	}
	call := entries[1].Blocks[0].Calls[0]
	shown := transcript.ReadResult(call)
	if shown.IsError {
		t.Error("a result the user stopped reads as a failure")
	}
	if !shown.Interrupted {
		t.Error("a result the user stopped does not read as interrupted")
	}
	if shown.Pending {
		t.Error("a stopped result reads as still running")
	}
	if label, pending := toolClusterLabel(call); pending {
		t.Errorf("the cluster says %q after the user stopped it", label)
	}
}

// TestAStoppedToolResultHidesTheHarnessWrapper: the text of a stopped result is the harness talking to the model —
// `<harness>` tags and an `[error]` prefix. Expanded, the row showed it to the user.
func TestAStoppedToolResultHidesTheHarnessWrapper(t *testing.T) {
	v := viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 500, text("run the tests")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 1000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c1", "name": "bash", "arguments": map[string]any{}}), nil),
		abortedResult(t, 2000, "c1", "bash"),
	)

	shown := transcript.ReadResult(transcript.BuildView(v)[1].Blocks[0].Calls[0])
	all := shown.Text + "\n" + shown.Notes
	if strings.Contains(all, "<harness>") || strings.Contains(all, "</harness>") {
		t.Errorf("the harness wrapper is still in the row: %q", all)
	}
	if strings.Contains(all, "[error]") {
		t.Errorf("the harness's severity prefix is still in the row: %q", all)
	}
	if shown.Notes != "Tool bash was aborted" {
		t.Errorf("the sentence is gone: %q", shown.Notes)
	}
}

// TestANoteIsNotTheEndOfATurn: a note left after a finished turn became that turn's closing entry — with time
// zero, so the answer lost its end row — and Copy took the note with the reply.
func TestANoteIsNotTheEndOfATurn(t *testing.T) {
	entries := transcript.BuildView(viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("do it")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "stop", text("done")), nil),
		entry(t, 3, kernel.EntryNote, nil, map[string]any{"text": "a note about the turn", "at": 3000}),
	))
	if len(entries) != 3 {
		t.Fatalf("%d rows: %+v", len(entries), entries)
	}
	if entries[2].CreatedAt != 3000 {
		t.Errorf("the note's time is %v, want its data's 3000", entries[2].CreatedAt)
	}

	ends := transcript.TurnEnds(entries, false)
	if _, ok := ends[entries[2].ID]; ok {
		t.Errorf("the note closed a turn: %+v", ends)
	}
	if _, ok := ends[entries[1].ID]; !ok {
		t.Errorf("the answer is not a turn's end: %+v", ends)
	}
	if end := ends[entries[1].ID]; end.EndedAt != 2000 {
		t.Errorf("the answer's end is %v, want its own time", end.EndedAt)
	}

	if got := transcript.ReplyText(entries); strings.Contains(got, "a note about the turn") {
		t.Errorf("Copy took the note with the reply: %q", got)
	} else if !strings.Contains(got, "done") {
		t.Errorf("Copy lost the reply: %q", got)
	}
}

// TestAnAnswerAfterANoteIsItsOwnRow: a note is not something to answer, so the answer after it opens a row rather
// than joining the note's.
func TestAnAnswerAfterANoteIsItsOwnRow(t *testing.T) {
	entries := transcript.BuildView(viewOf(
		entry(t, 1, kernel.EntryNote, nil, map[string]any{"text": "the host noted something", "at": 1000}),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "stop", text("an answer")), nil),
	))
	if len(entries) != 2 {
		t.Fatalf("%d rows, want the note and the answer: %+v", len(entries), entries)
	}
	if entries[1].Blocks[0].Text != "an answer" {
		t.Errorf("the second row is %+v", entries[1].Blocks)
	}
}

// TestAnEmptyUserEntryStartsANewRow: a handoff or a write submission is written as a user entry with nothing to
// show, and it is skipped. Without a boundary there, the answer after it joined the turn before — two turns in
// one row, with the second's time.
func TestAnEmptyUserEntryStartsANewRow(t *testing.T) {
	entries := transcript.BuildView(viewOf(
		entry(t, 1, kernel.EntryUser, userMessage(t, 1000, text("first")), nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "stop", text("first answer")), nil),
		entry(t, 3, kernel.EntryUser, userMessage(t, 3000), nil),
		entry(t, 4, kernel.EntryAssistant, assistantMessage(t, 4000, "stop", text("second answer")), nil),
	))
	if len(entries) != 3 {
		t.Fatalf("%d rows, want the two turns and the second answer: %+v", len(entries), entries)
	}
	if entries[2].Blocks[0].Text != "second answer" || entries[2].CreatedAt != 4000 {
		t.Errorf("the answer after the empty user entry reads %+v", entries[2])
	}
}

// toolClusterLabel is ClusterLabel over one call, which is what a row's header asks.
func toolClusterLabel(c *transcript.ToolCall) (string, bool) {
	return transcript.ClusterLabel([]*transcript.ToolCall{c})
}
