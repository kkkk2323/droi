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

// Record asks the Kernel where this submission is: queued, placed, done, or unanswered.
func (k *Kernel) Record(t testing.TB, c *kernel.Client, sub Submission, within time.Duration) Record {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	return k.record(ctx, t, c, sub)
}

func (k *Kernel) record(ctx context.Context, t testing.TB, c *kernel.Client, sub Submission) Record {
	t.Helper()
	var answer struct {
		Status string          `json:"status"`
		Entry  json.RawMessage `json:"entry"`
		Answer json.RawMessage `json:"answer"`
		Reason string          `json:"reason"`
	}
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

// WaitForReply waits until this submission's own answer holds text, and returns the answer's text.
//
// The answer is the entry the Kernel says answered this submission, so a reply that was already in flight is
// never mistaken for it — the false pass a "newest assistant entry" wait produces.
func (k *Kernel) WaitForReply(t testing.TB, c *kernel.Client, sub Submission, text string, within time.Duration) string {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	for {
		record := k.record(ctx, t, c, sub)
		if record.Status == "unanswered" {
			t.Fatalf("fakekernel: submission %d was never answered: %s\n%s", sub.ID, record.Reason, k.Logs())
		}
		if record.Status == "done" && record.Answer != 0 {
			for _, entry := range k.entriesFrom(ctx, t, c, sub.Conversation, record.Answer-1) {
				if entry.ID != record.Answer || entry.Kind != "pi.assistant" {
					continue
				}
				if got := entry.Text; strings.Contains(got, text) {
					return got
				}
				t.Fatalf("fakekernel: submission %d was answered by %q, which does not hold %q",
					sub.ID, entry.Text, text)
			}
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
}

// EntriesAfter is the entries this submission's turn wrote, oldest first.
//
// "This submission's" is the Kernel's answer, not a guess: the turn starts at the user's own entry and ends at
// the reply that answered it. A submission still queued has no turn yet, and answers the entries so far —
// nothing — rather than whatever the previous turn left in the transcript.
func (k *Kernel) EntriesAfter(t testing.TB, c *kernel.Client, sub Submission, within time.Duration) []Entry {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	record := k.record(ctx, t, c, sub)
	from := record.Entry - 1
	if from < 0 {
		from = 0
	}
	return k.entriesFrom(ctx, t, c, sub.Conversation, from)
}

// WaitForEntries waits until this submission's turn holds an entry satisfying match, and returns the turn.
func (k *Kernel) WaitForEntries(
	t testing.TB, c *kernel.Client, sub Submission, match func([]Entry) bool, within time.Duration,
) []Entry {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	for {
		record := k.record(ctx, t, c, sub)
		if record.Entry != 0 {
			entries := k.entriesFrom(ctx, t, c, sub.Conversation, record.Entry-1)
			if match(entries) {
				return entries
			}
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

// entriesFrom reads the conversation's entries above `from`, oldest first, page by page.
//
// Ascending and paginated on purpose: the RPC's default order is newest first, which reads a turn backwards,
// and one page silently truncates a long one.
func (k *Kernel) entriesFrom(
	ctx context.Context, t testing.TB, c *kernel.Client, id kernel.ID, from kernel.ID,
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
			entries = append(entries, Entry{
				ID: entry.ID, Kind: entry.Kind, Text: entryText(entry), Raw: entry.Model,
			})
		}
		if len(page.Cursor) == 0 || string(page.Cursor) == "null" || len(page.Items) == 0 {
			return entries
		}
		cursor = page.Cursor
	}
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
