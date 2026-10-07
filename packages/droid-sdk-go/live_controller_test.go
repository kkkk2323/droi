package droid_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func liveModel() string {
	if m := os.Getenv("DROID_LIVE_MODEL"); m != "" {
		return m
	}
	return "glm-5.3-flash"
}

// eventLog keeps a Controller's events.
type eventLog struct {
	mu     sync.Mutex
	events []controller.Event
	ch     chan struct{}
}

func logEvents(c *controller.Controller) *eventLog {
	l := &eventLog{ch: make(chan struct{}, 1)}
	c.Subscribe(func(e controller.Event) {
		l.mu.Lock()
		l.events = append(l.events, e)
		l.mu.Unlock()
		select {
		case l.ch <- struct{}{}:
		default:
		}
	})
	return l
}

func (l *eventLog) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

// wait returns the first event from index from on that ok accepts, and the
// index after it.
func (l *eventLog) wait(t *testing.T, ctx context.Context, from int, what string, ok func(controller.Event) bool) (controller.Event, int) {
	t.Helper()
	for {
		l.mu.Lock()
		for i := from; i < len(l.events); i++ {
			if ok(l.events[i]) {
				e := l.events[i]
				l.mu.Unlock()
				return e, i + 1
			}
		}
		l.mu.Unlock()
		select {
		case <-l.ch:
		case <-ctx.Done():
			t.Fatalf("no event: %s", what)
		}
	}
}

// text is what the agent streamed in a Session since index from.
func (l *eventLog) text(sessionID string, from int) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, e := range l.events[from:] {
		if n, ok := e.(controller.SessionNotification); ok && n.SessionID == sessionID {
			if d, ok := n.Value.(*protocol.AssistantTextDeltaNotification); ok {
				b.WriteString(d.TextDelta)
			}
		}
	}
	return b.String()
}

func turnCompleted(sessionID string) func(controller.Event) bool {
	return func(e controller.Event) bool {
		n, ok := e.(controller.SessionNotification)
		if !ok || n.SessionID != sessionID {
			return false
		}
		_, ok = n.Value.(*protocol.AgentTurnCompletedNotification)
		return ok
	}
}

func gitRepo(t *testing.T) string {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Acme\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "init")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Acme\n\nNow with docs.\n"), 0o644)
	return dir
}

// TestLiveController drives a real Daemon through the Controller: turns, a
// permission, a message sent while the agent is busy, the session and
// workspace methods, and a Daemon restart in the middle of a Session.
func TestLiveController(t *testing.T) {
	d := startLive(t)
	key := os.Getenv("FACTORY_API_KEY")
	ctl := controller.New(controller.Config{
		URL:                  d.URL,
		Credential:           func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: key}, nil },
		MaxReconnectAttempts: 20,
		MaxReconnectDelay:    2 * time.Second,
		Record:               recorder(t, "controller"),
	})
	defer ctl.Close()
	ev := logEvents(ctl)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := ctl.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	cwd := gitRepo(t)
	init, err := ctl.InitializeSession(ctx, protocol.InitializeSessionParams{Cwd: cwd, ModelID: liveModel(), AutonomyLevel: protocol.AutonomyLevelOff})
	if err != nil {
		t.Fatal("initialize_session:", err)
	}
	id := init.SessionID
	cl := func() *droid.Client {
		c, err := ctl.Client()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	turn := func(t *testing.T, text string) string {
		t.Helper()
		from := ev.len()
		if _, err := ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: text}); err != nil {
			t.Fatal("add_user_message:", err)
		}
		ev.wait(t, ctx, from, "turn completed", turnCompleted(id))
		return ev.text(id, from)
	}

	t.Run("turn", func(t *testing.T) {
		if got := turn(t, "Reply with exactly the word pong and nothing else."); !strings.Contains(strings.ToLower(got), "pong") {
			t.Fatalf("reply %q", got)
		}
	})

	t.Run("permission", func(t *testing.T) {
		from := ev.len()
		if _, err := ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id,
			Text: "Use the Execute tool to run exactly this shell command: echo droid-sdk-go > out.txt . Then reply done."}); err != nil {
			t.Fatal(err)
		}
		e, next := ev.wait(t, ctx, from, "permission requested", func(e controller.Event) bool { _, ok := e.(controller.PermissionRequested); return ok })
		p := e.(controller.PermissionRequested).Permission
		t.Logf("permission for %s, options %d", p.ToolUses[0].ToolUse.Name, len(p.Options))
		if err := ctl.RespondToPermission(ctx, p.RequestID, controller.PermissionAnswer{SelectedOption: protocol.ToolConfirmationOutcomeProceedOnce}); err != nil {
			t.Fatal(err)
		}
		ev.wait(t, ctx, next, "turn completed", turnCompleted(id))
		if b, err := os.ReadFile(filepath.Join(cwd, "out.txt")); err != nil || strings.TrimSpace(string(b)) != "droid-sdk-go" {
			t.Fatalf("out.txt: %q %v", b, err)
		}
		if n := len(ctl.PendingPermissions("")); n != 0 {
			t.Fatalf("%d permissions still pending", n)
		}
	})

	t.Run("busy", func(t *testing.T) {
		from := ev.len()
		if _, err := ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: "Without using any tools, write the numbers from 1 to 60, one per line, and nothing else."}); err != nil {
			t.Fatal(err)
		}
		begin := time.Now()
		if _, err := ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: "Then, without using any tools, reply with exactly the word queued-ok."}); err != nil {
			t.Fatal("second message while busy:", err)
		}
		t.Logf("message sent while busy returned after %v", time.Since(begin).Round(time.Millisecond))
		deadline, stop := context.WithTimeout(ctx, 3*time.Minute)
		defer stop()
		next := from
		for !strings.Contains(ev.text(id, from), "queued-ok") {
			var e controller.Event
			e, next = ev.wait(t, deadline, next, "reply to the message sent while busy", func(e controller.Event) bool {
				_, asked := e.(controller.PermissionRequested)
				return asked || turnCompleted(id)(e)
			})
			if p, ok := e.(controller.PermissionRequested); ok {
				if err := ctl.RespondToPermission(ctx, p.Permission.RequestID, controller.PermissionAnswer{SelectedOption: protocol.ToolConfirmationOutcomeProceedOnce}); err != nil {
					t.Fatal(err)
				}
			}
		}
		s := ctl.Store().Session(id)
		if q := s.QueuedMessages(); len(q) != 0 {
			t.Fatalf("still queued after both turns: %+v", q)
		}
		users := 0
		for _, m := range s.Messages() {
			if m.Role == "user" {
				users++
			}
		}
		t.Logf("store: %d messages, %d from the user", len(s.Messages()), users)
		for end := time.Now().Add(15 * time.Second); s.WorkingState() != protocol.DroidWorkingStateIdle; time.Sleep(50 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("working state %s after both turns", s.WorkingState())
			}
		}
		daemon := must(cl().GetSessionMessages(ctx, protocol.GetSessionMessagesParams{SessionID: id, Limit: ptr(100.0)}))(t)
		var want, got []string
		for _, m := range daemon.Messages {
			// Empty user_only bookkeeping messages are stored but never sent.
			if m.Visibility == "user_only" && len(m.Content) == 0 {
				continue
			}
			want = append(want, m.ID)
		}
		slices.Reverse(want)
		for _, m := range s.Messages() {
			got = append(got, m.ID)
		}
		if !slices.Equal(got, want) {
			for _, m := range daemon.Messages {
				if !slices.Contains(got, m.ID) {
					var kinds []string
					for _, b := range m.Content {
						kinds = append(kinds, b.Type)
					}
					t.Logf("not in the store: %s role=%s visibility=%q blocks=%v", m.ID, m.Role, m.Visibility, kinds)
				}
			}
			t.Fatalf("store transcript differs from the Daemon's")
		}
	})

	t.Run("methods", func(t *testing.T) {
		c := cl()
		must(c.UpdateSessionSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: id, ReasoningEffort: protocol.ReasoningEffortLow}))(t)
		defaults := must(c.GetDefaultSettings(ctx))(t)
		t.Logf("defaults: model %q, %d models", defaults.ModelID, len(defaults.AvailableModels))
		must(c.RenameSession(ctx, protocol.RenameSessionParams{SessionID: id, Title: "Go SDK live check"}))(t)
		listed := must(c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{Limit: ptr(10.0)}))(t)
		if len(listed.Sessions) == 0 || listed.Sessions[0].Title != "Go SDK live check" {
			t.Fatalf("sessions: %+v", listed.Sessions)
		}
		found := must(c.SearchSessions(ctx, protocol.SearchSessionsParams{Query: "pong"}))(t)
		t.Logf("search: %d sessions", len(found.Sessions))
		page := must(c.GetSessionMessages(ctx, protocol.GetSessionMessagesParams{SessionID: id, Limit: ptr(2.0)}))(t)
		if len(page.Messages) != 2 || !page.HasMore || page.NextCursor == "" {
			t.Fatalf("first page: %d hasMore=%v", len(page.Messages), page.HasMore)
		}
		older := must(c.GetSessionMessages(ctx, protocol.GetSessionMessagesParams{SessionID: id, Limit: ptr(2.0), Cursor: page.NextCursor}))(t)
		if len(older.Messages) == 0 || older.Messages[0].ID == page.Messages[0].ID {
			t.Fatalf("older page: %d", len(older.Messages))
		}
		bd := must(c.GetContextBreakdown(ctx, protocol.GetContextBreakdownParams{SessionID: id}))(t)
		t.Logf("context: %v of %v tokens", bd.UsedTokens, bd.ContextBudget)
		skills := must(c.ListSkills(ctx, protocol.ListSkillsParams{SessionID: id}))(t)
		cmds := must(c.ListCommands(ctx, protocol.ListCommandsParams{SessionID: id}))(t)
		mcp := must(c.ListMCPServers(ctx, protocol.ListMCPServersParams{SessionID: id}))(t)
		t.Logf("%d skills, %d commands, %d MCP servers", len(skills.Skills), len(cmds.Commands), len(mcp.Servers))
		diff := must(c.GetGitDiff(ctx, protocol.GetGitDiffParams{SessionID: id}))(t)
		v := must(diff.Value())(t)
		ok, isOK := v.(*protocol.DaemonGetGitDiffSuccessResult)
		if !isOK || ok.Data.Branch != "main" || !strings.Contains(ok.Data.UnstagedDiff+ok.Data.LocalDiff+ok.Data.Diff, "Now with docs.") {
			t.Fatalf("diff: %#v", v)
		}
		f := must(c.GetWorkspaceFileContent(ctx, protocol.GetWorkspaceFileContentParams{SessionID: id, FilePath: "README.md"}))(t)
		content := f.Content
		if f.Encoding == "base64" {
			b, _ := base64.StdEncoding.DecodeString(f.Content)
			content = string(b)
		}
		if !strings.Contains(content, "Now with docs.") {
			t.Fatalf("file: %+v", f)
		}
		if v := must(c.ValidateWorkingDirectory(ctx, protocol.ValidateWorkingDirectoryParams{WorkingDirectory: cwd}))(t); !v.IsValid {
			t.Fatal("workspace invalid")
		}
		if r := must(c.ArchiveSession(ctx, protocol.ArchiveSessionParams{SessionID: id, Force: ptr(true)}))(t); !r.Success {
			t.Fatal("not archived")
		}
		must(c.UnarchiveSession(ctx, protocol.UnarchiveSessionParams{SessionID: id}))(t)
		_, err := ctl.LoadSession(ctx, protocol.LoadSessionParams{SessionID: "00000000-0000-4000-8000-000000000000"})
		if !errors.Is(err, controller.ErrSessionNotFound) {
			t.Fatalf("unknown session: %v", err)
		}
	})

	t.Run("daemon restart", func(t *testing.T) {
		from := ev.len()
		d.restart()
		_, next := ev.wait(t, ctx, from, "session loaded again", func(e controller.Event) bool {
			l, ok := e.(controller.SessionLoaded)
			return ok && l.SessionID == id
		})
		t.Logf("reconnected and reloaded")
		if got := turn(t, "Reply with exactly the word pong2 and nothing else."); !strings.Contains(strings.ToLower(got), "pong2") {
			t.Fatalf("reply after restart %q", got)
		}
		_ = next
	})

	t.Run("compact", func(t *testing.T) {
		r := must(cl().CompactSession(ctx, protocol.CompactSessionParams{SessionID: id}))(t)
		t.Logf("compacted into %s, %v messages removed", r.NewSessionID, r.RemovedCount)
		if r.NewSessionID == "" {
			t.Fatal("no new session")
		}
	})
}
