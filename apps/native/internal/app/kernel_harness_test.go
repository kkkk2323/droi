package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/app/testsupport"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// kernelSessions is the Kernel half of a window test (plan A of M2): a Fake Kernel beside the Fake Daemon, holding
// the scenario's Sessions as conversations. The window draws those Sessions from it, while its list, new Sessions
// and the Host's endpoints stay on the Fake Daemon.
type kernelSessions struct {
	k     *fakekernel.Kernel
	c     *kernel.Client
	convs map[string]kernel.ID
	// reader is the test's own connection, for reading what Kernel holds; dialled on first use.
	reader *kernel.Client
}

// Why a test keeps its Sessions on the Fake Daemon: the plan item each waits for.
const (
	untilSubagents  = "subagents land as child conversations and droi.subagent entries (M3, 6.3)"
	untilProcesses  = "the Daemon's jobs are read from Execute's words; Kernel's bash_background is processes_kernel_test.go's"
	untilCallTiming = "tok/s comes back in M4, when Kernel records each call's duration"
	goesWithCleanup = "a conversation is never closed or cleaned up while idle (gap-analysis §4.1); the test goes with M2's cleanup"
	untilPlanMode   = "Spec mode is replaced by plan mode (Q4, M3); the test goes with M2's cleanup"
	untilCodemode   = "a new Session's tool mode (tool calls or scripts) comes with codemode (M3, 3E)"
)

// onDaemon holds the tests that keep their Sessions on the Fake Daemon, by name, with why.
var onDaemon sync.Map

// keepOnDaemon keeps this test's Sessions on the Fake Daemon: what it checks has no Kernel counterpart yet. why
// names the plan item that gives it one, so the list of these is the list of what is left to move. Call it before
// the harness starts.
func keepOnDaemon(t *testing.T, why string) {
	t.Helper()
	onDaemon.Store(t.Name(), why)
	t.Cleanup(func() { onDaemon.Delete(t.Name()) })
}

// keptOnDaemon is why the test, or the test it runs under, keeps its Sessions on the Fake Daemon.
func keptOnDaemon(name string) (string, bool) {
	for {
		if why, ok := onDaemon.Load(name); ok {
			return why.(string), true
		}
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return "", false
		}
		name = name[:i]
	}
}

// kernelScripts change a test's Fake Kernel scenario, by test name.
var kernelScripts sync.Map

// scriptKernel has this test's Fake Kernel answer as change makes its scenario: a test whose model calls a tool.
// Call it before the harness starts.
func scriptKernel(t *testing.T, change func(*fakekernel.Scenario)) {
	t.Helper()
	kernelScripts.Store(t.Name(), change)
	t.Cleanup(func() { kernelScripts.Delete(t.Name()) })
}

// kernelSessionsEnv set to "daemon" runs every window test on the Fake Daemon alone, as before the Kernel: the way
// to tell whether a failure is the Kernel path's.
const kernelSessionsEnv = "DROI_VIEW_SESSIONS"

// startKernelSessions writes sc's Sessions into a new Fake Kernel, by the ids d gave them. It answers nil, and the
// test runs on the Fake Daemon alone, when the scenario has no Sessions, when part of it has no Kernel counterpart
// yet (logged, with the plan item that will give it one), or when there is no Kernel binary and
// DROI_FAKEKERNEL_REQUIRED is not set.
func startKernelSessions(t *testing.T, sc fakedaemon.Scenario, d *fakedaemon.Daemon) *kernelSessions {
	t.Helper()
	if len(sc.Sessions) == 0 || os.Getenv(kernelSessionsEnv) == "daemon" {
		return nil
	}
	if why, ok := keptOnDaemon(t.Name()); ok {
		t.Logf("the Sessions stay on the Fake Daemon: %s", why)
		return nil
	}
	ksc, err := testsupport.WindowScenario(sc)
	var unsupported *testsupport.UnsupportedScenarioError
	if errors.As(err, &unsupported) {
		t.Logf("the Sessions stay on the Fake Daemon: %v", err)
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	if !fakekernel.Available() && os.Getenv(fakekernel.RequiredEnv) == "" {
		return nil
	}
	if change, ok := kernelScripts.Load(t.Name()); ok {
		change.(func(*fakekernel.Scenario))(&ksc)
	}
	ids := make([]string, len(d.Sessions))
	for i, s := range d.Sessions {
		ids[i] = s.SessionID
	}
	// The Fake Daemon's fixtures start six hours ago, recent for the sidebar's three-day window.
	start := time.Now().Add(-6 * time.Hour)
	k := fakekernel.Start(t, ksc)
	c := k.Dial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	agent := &kernel.AgentChange{Model: kernel.Some(kernel.ModelRef{Provider: fakekernel.ProviderID, ModelID: fakekernel.ModelID})}
	convs, err := testsupport.ImportSessions(ctx, c, sc, ids, start, agent)
	if errors.As(err, &unsupported) {
		t.Logf("the Sessions stay on the Fake Daemon: %v", err)
		return nil
	}
	if err != nil {
		t.Fatalf("%v\n%s", err, k.Logs())
	}
	t.Logf("the Sessions are drawn from a Fake Kernel")
	return &kernelSessions{k: k, c: c, convs: convs}
}

// client is the test's own connection to the Kernel, dialled on first use.
func (ks *kernelSessions) client(t *testing.T) *kernel.Client {
	t.Helper()
	if ks.reader == nil {
		ks.reader = ks.k.Dial(t)
	}
	return ks.reader
}

// The Session Times' news from another client, which the Fake Daemon's Sessions hear through the Gateway and
// Kernel's conversations as Kernel keeps them.

// sentElsewhere is a message sent to Session id from another client, a phone's. On Kernel it is a user message in
// the conversation, which moves its last activity.
func (h *harness) sentElsewhere(id string) {
	h.t.Helper()
	if h.kernel == nil {
		h.times.Sent(id)
		return
	}
	h.kernel.add(h.t, id, fakedaemon.Message{Role: "user", Text: "From the phone"})
}

// endedElsewhere is a turn of Session id that ended while this window was not looking, an Automation's or a Loop's.
// On Kernel it is an answer in the conversation.
func (h *harness) endedElsewhere(id string) {
	h.t.Helper()
	if h.kernel == nil {
		h.times.Working(id)
		h.times.Ended(id)
		return
	}
	h.kernel.add(h.t, id, fakedaemon.Message{Role: "assistant", Text: "Done elsewhere."})
}

// viewedElsewhere is Session id shown on another client: on Kernel, opened there.
func (h *harness) viewedElsewhere(id string) {
	h.t.Helper()
	if h.kernel == nil {
		h.times.Viewed(id)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := h.kernel.client(h.t).Open(ctx, kernel.OpenParams{ID: h.kernel.convs[id], Window: &fakekernel.DefaultWindow}); err != nil {
		h.t.Fatal(err)
	}
}

// add appends m to Session session's conversation, as now, from the test's own connection.
func (ks *kernelSessions) add(t *testing.T, session string, m fakedaemon.Message) {
	t.Helper()
	entries, err := testsupport.History([]fakedaemon.Message{m}, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := ks.client(t).Import(ctx, kernel.ImportParams{ID: ks.convs[session], Entries: entries}); err != nil {
		t.Fatal(err)
	}
}

// seen is when Kernel says a person last saw Session session, and its last activity, in Unix ms.
func (ks *kernelSessions) seen(t *testing.T, session string) (viewed, activity int64) {
	t.Helper()
	s := ks.row(t, session)
	if s.ViewedAt != nil {
		viewed = *s.ViewedAt
	}
	return viewed, s.LastActivityAt
}

// row is Kernel's row of Session session's conversation, live or archived.
func (ks *kernelSessions) row(t *testing.T, session string) kernel.Summary {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, archived := range []bool{false, true} {
		params := kernel.ListParams{Limit: 200, Archived: &archived}
		for range 100 {
			res, err := ks.client(t).List(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range res.Items {
				if s.ID == ks.convs[session] {
					return s
				}
			}
			if len(res.Cursor) == 0 || len(res.Items) == 0 || bytes.Equal(res.Cursor, params.Cursor) {
				break
			}
			params.Cursor = res.Cursor
		}
	}
	t.Fatalf("Kernel does not list %s", session)
	return kernel.Summary{}
}

// archivedOnBackend waits until the backend that holds Session id was asked to archive it, or to unarchive it: the
// Daemon's daemon.archive_session, or Kernel's row.
func (h *harness) archivedOnBackend(id string, archived bool) {
	h.t.Helper()
	if h.kernel == nil {
		method := "daemon.archive_session"
		if !archived {
			method = "daemon.unarchive_session"
		}
		if _, err := h.d.WaitForRequest(method, 1); err != nil {
			h.t.Fatal(err)
		}
		return
	}
	h.until("Kernel's row", func() bool { return h.kernel.row(h.t, id).Archived == archived })
}

// id is Config.KernelID for the window: the conversation that holds a Session the scenario described.
func (ks *kernelSessions) id(session string) (kernel.ID, bool) {
	id, ok := ks.convs[session]
	return id, ok
}

// session is Config.KernelSession: the Session a conversation holds.
func (ks *kernelSessions) session(conv kernel.ID) (string, bool) {
	for session, id := range ks.convs {
		if id == conv {
			return session, true
		}
	}
	return "", false
}

// inbox is what Kernel holds queued for a Session's conversation, read on a connection of the test's own: opening
// it on the window's would take the window's subscription.
func (ks *kernelSessions) inbox(t *testing.T, session string) []sessionview.InboxItem {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opened, err := ks.client(t).Open(ctx, kernel.OpenParams{ID: ks.convs[session], Window: &fakekernel.DefaultWindow})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Items []sessionview.InboxItem `json:"items"`
	}
	if raw := opened.View.Doc("pi.inbox"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
	}
	return doc.Items
}

// withdrew waits until the backend that holds Session id no longer holds the queued message text: the Daemon was
// asked to delete it, or Kernel's inbox lost it.
func (h *harness) withdrew(id, text string) {
	h.t.Helper()
	if h.kernel == nil {
		req, err := h.d.WaitForRequest("daemon.resolve_queued_user_message", 1)
		if err != nil {
			h.t.Fatal(err)
		}
		if !strings.Contains(string(req.Params), `"action":"delete"`) {
			h.t.Fatalf("params %v", req.Params)
		}
		return
	}
	h.until("Kernel to let the queued message go", func() bool {
		for _, item := range h.kernel.inbox(h.t, id) {
			if strings.Contains(string(item.Content), text) {
				return false
			}
		}
		return true
	})
}

// sentText waits until the backend that holds the open Session was sent a message of exactly text: the Daemon's
// daemon.add_user_message, or the question Kernel put to the model.
func (h *harness) sentText(text string) {
	h.t.Helper()
	if h.kernel == nil {
		h.sent(protocol.MethodAddUserMessage, func(p map[string]any) bool { return p["text"] == text })
		return
	}
	h.until("the model asked with the message", func() bool {
		for _, r := range h.kernel.k.Requests() {
			if r.Prompt() == text {
				return true
			}
		}
		return false
	})
}
