package fakekernel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// DefaultWindow is a view window wide enough for a test's transcript.
var DefaultWindow = kernel.Window{Min: 20, Max: 200}

// Submission is one submitted turn, and the boundary a wait for its reply starts from.
//
// The boundary is the point: `AfterEntry` is the highest entry id the conversation held **before** this
// submission. Without it a wait for "the reply" finds the previous turn's reply — the newest assistant entry
// in the window — and a two-turn test passes by reading the wrong turn. That false pass is why every wait
// here takes a Submission instead of a conversation id.
type Submission struct {
	Conversation kernel.ID
	// ID is the submission's own id, which the transcript's entries carry as `byTaskId`.
	ID kernel.ID
	// AfterEntry is the last entry id before this submission; a reply for it is above this.
	AfterEntry kernel.ID
}

// CreateConversation makes a conversation on the fake model's agent and opens it, so the connection gets
// conversation.view pushes for it. Its working directory is the Kernel's own workspace — never the source
// tree, and never the user's home.
func (k *Kernel) CreateConversation(t testing.TB, c *kernel.Client) kernel.ID {
	t.Helper()
	return k.CreateConversationIn(t, c, k.Workspace)
}

// CreateConversationIn is CreateConversation with a working directory, for a test whose tools must run in a
// place of their own. A directory under the Kernel's workspace keeps it throwaway; an absolute path under
// the test's t.TempDir() works too.
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

// Submit sends one message and returns the turn's boundary for waiting.
func (k *Kernel) Submit(t testing.TB, c *kernel.Client, id kernel.ID, text string) Submission {
	t.Helper()
	ctx, cancel := k.context(t, 60*time.Second)
	defer cancel()
	before := k.lastEntry(t, c, id)
	// No WhenBusy: the Kernel's values are "steer", "followUp" and "reject", and the Go client's
	// WhenBusyQueue ("queue") is refused as invalid params — see shared/interfaces/fake-kernel.md.
	result, err := c.Submit(ctx, kernel.SubmitParams{ID: id, Input: text})
	if err != nil {
		t.Fatalf("fakekernel: conversation.submit %q: %v\n%s", text, err, k.Logs())
	}
	return Submission{Conversation: id, ID: result.SubmissionID, AfterEntry: before}
}

// lastEntry is the conversation's highest entry id right now, which is what a Submission's AfterEntry holds.
func (k *Kernel) lastEntry(t testing.TB, c *kernel.Client, id kernel.ID) kernel.ID {
	t.Helper()
	ctx, cancel := k.context(t, 30*time.Second)
	defer cancel()
	page, err := c.Entries(ctx, kernel.EntriesParams{ID: id, Limit: 200})
	if err != nil {
		t.Fatalf("fakekernel: conversation.entries: %v\n%s", err, k.Logs())
	}
	var highest kernel.ID
	for _, entry := range page.Items {
		if entry.ID > highest {
			highest = entry.ID
		}
	}
	return highest
}

// WaitForReply waits until this submission's own assistant reply contains text, and returns it.
//
// Only entries above the submission's boundary count, so a wait cannot be answered by the previous turn.
func (k *Kernel) WaitForReply(t testing.TB, c *kernel.Client, sub Submission, text string, within time.Duration) string {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	for {
		page, err := c.Entries(ctx, kernel.EntriesParams{ID: sub.Conversation, Limit: 200})
		if err == nil {
			for _, entry := range page.Items {
				if entry.ID <= sub.AfterEntry || entry.Kind != "pi.assistant" {
					continue
				}
				if got := assistantText(entry.Model); strings.Contains(got, text) {
					return got
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fakekernel: submission %d got no assistant reply containing %q within %s: %v\n%s",
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

// Entry is one transcript entry above a submission's boundary, as a test reads it.
type Entry struct {
	ID   kernel.ID
	Kind string
	// Text is the entry's own text: an assistant message's text, or a tool result's content.
	Text string
	// Raw is the entry's model JSON, for a test that needs a part this does not decode.
	Raw json.RawMessage
}

// EntriesAfter is the conversation's transcript above the submission's boundary, oldest first.
//
// It is what a view test asserts on when it needs the whole turn — a tool call, its result, the reply —
// rather than one reply's text.
func (k *Kernel) EntriesAfter(t testing.TB, c *kernel.Client, sub Submission, within time.Duration) []Entry {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	page, err := c.Entries(ctx, kernel.EntriesParams{ID: sub.Conversation, Limit: 200})
	if err != nil {
		t.Fatalf("fakekernel: conversation.entries: %v\n%s", err, k.Logs())
	}
	entries := make([]Entry, 0, len(page.Items))
	for _, entry := range page.Items {
		if entry.ID <= sub.AfterEntry {
			continue
		}
		entries = append(entries, Entry{
			ID: entry.ID, Kind: entry.Kind, Text: entryText(entry), Raw: entry.Model,
		})
	}
	return entries
}

// WaitForEntries waits until the submission's turn holds an entry satisfying match, and returns the whole
// turn so far: a tool result and the reply that follows it, in one assertion.
func (k *Kernel) WaitForEntries(
	t testing.TB, c *kernel.Client, sub Submission, match func([]Entry) bool, within time.Duration,
) []Entry {
	t.Helper()
	ctx, cancel := k.context(t, within)
	defer cancel()
	for {
		page, err := c.Entries(ctx, kernel.EntriesParams{ID: sub.Conversation, Limit: 200})
		if err == nil {
			entries := make([]Entry, 0, len(page.Items))
			for _, entry := range page.Items {
				if entry.ID <= sub.AfterEntry {
					continue
				}
				entries = append(entries, Entry{
					ID: entry.ID, Kind: entry.Kind, Text: entryText(entry), Raw: entry.Model,
				})
			}
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

// ViewPush is one conversation.view notification, decoded.
type ViewPush struct {
	ID    kernel.ID
	View  *kernel.View
	Whole bool
}

// WaitForView waits for a conversation.view push that satisfies match, bounded by the test's deadline as
// well as by within.
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

// entryText is an entry's own text. Pi's model messages carry content parts: text, thinking, a tool call, or
// a tool result's blocks; this joins what a person would read.
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

// assistantText is an entry's assistant text, for the reply assertions.
func assistantText(model json.RawMessage) string {
	return entryText(kernel.Entry{Model: model})
}
