package titles_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/titles"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// A conversation named by the model: the title it answered, written to the list, and the call that asked for
// it carrying the question, the answer and the language rule.
func TestANamedConversationGetsTheTitleTheModelGave(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Steps:       []fakekernel.Step{{Text: "The workflow file is fixed."}},
		Completions: []fakekernel.Completion{{Text: `{"title":"修复部署脚本"}`}},
	})
	c := k.Dial(t)
	o := k.Observe(c)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "帮我修复部署脚本，它一直在 CI 里失败", "The workflow file is fixed.", 30*time.Second)

	m := titles.New(titles.Config{Source: o, TitleSource: sourceOf(t, c), Logf: t.Logf})
	got, err := m.Name(context.Background(), id)
	if err != nil {
		t.Fatalf("naming: %v", err)
	}
	if got != "修复部署脚本" {
		t.Fatalf("the title is %q, not the model's answer", got)
	}
	if row := listRow(t, c, id); row.Title != "修复部署脚本" || row.TitleSource != kernel.TitleSourceChosen {
		t.Fatalf("the list has %q (%s) as the title, want the model's, written as chosen", row.Title, row.TitleSource)
	}

	calls := k.Completions()
	if len(calls) != 1 {
		t.Fatalf("%d model.complete calls, want 1", len(calls))
	}
	prompt := calls[0].FullPrompt()
	if !strings.Contains(prompt, "修复部署脚本") {
		t.Fatalf("the call did not carry the question: %q", prompt)
	}
	if !strings.Contains(prompt, "workflow file is fixed") {
		t.Fatalf("the call did not carry the answer: %q", prompt)
	}
	if !strings.Contains(prompt, "same language as the user") {
		t.Fatalf("the call did not tell the model which language to answer in: %q", prompt)
	}
	if updates := o.CallsOf(kernel.MethodIndexUpdate); len(updates) != 1 {
		t.Fatalf("%d index.update calls, want 1", len(updates))
	}
}

// A model that fails costs no title: the conversation is named with the user's own words, which is a line
// somebody can read, already in their language, and needs no second call.
func TestAFailedModelStillNamesTheConversationWithItsFirstWords(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Steps:       []fakekernel.Step{{Text: "ok"}},
		Completions: []fakekernel.Completion{{Failure: &fakekernel.Failure{Status: 500, Body: "upstream exploded"}}},
	})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "fix the deploy\nplease", "ok", 30*time.Second)

	m := titles.New(titles.Config{Source: c, TitleSource: sourceOf(t, c)})
	got, err := m.Name(context.Background(), id)
	if err != nil {
		t.Fatalf("naming: %v", err)
	}
	if got != "fix the deploy please" {
		t.Fatalf("the fallback title is %q", got)
	}
	if title := listTitle(t, c, id); title != "fix the deploy please" {
		t.Fatalf("the list has %q as the title", title)
	}
}

// A title somebody chose is never replaced: the user renamed the conversation, so the model is not asked at
// all, and no write follows.
func TestAConversationSomebodyNamedIsLeftAlone(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Steps:       []fakekernel.Step{{Text: "ok"}},
		Completions: []fakekernel.Completion{{Text: `{"title":"a name nobody asked for"}`}},
	})
	c := k.Dial(t)
	o := k.Observe(c)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "fix the deploy", "ok", 30*time.Second)
	renamed := "部署修复（我自己起的）"
	if err := c.UpdateIndex(context.Background(), kernel.IndexUpdateParams{ID: id, Title: kernel.TitleText(renamed)}); err != nil {
		t.Fatal(err)
	}

	m := titles.New(titles.Config{Source: o, TitleSource: sourceOf(t, c)})
	got, err := m.Name(context.Background(), id)
	if err != nil {
		t.Fatalf("naming: %v", err)
	}
	if got != "" {
		t.Fatalf("it wrote a title %q over the user's", got)
	}
	if calls := k.Completions(); len(calls) != 0 {
		t.Fatalf("%d model calls, want none: the title was already there", len(calls))
	}
	if updates := o.CallsOf(kernel.MethodIndexUpdate); len(updates) != 0 {
		t.Fatalf("%d index.update calls, want none", len(updates))
	}
	if title := listTitle(t, c, id); title != renamed {
		t.Fatalf("the user's title is now %q", title)
	}
}

// The trigger is the end of a round, and a conversation is named once: two ends are one title, and a round
// that is still running names nothing.
func TestNamingHappensAtTheEndOfARoundAndOnlyOnce(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Steps:       []fakekernel.Step{{Text: "ok"}},
		Completions: []fakekernel.Completion{{Text: `{"title":"deploy the fix"}`}},
	})
	c := k.Dial(t)
	o := k.Observe(c)
	id := k.CreateConversation(t, c)
	sub := k.Submit(t, c, id, "fix the deploy")
	k.WaitForReply(t, c, sub, "ok", 30*time.Second)

	m := titles.New(titles.Config{Source: o, TitleSource: sourceOf(t, c), Logf: t.Logf})
	// A round that is running names nothing, even though the first answer is already there.
	m.Busy(context.Background(), kernel.BusyNotification{ID: id, Run: []byte(`{"taskId":1}`)})
	m.Wait()
	if calls := k.Completions(); len(calls) != 0 {
		t.Fatalf("%d model calls during a round, want none", len(calls))
	}

	m.Busy(context.Background(), kernel.BusyNotification{ID: id, Run: []byte("null")})
	m.Wait()
	if title := listTitle(t, c, id); title != "deploy the fix" {
		t.Fatalf("the title is %q after the round ended", title)
	}

	m.Busy(context.Background(), kernel.BusyNotification{ID: id, Run: []byte("null")})
	m.Wait()
	if updates := o.CallsOf(kernel.MethodIndexUpdate); len(updates) != 1 {
		t.Fatalf("%d index.update calls after two ends, want one", len(updates))
	}
	if calls := k.Completions(); len(calls) != 1 {
		t.Fatalf("%d model calls after two ends, want one", len(calls))
	}
}

// A name is written once, for good: the Droi that starts tomorrow, or another client, remembers nothing of today's
// naming, and finds the conversation named.
func TestANamedConversationIsNotNamedAgainAfterARestart(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Steps:       []fakekernel.Step{{Text: "ok"}, {Text: "done"}},
		Completions: []fakekernel.Completion{{Text: `{"title":"deploy the fix"}`}, {Text: `{"title":"a second name"}`}},
	})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "fix the deploy", "ok", 30*time.Second)
	if got, err := titles.New(titles.Config{Source: c, TitleSource: sourceOf(t, c)}).Name(context.Background(), id); err != nil || got != "deploy the fix" {
		t.Fatalf("the first naming wrote %q (%v)", got, err)
	}

	k.Ask(t, c, id, "and the staging one", "done", 30*time.Second)
	o := k.Observe(c)
	restarted := titles.New(titles.Config{Source: o, TitleSource: sourceOf(t, c)})
	restarted.Busy(context.Background(), kernel.BusyNotification{ID: id, Run: []byte("null")})
	restarted.Wait()
	if calls := k.Completions(); len(calls) != 1 {
		t.Fatalf("%d model calls, want the first one only", len(calls))
	}
	if updates := o.CallsOf(kernel.MethodIndexUpdate); len(updates) != 0 {
		t.Fatalf("%d index.update calls after the restart, want none", len(updates))
	}
	if title := listTitle(t, c, id); title != "deploy the fix" {
		t.Fatalf("the title is now %q", title)
	}
}

// A conversation with no question yet has nothing to be named after, and naming it would spend a call on
// nothing.
func TestAConversationWithNothingSaidIsNotNamed(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Completions: []fakekernel.Completion{{Text: `{"title":"never"}`}},
	})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	m := titles.New(titles.Config{Source: c, TitleSource: sourceOf(t, c)})
	if got, err := m.Name(context.Background(), id); err != nil || got != "" {
		t.Fatalf("named an empty conversation %q (%v)", got, err)
	}
	if calls := k.Completions(); len(calls) != 0 {
		t.Fatalf("%d model calls, want none", len(calls))
	}
}

// A Kernel that cannot name anything still leaves every conversation with a title: the model is reported
// unsupported, and the fallback names the conversation.
func TestAKernelWithoutModelsFallsBackToTheFirstWords(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{{Text: "ok"}}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "rename me", "ok", 30*time.Second)

	m := titles.New(titles.Config{Source: unsupported{c}, TitleSource: sourceOf(t, c)})
	got, err := m.Name(context.Background(), id)
	if err != nil {
		t.Fatalf("naming: %v", err)
	}
	if got != "rename me" {
		t.Fatalf("the fallback title is %q", got)
	}
	if title := listTitle(t, c, id); title != "rename me" {
		t.Fatalf("the list has %q", title)
	}
}

// unsupported answers every model.complete the way a Kernel whose capabilities.models is false does.
type unsupported struct{ *kernel.Client }

func (unsupported) ModelComplete(context.Context, kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error) {
	return kernel.ModelCompleteResult{}, &kernel.RPCError{Code: kernel.CodeUnsupported, Message: "models is not implemented in this build"}
}

// sourceOf reads where a conversation's title came from in the list, the way the window's list would.
func sourceOf(t *testing.T, c *kernel.Client) func(ctx context.Context, id kernel.ID) (string, bool) {
	t.Helper()
	return func(ctx context.Context, id kernel.ID) (string, bool) {
		return listRow(t, c, id).TitleSource, true
	}
}

// listTitle is one conversation's title in the list, "" when it has none.
func listTitle(t *testing.T, c *kernel.Client, id kernel.ID) string {
	t.Helper()
	return listRow(t, c, id).Title
}

func listRow(t *testing.T, c *kernel.Client, id kernel.ID) kernel.Summary {
	t.Helper()
	page, err := c.List(context.Background(), kernel.ListParams{Limit: 200})
	if err != nil {
		t.Fatalf("conversation.list: %v", err)
	}
	for _, row := range page.Items {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("conversation %d is not in the list", id)
	return kernel.Summary{}
}

// The Host does not poll: it feeds naming the conversation.busy notifications Kernel already broadcasts, which
// says a round ended without anything having to be opened first. This drives that path end to end — a submit,
// the Kernel's own notifications, a title — so a naming that only worked when a test called Name directly
// would fail here.
func TestNamingFollowsTheConversationsBusyNotifications(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Steps:       []fakekernel.Step{{Text: "the workflow file is fixed"}},
		Completions: []fakekernel.Completion{{Text: `{"title":"修复部署脚本"}`}},
	})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	m := titles.New(titles.Config{Source: c, TitleSource: sourceOf(t, c), Logf: t.Logf})
	defer m.Wait()

	sub := k.Submit(t, c, id, "帮我修复部署脚本")

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case note, ok := <-c.Notifications():
			if !ok {
				t.Fatalf("the connection closed: %v", c.Err())
			}
			if note.Method != "conversation.busy" {
				continue
			}
			var busy kernel.BusyNotification
			if err := note.Decode(&busy); err != nil {
				t.Fatal(err)
			}
			m.Busy(context.Background(), busy)
		case <-time.After(100 * time.Millisecond):
		}
		if title := listTitle(t, c, id); title == "修复部署脚本" {
			k.WaitForReply(t, c, sub, "the workflow file is fixed", 30*time.Second)
			return
		}
	}
	t.Fatalf("no title after the round ended; the list has %q", listTitle(t, c, id))
}
