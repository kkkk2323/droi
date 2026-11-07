package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLiveKernelThreeConversations is M1's first acceptance check: three
// conversations on one Kernel, two turns each, all at the same time, and each
// transcript holds its own words only.
//
// It needs a Kernel binary (DROI_KERNEL_BIN, or the release's build) and points
// its provider at a local fake one.
func TestLiveKernelThreeConversations(t *testing.T) {
	provider := startFakeProvider(t)
	k := startLiveKernel(t, provider)
	ctx := t.Context()

	info, err := k.Client.Harness(ctx)
	if err != nil {
		t.Fatalf("harness.info: %v\n%s", err, k.Log.String())
	}
	if len(info.Models) == 0 {
		t.Fatalf("harness.info lists no models:\n%s", k.Log.String())
	}
	t.Logf("kernel %s, %d models, capabilities %+v", info.Version, len(info.Models), info.Capabilities)

	const conversations, turns = 3, 2
	ids := make([]ID, conversations)
	for i := range ids {
		// The agent is named for each conversation: Kernel ignores its config
		// file's default agent today (reported), so a conversation that names
		// none runs with "No model is configured".
		created, err := k.Client.Create(ctx, CreateParams{Kind: "chat", Cwd: t.TempDir(),
			Agent: &AgentChange{Model: Some(ModelRef{Provider: "fake", ModelID: "fake-1"})}})
		if err != nil {
			t.Fatalf("conversation %d: %v\n%s", i, err, k.Log.String())
		}
		ids[i] = created.ConversationID
		if _, err := k.Client.Open(ctx, OpenParams{ID: ids[i], Window: &Window{Min: 20, Max: 100}}); err != nil {
			t.Fatalf("open %d: %v", ids[i], err)
		}
	}

	// Every conversation sends its turns at the same time.
	var wg sync.WaitGroup
	errs := make(chan error, conversations*turns)
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for turn := 1; turn <= turns; turn++ {
				text := fmt.Sprintf("conversation %d turn %d", i+1, turn)
				if _, err := k.Client.Submit(ctx, SubmitParams{ID: id, Input: text, WhenBusy: WhenBusyFollowUp}); err != nil {
					errs <- fmt.Errorf("%s: %w", text, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("submit: %v", err)
	}

	// Wait until every conversation has said its own turns back.
	deadline := time.Now().Add(3 * time.Minute)
	for _, id := range ids {
		for {
			if time.Now().After(deadline) {
				t.Fatalf("conversation %d never finished its turns:\n%s", id, k.Log.String())
			}
			entries, err := k.Client.Entries(ctx, EntriesParams{ID: id, Limit: 200})
			if err == nil {
				echoes := 0
				for _, entry := range entries.Items {
					if entry.Kind == EntryAssistant && strings.Contains(string(entry.Model), "echo:") {
						echoes++
					}
				}
				if echoes >= turns {
					break
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
	}

	// No conversation may hold another's words.
	for i, id := range ids {
		entries, err := k.Client.Entries(ctx, EntriesParams{ID: id, Limit: 400})
		if err != nil {
			t.Fatalf("entries %d: %v", id, err)
		}
		body := fmt.Sprint(entries.Items)
		for other := 1; other <= conversations; other++ {
			marker := fmt.Sprintf("conversation %d turn", other)
			has := strings.Contains(body, marker)
			if has != (other == i+1) {
				t.Errorf("conversation %d holds %q: %v", id, marker, has)
			}
		}
	}
	t.Logf("provider answered %d requests", provider.calls.Load())
}

// TestLiveKernelEveryMethod walks every method of the contract against a real
// Kernel: each answers, or answers the contract's own refusal, and the list must
// match Methods exactly or the walk fails.
func TestLiveKernelEveryMethod(t *testing.T) {
	provider := startFakeProvider(t)
	k := startLiveKernel(t, provider)
	c := k.Client
	ctx := t.Context()

	model := ModelRef{Provider: "fake", ModelID: "fake-1"}
	created, err := c.Create(ctx, CreateParams{Kind: "chat", Cwd: t.TempDir(),
		Agent: &AgentChange{Model: Some(model)}})
	if err != nil {
		t.Fatalf("create: %v\n%s", err, k.Log.String())
	}
	id := created.ConversationID
	if _, err := c.Open(ctx, OpenParams{ID: id}); err != nil {
		t.Fatalf("open: %v", err)
	}
	// A real turn first, so the methods below have something to work on: a
	// fork needs an entry that exists, usage needs a model call, and
	// submission.get needs a submission that settled.
	submitted, err := c.Submit(ctx, SubmitParams{ID: id, Input: "the walk's first turn", WhenBusy: WhenBusyFollowUp})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	firstSubmission := submitted.SubmissionID
	var lastEntry ID
	for deadline := time.Now().Add(90 * time.Second); ; {
		entries, err := c.Entries(ctx, EntriesParams{ID: id, Limit: 100})
		if err == nil && len(entries.Items) > 0 {
			lastEntry = entries.Items[0].ID
			done := false
			for _, e := range entries.Items {
				if e.Kind == EntryAssistant {
					done = true
				}
			}
			if done {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the walk's first turn never finished:\n%s", k.Log.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	type step struct {
		method string
		call   func() error
		// wantCode is the refusal the contract allows at this stage; 0 means
		// the method must work.
		wantCode int
	}
	steps := []step{
		{"harness.info", func() error { _, err := c.Harness(ctx); return err }, 0},
		{"conversation.create", func() error { _, err := c.Create(ctx, CreateParams{Kind: "chat", Cwd: t.TempDir()}); return err }, 0},
		{"conversation.list", func() error {
			// The sidebar's rows: no children, and the repository a conversation belongs to.
			page, err := c.List(ctx, ListParams{Limit: 10})
			if err == nil && len(page.Items) == 0 {
				t.Error("conversation.list answered no rows")
			}
			return err
		}, 0},
		{"conversation.open", func() error { _, err := c.Open(ctx, OpenParams{ID: id}); return err }, 0},
		{"conversation.unwatch", func() error { return c.Unwatch(ctx, id) }, 0},
		{"conversation.submit", func() error {
			_, err := c.Submit(ctx, SubmitParams{ID: id, Input: "method walk", WhenBusy: WhenBusyFollowUp})
			return err
		}, 0},
		{"conversation.abort", func() error { return c.Abort(ctx, AbortParams{ID: id}) }, 0},
		{"submission.abort", func() error {
			// A submission that already settled is the contract's -32004.
			_, err := c.AbortSubmission(ctx, AbortSubmissionParams{ID: id, SubmissionID: 999999})
			return err
		}, CodeSubmissionGone},
		{"submission.get", func() error {
			record, err := c.SubmissionGet(ctx, AbortSubmissionParams{ID: id, SubmissionID: firstSubmission})
			if err == nil && record.Status != "done" {
				t.Errorf("submission.get answered %q for the turn that finished", record.Status)
			}
			return err
		}, 0},
		{"conversation.configure", func() error {
			level := "off"
			_, err := c.Configure(ctx, ConfigureParams{ID: id, Change: &AgentChange{ThinkingLevel: Some(level)}})
			return err
		}, 0},
		{"conversation.entries", func() error { _, err := c.Entries(ctx, EntriesParams{ID: id, Limit: 5}); return err }, 0},
		{"conversation.usage", func() error { _, err := c.Usage(ctx, id); return err }, 0},
		{"conversation.compact", func() error { _, err := c.Compact(ctx, CompactParams{ID: id}); return err }, 0},
		{"conversation.fork", func() error {
			forked, err := c.Fork(ctx, ForkParams{ID: id, At: lastEntry})
			if err == nil && forked.ConversationID == 0 {
				t.Error("a fork answered no conversation")
			}
			return err
		}, 0},
		{"conversation.import", func() error {
			createdAt := int64(1762480000000)
			imported, err := c.Import(ctx, ImportParams{
				Key:       "the-walks-import",
				CreatedAt: &createdAt,
				Entries: []ImportEntry{{
					Kind:  EntryUser,
					Model: json.RawMessage(`[{"role":"user","content":"imported by the walk","timestamp":1762480000000}]`),
				}},
			})
			if err == nil && (imported.ConversationID == 0 || len(imported.EntryIDs) != 1) {
				t.Errorf("conversation.import answered %d with %d entries", imported.ConversationID, len(imported.EntryIDs))
			}
			return err
		}, 0},
		{"task.abort", func() error { _, err := c.AbortTask(ctx, AbortTaskParams{TaskID: 999999}); return err }, CodeTaskGone},
		{"index.update", func() error {
			title := "walked"
			return c.UpdateIndex(ctx, IndexUpdateParams{ID: id, Title: TitleText(title)})
		}, 0},
		{"process.list", func() error { _, err := c.Processes(ctx, id); return err }, 0},
		// M1 answers the write methods of a capability it does not have with
		// -32009, which is what these three report.
		{"process.stop", func() error { _, err := c.StopProcess(ctx, StopProcessParams{ID: id, ProcessID: "nope"}); return err }, CodeUnsupported},
		{"approval.respond", func() error {
			_, err := c.RespondApproval(ctx, ApprovalRespondParams{RequestID: "nope", Outcome: ApprovalCancel})
			return err
		}, CodeUnsupported},
		{"ask.respond", func() error {
			_, err := c.RespondAsk(ctx, AskRespondParams{RequestID: "nope", Cancelled: boolPtr(true)})
			return err
		}, CodeUnsupported},
		{"mcp.list", func() error { _, err := c.MCP(ctx, MCPListParams{}); return err }, 0},
		{"mcp.set", func() error {
			_, err := c.SetMCP(ctx, MCPSetParams{Name: "walk", Server: &MCPServerConfig{Transport: "http", URL: "https://example.invalid/mcp"}})
			return err
		}, CodeUnsupported},
		{"mcp.authorize", func() error { _, err := c.AuthorizeMCP(ctx, MCPAuthorizeParams{Name: "walk"}); return err }, CodeUnsupported},
		{"mcp.authorize.cancel", func() error { return c.CancelMCPAuthorize(ctx, MCPAuthorizeParams{Name: "walk"}) }, CodeUnsupported},
		{"skills.list", func() error { _, err := c.Skills(ctx); return err }, 0},
		{"skills.setDisabled", func() error {
			_, err := c.SetSkillDisabled(ctx, SkillsSetDisabledParams{Name: "walk", Disabled: true})
			return err
		}, CodeUnsupported},
		{"policy.set", func() error {
			_, err := c.PolicySet(ctx, PolicySetParams{Policy: PolicyChange{Autonomy: "ask"}})
			return err
		}, CodeUnsupported},
		{"resources.list", func() error {
			_, err := c.Resources(ctx, ResourcesListParams{ID: &id})
			return err
		}, CodeUnsupported},
		{"model.complete", func() error {
			// A real answer, not a refusal: the method is built, so its text must be the model's.
			answer, err := c.ModelComplete(ctx, ModelCompleteParams{Model: model, Input: "say hi"})
			if err == nil && answer.Text == "" {
				t.Error("model.complete answered no text")
			}
			return err
		}, 0},
		// Last: it tells Kernel to close, and it does.
		{"kernel.shutdown", func() error {
			result, err := c.Shutdown(ctx)
			if err == nil && result.Status != ShutdownClosing {
				t.Errorf("kernel.shutdown answered %q", result.Status)
			}
			return err
		}, 0},
	}
	if len(steps) != len(Methods) {
		t.Fatalf("the walk covers %d methods, the contract has %d", len(steps), len(Methods))
	}
	walked := map[string]bool{}
	for _, s := range steps {
		walked[s.method] = true
		err := s.call()
		switch {
		case s.wantCode == 0 && err != nil:
			t.Errorf("%s failed: %v", s.method, err)
		case s.wantCode != 0 && !Is(err, s.wantCode):
			t.Errorf("%s answered %v, want code %d", s.method, err, s.wantCode)
		default:
			t.Logf("%s: ok (%v)", s.method, err)
		}
	}
	for _, method := range Methods {
		if !walked[method] {
			t.Errorf("the walk never called %s", method)
		}
	}
	if rest := strings.TrimSpace(k.Log.String()); rest != "" {
		t.Logf("kernel log (tail):\n%s", tail(rest, 2000))
	}
}

func boolPtr(b bool) *bool { return &b }

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

var _ = json.Marshal
var _ = context.Background
