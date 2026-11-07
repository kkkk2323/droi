package fakekernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// DefaultWindow is a view window wide enough for a test's transcript.
var DefaultWindow = kernel.Window{Min: 20, Max: 200}

// pageSize is how many entries one RPC asks for; a test transcript is far longer than one page.
const pageSize = 200

// Submission is one submitted turn. Its boundaries come from the Kernel, not from the transcript.
//
// A wait must not be "the newest assistant entry": an entry carries the *task* that wrote it, and one task can
// answer a queued input together with the one after it. `submission.get` gives the two entries that belong to
// this submission — the user's own (`Entry`) and the reply that answered it (`Answer`) — which is why every
// wait here takes a Submission instead of a conversation id.
type Submission struct {
	Conversation kernel.ID
	ID           kernel.ID
}

// Record is where one submission is: the Kernel's answer to `submission.get`.
//
// It carries no end for a turn that answered nothing: Pi keeps no link from a submission to the run task that
// carried it, so the Kernel reports only what Pi stores and a client reads the status (see
// shared/interfaces/fake-kernel.md).
type Record struct {
	Status string
	// Entry is the user's own entry; absent while the submission is still queued.
	Entry kernel.ID
	// Answer is the entry that answered it; present once Status is "done".
	Answer kernel.ID
	Reason string
}

// CreateConversation makes a conversation on the fake model's agent and opens it, so the connection gets
// conversation.view pushes for it. Its working directory is the Kernel's own workspace — never the source
// tree, and never the user's home.
func (k *Kernel) CreateConversation(t testing.TB, c *kernel.Client) kernel.ID {
	t.Helper()
	return k.CreateConversationIn(t, c, k.Workspace)
}

// CreateConversationIn is CreateConversation with a working directory, for a test whose tools must run in a
// place of their own.
func (k *Kernel) CreateConversationIn(t testing.TB, c *kernel.Client, dir string) kernel.ID {
	t.Helper()
	ctx, cancel := k.context(t, 60*time.Second)
	defer cancel()
	if dir == "" {
		dir = k.Workspace
	}
	created, err := c.Create(ctx, kernel.CreateParams{
		Agent: &kernel.AgentChange{Model: &kernel.ModelRef{Provider: ProviderID, ModelID: ModelID}},
		Kind:  "chat",
		Cwd:   dir,
	})
	if err != nil {
		t.Fatalf("fakekernel: conversation.create: %v\n%s", err, k.Logs())
	}
	if _, err := c.Open(ctx, kernel.OpenParams{ID: created.ConversationID, Window: &DefaultWindow}); err != nil {
		t.Fatalf("fakekernel: conversation.open: %v\n%s", err, k.Logs())
	}
	return created.ConversationID
}

// Submit sends one message and returns the turn to wait on.
func (k *Kernel) Submit(t testing.TB, c *kernel.Client, id kernel.ID, text string) Submission {
	t.Helper()
	ctx, cancel := k.context(t, 60*time.Second)
	defer cancel()
	// No WhenBusy: the Kernel's values are "steer", "followUp" and "reject", and the Go client's
	// WhenBusyQueue ("queue") is refused as invalid params — see shared/interfaces/fake-kernel.md.
	result, err := c.Submit(ctx, kernel.SubmitParams{ID: id, Input: text})
	if err != nil {
		t.Fatalf("fakekernel: conversation.submit %q: %v\n%s", text, err, k.Logs())
	}
	return Submission{Conversation: id, ID: result.SubmissionID}
}

// submissionRecord asks the Kernel where this submission is.
type submissionRecord struct {
	Status string          `json:"status"`
	Entry  json.RawMessage `json:"entry"`
	Answer json.RawMessage `json:"answer"`
	Reason string          `json:"reason"`
}

// Record asks the Kernel where this submission is: queued, placed, done, or unanswered.
func (k *Kernel) Record(t testing.TB, c *kernel.Client, sub Submission, within time.Duration) Record {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	return k.record(ctx, t, c, sub)
}

func (k *Kernel) record(ctx context.Context, t testing.TB, c *kernel.Client, sub Submission) Record {
	t.Helper()
	var answer submissionRecord
	if err := c.Call(ctx, "submission.get", map[string]any{
		"id": sub.Conversation, "submissionId": sub.ID,
	}, &answer); err != nil {
		t.Fatalf("fakekernel: submission.get %d: %v\n%s", sub.ID, err, k.Logs())
	}
	return Record{
		Status: answer.Status,
		Entry:  idOf(answer.Entry),
		Answer: idOf(answer.Answer),
		Reason: answer.Reason,
	}
}

// finishedTurn says whether a submission's turn is complete, or fails the test when it can never be.
//
// Every terminal submission is final: waiting longer cannot add an entry. An `unanswered` one — aborted, or
// ended without a reply — is reported with its reason at once, rather than spending the caller's timeout to
// say "deadline exceeded" about something the Kernel already knew.
func (k *Kernel) finishedTurn(t testing.TB, sub Submission, record Record) (done bool) {
	t.Helper()
	switch record.Status {
	case "queued", "placed":
		return false
	case "done":
		return true
	case "unanswered":
		t.Fatalf("fakekernel: submission %d was never answered (%s); its turn is not a reply",
			sub.ID, record.Reason)
		return true
	}
	// A status this package does not know: treat it as unfinished, so a new one does not silently pass tests.
	return false
}

// WaitForReply waits until this submission's own answer holds text, and returns the answer's text.
//
// The answer is the entry the Kernel says answered this submission, so a reply that was already in flight is
// never mistaken for it. A submission that ended without an answer fails the test at once, with the Kernel's
// reason, instead of spending the caller's timeout.
func (k *Kernel) WaitForReply(t testing.TB, c *kernel.Client, sub Submission, text string, within time.Duration) string {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	for {
		record := k.record(ctx, t, c, sub)
		if k.finishedTurn(t, sub, record) {
			// A `continue` has no entry of its own, even when it answered, so the answer is the whole bound:
			// reading from below it asks for one entry instead of the conversation.
			from := record.Entry - 1
			if record.Entry == 0 {
				from = record.Answer - 1
			}
			for _, entry := range k.entriesBetween(ctx, t, c, sub.Conversation, from, record.Answer) {
				if entry.ID != record.Answer || entry.Kind != "pi.assistant" {
					continue
				}
				if got := entry.Text; strings.Contains(got, text) {
					return got
				}
				t.Fatalf("fakekernel: submission %d was answered by %q, which does not hold %q",
					sub.ID, entry.Text, text)
			}
			t.Fatalf("fakekernel: submission %d answered entry %d, which is not in the transcript\n%s",
				sub.ID, record.Answer, k.Logs())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fakekernel: submission %d got no answer containing %q within %s: %v\n%s",
				sub.ID, text, within, ctx.Err(), k.Logs())
			return ""
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// WaitForEntries waits until this submission's turn holds an entry satisfying match, and returns the turn.
//
// The bounds come from the submission's own status, so a later turn's entries are never part of this one — and
// a turn that has settled without a match fails at once rather than waiting for entries that cannot arrive.
func (k *Kernel) WaitForEntries(
	t testing.TB, c *kernel.Client, sub Submission, match func([]Entry) bool, within time.Duration,
) []Entry {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	for {
		record := k.record(ctx, t, c, sub)
		// The status decides first. A submission that ended without an answer is terminal, and saying why is
		// more useful than any entry a predicate could find in it — a predicate matching the user's own entry
		// would otherwise report success for a turn that was cut short.
		done := k.finishedTurn(t, sub, record)
		entries := k.turnEntries(ctx, t, c, sub, record)
		if match(entries) {
			return entries
		}
		// A settled submission is final: a queued one that was withdrawn has no turn at all, and a done one
		// cannot grow. Waiting further would only turn a known answer into a timeout.
		if done {
			t.Fatalf("fakekernel: submission %d settled with %d entries, none matching: %+v\n%s",
				sub.ID, len(entries), entries, k.Logs())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fakekernel: submission %d never produced the expected entries: %v\n%s",
				sub.ID, ctx.Err(), k.Logs())
			return nil
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// turnEntries is the transcript of one submission, bounded by its status.
//
// The Kernel reports Pi's own fields — `entry`, `answer`, `status` — and nothing else, because Pi keeps no link
// from a submission to the run task that carried it. So the bounds are read off the status:
//
//   - queued: not placed yet, so it wrote no model entry; its turn is empty rather than "everything so far";
//   - placed: from its own entry to the newest entry of the conversation. One conversation runs one turn at a
//     time, and a later input either queues (it has no entry yet) or steers into this same run, which Pi
//     answers with this submission's own `done` record — so every entry after `entry` is this turn's, tool
//     results and partial answers included while it still runs;
//   - done: from its own entry to its own answer, both durable and both Pi's.
//
// Reading a turn is two RPCs, and the record and the transcript are separate snapshots: between them the run
// can end and a queued follow-up can be placed and answered. So the record is read again afterwards and the
// result is bounded by what it says now — an unanswered submission's turn stops existing, and a finished one
// stops at its answer. Without that, a wait reads the next turn's reply as if it were its own.
//
// Two submissions have no range at all, and are reported rather than guessed at: one that ended without an
// answer, whose entries nothing distinguishes from a later turn's; and a `continue`, which places no entry of
// its own, so no lower bound exists (its answer still bounds WaitForReply). Read the conversation for those.
func (k *Kernel) turnEntries(
	ctx context.Context, t testing.TB, c *kernel.Client, sub Submission, record Record,
) []Entry {
	t.Helper()
	switch record.Status {
	case "queued":
		return nil
	case "placed", "done":
		if record.Entry == 0 {
			t.Fatalf("fakekernel: submission %d has no entry of its own (%s), so its turn has no definite "+
				"range; a continue places none, and an input's own entry is what bounds it\n%s",
				sub.ID, record.Status, k.Logs())
			return nil
		}
		entries := k.readTurn(ctx, t, c, sub, record)
		return k.boundBySettledRecord(ctx, t, c, sub, entries)
	case "unanswered":
		t.Fatalf("fakekernel: submission %d was never answered (%s), so it has no definite range; "+
			"read the conversation's entries instead\n%s", sub.ID, record.Reason, k.Logs())
		return nil
	}
	t.Fatalf("fakekernel: submission %d has an unknown status %q\n%s", sub.ID, record.Status, k.Logs())
	return nil
}

// readTurn reads the entries a placed or done submission covers, with the Kernel's own bookkeeping filtered out
// unless it belongs to this submission.
func (k *Kernel) readTurn(
	ctx context.Context, t testing.TB, c *kernel.Client, sub Submission, record Record,
) []Entry {
	t.Helper()
	to := kernel.ID(0)
	if record.Status == "done" {
		to = record.Answer
	}
	var entries []Entry
	for _, entry := range k.entriesBetween(ctx, t, c, sub.Conversation, record.Entry-1, to) {
		if !belongsToTurn(entry, sub) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// boundBySettledRecord re-reads the record after the transcript and trims what the turn no longer owns.
//
// The two reads are separate snapshots, so the run may have ended — and a queued follow-up may have been
// answered — while the entries were being paged. What the record says now decides: unanswered has no range at
// all, and done ends at its answer.
func (k *Kernel) boundBySettledRecord(
	ctx context.Context, t testing.TB, c *kernel.Client, sub Submission, entries []Entry,
) []Entry {
	t.Helper()
	now := k.record(ctx, t, c, sub)
	switch now.Status {
	case "unanswered":
		t.Fatalf("fakekernel: submission %d was never answered (%s), so it has no definite range; "+
			"read the conversation's entries instead\n%s", sub.ID, now.Reason, k.Logs())
		return nil
	case "done":
		if now.Answer == 0 {
			t.Fatalf("fakekernel: submission %d is done without an answer\n%s", sub.ID, k.Logs())
			return nil
		}
		trimmed := entries[:0:0]
		for _, entry := range entries {
			if entry.ID <= now.Answer {
				trimmed = append(trimmed, entry)
			}
		}
		return trimmed
	}
	return entries
}

// belongsToTurn reports whether one entry is part of `sub`'s turn.
//
// Model-visible entries are: one conversation runs one turn at a time, so everything between a turn's own entry
// and its answer was written by that turn. The Kernel's own `droi.*` bookkeeping is not model-visible and can be
// written while another submission is merely queued — a request memo for a submission that is not even placed.
// So bookkeeping belongs to a turn only when it names that submission; one that names none is left out rather
// than attributed to whichever turn happens to be running.
func belongsToTurn(entry Entry, sub Submission) bool {
	if !strings.HasPrefix(entry.Kind, "droi.") {
		return true
	}
	var data struct {
		SubmissionID *kernel.ID `json:"submissionId"`
	}
	if len(entry.Data) == 0 || json.Unmarshal(entry.Data, &data) != nil || data.SubmissionID == nil {
		return false
	}
	return *data.SubmissionID == sub.ID
}

// entriesBetween reads the conversation's entries above `from` and at or below `to` (0: no upper bound),
// oldest first, page by page.
//
// Ascending and paginated on purpose: the RPC's default order is newest first, which reads a turn backwards,
// and one page silently truncates a long one.
func (k *Kernel) entriesBetween(
	ctx context.Context, t testing.TB, c *kernel.Client, id kernel.ID, from, to kernel.ID,
) []Entry {
	t.Helper()
	var entries []Entry
	var cursor json.RawMessage
	for {
		params := map[string]any{
			"id":    id,
			"query": map[string]any{"order": "ascending", "minEntryId": from + 1},
			"limit": pageSize,
		}
		if len(cursor) > 0 {
			params["cursor"] = cursor
		}
		var page struct {
			Items  []kernel.Entry  `json:"items"`
			Cursor json.RawMessage `json:"cursor"`
		}
		if err := c.Call(ctx, "conversation.entries", params, &page); err != nil {
			t.Fatalf("fakekernel: conversation.entries: %v\n%s", err, k.Logs())
		}
		for _, entry := range page.Items {
			if entry.ID <= from {
				continue
			}
			if to != 0 && entry.ID > to {
				return entries
			}
			entries = append(entries, Entry{
				ID: entry.ID, Kind: entry.Kind, Text: entryText(entry), Raw: entry.Model, Data: entry.Data,
			})
		}
		if len(page.Cursor) == 0 || string(page.Cursor) == "null" || len(page.Items) == 0 {
			return entries
		}
		cursor = page.Cursor
	}
}

// Ask submits and waits for the reply in one call, which is what most tests want.
func (k *Kernel) Ask(t testing.TB, c *kernel.Client, id kernel.ID, prompt, reply string, within time.Duration) string {
	t.Helper()
	return k.WaitForReply(t, c, k.Submit(t, c, id, prompt), reply, within)
}

// Entry is one transcript entry, as a test reads it.
type Entry struct {
	ID   kernel.ID
	Kind string
	// Text is the entry's own text: an assistant message's text, or a tool result's content.
	Text string
	// Raw is the entry's model JSON, for a test that needs a part this does not decode.
	Raw json.RawMessage
	// Data is the entry's own payload, which the Kernel's bookkeeping entries carry instead of a model.
	Data json.RawMessage
}

// EntriesAfter is the entries this submission's turn wrote, oldest first.
func (k *Kernel) EntriesAfter(t testing.TB, c *kernel.Client, sub Submission, within time.Duration) []Entry {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	return k.turnEntries(ctx, t, c, sub, k.record(ctx, t, c, sub))
}

// ViewPush is one conversation.view notification, decoded.
type ViewPush struct {
	ID    kernel.ID
	View  *kernel.View
	Whole bool
}

// WaitForView waits for a conversation.view push that satisfies match, bounded by the test's deadline as well
// as by within.
func (k *Kernel) WaitForView(
	t testing.TB, c *kernel.Client, match func(ViewPush) bool, within time.Duration,
) ViewPush {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	for {
		select {
		case note, ok := <-c.Notifications():
			if !ok {
				t.Fatalf("fakekernel: the connection closed while waiting for a view push: %v", c.Err())
			}
			if note.Method != "conversation.view" {
				continue
			}
			var view kernel.ViewNotification
			if err := note.Decode(&view); err != nil {
				t.Fatalf("fakekernel: decoding conversation.view: %v", err)
			}
			push := ViewPush{ID: view.ID, View: view.Value, Whole: view.Whole()}
			if match(push) {
				return push
			}
		case <-ctx.Done():
			t.Fatalf("fakekernel: no matching view push within %s: %v\n%s", within, ctx.Err(), k.Logs())
			return ViewPush{}
		}
	}
}

// idOf reads a JSON id, which the Kernel may leave out.
func idOf(raw json.RawMessage) kernel.ID {
	if len(raw) == 0 {
		return 0
	}
	var id kernel.ID
	if err := json.Unmarshal(raw, &id); err != nil {
		return 0
	}
	return id
}

// entryText is an entry's own text. Pi's model messages carry content parts: text, thinking, a tool call, or a
// tool result's blocks; this joins what a person would read.
func entryText(entry kernel.Entry) string {
	var messages []struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(entry.Model, &messages); err != nil {
		return ""
	}
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			parts = append(parts, text)
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &blocks) == nil {
			for _, block := range blocks {
				if block.Text != "" {
					parts = append(parts, block.Text)
				}
			}
		}
	}
	return strings.Join(parts, "")
}
