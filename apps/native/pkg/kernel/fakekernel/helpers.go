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

// CreateConversation makes a conversation on the fake model's agent and opens it, so the connection gets
// conversation.view pushes for it.
func (k *Kernel) CreateConversation(t testing.TB, c *kernel.Client) kernel.ID {
	t.Helper()
	return k.CreateConversationIn(t, c, "")
}

// CreateConversationIn is CreateConversation with a working directory, which is what a test of tool output
// wants: the tools run there.
func (k *Kernel) CreateConversationIn(t testing.TB, c *kernel.Client, dir string) kernel.ID {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
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

// Submit sends one message and returns the submission's id.
func (k *Kernel) Submit(t testing.TB, c *kernel.Client, id kernel.ID, text string) kernel.ID {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	// No WhenBusy: the Kernel's values are "steer", "followUp" and "reject", and the Go client's
	// WhenBusyQueue ("queue") is refused as invalid params — see shared/interfaces/fake-kernel.md.
	result, err := c.Submit(ctx, kernel.SubmitParams{ID: id, Input: text})
	if err != nil {
		t.Fatalf("fakekernel: conversation.submit %q: %v\n%s", text, err, k.Logs())
	}
	return result.SubmissionID
}

// WaitForReply waits until the conversation's transcript holds an assistant message with the text, and
// returns it. It reads the entries rather than the view: the view is a window, and a long transcript scrolls
// the reply out of it.
func (k *Kernel) WaitForReply(t testing.TB, c *kernel.Client, id kernel.ID, text string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		page, err := c.Entries(ctx, kernel.EntriesParams{ID: id, Limit: 200})
		cancel()
		if err == nil {
			for _, entry := range page.Items {
				if entry.Kind != "pi.assistant" {
					continue
				}
				if got := assistantText(entry.Model); strings.Contains(got, text) {
					return got
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("fakekernel: no assistant reply containing %q in %s\n%s", text, within, k.Logs())
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// ViewPush is one conversation.view notification, decoded.
type ViewPush struct {
	ID    kernel.ID
	View  *kernel.View
	Whole bool
}

// WaitForView waits for a view push for the conversation that satisfies match. It consumes notifications as
// it goes, so a test that needs the pushes before this call must read them itself.
func (k *Kernel) WaitForView(t testing.TB, c *kernel.Client, match func(ViewPush) bool, within time.Duration) ViewPush {
	t.Helper()
	deadline := time.After(within)
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
		case <-deadline:
			t.Fatalf("fakekernel: no matching view push in %s\n%s", within, k.Logs())
		}
	}
}

// assistantText is an entry's assistant text: Pi's model messages, whose parts may be text or thinking.
func assistantText(model json.RawMessage) string {
	var messages []struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(model, &messages); err != nil {
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
				if block.Type == "text" {
					parts = append(parts, block.Text)
				}
			}
		}
	}
	return strings.Join(parts, "")
}
