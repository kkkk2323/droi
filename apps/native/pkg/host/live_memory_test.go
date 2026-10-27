package host_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

// TestLiveMemory runs Memory against a real Daemon started with the Runtime
// Overlay, the app's own binary as the Memory Server and the hook. Only with
// FACTORY_API_KEY set; DROI_LIVE_MODEL picks the model and
// FACTORY_API_BASE_URL the proxy.
func TestLiveMemory(t *testing.T) {
	key := os.Getenv("FACTORY_API_KEY")
	if key == "" {
		t.Skip("FACTORY_API_KEY is not set")
	}
	home, _ := os.UserHomeDir()
	droidPath := filepath.Join(home, ".local", "bin", "droid")
	if p, err := exec.LookPath("droid"); err == nil {
		droidPath = p
	}
	model := os.Getenv("DROI_LIVE_MODEL")
	if model == "" {
		model = "glm-5.3-flash"
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	bin := filepath.Join(root, "Droi")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/kkkk2323/droi/apps/native").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	fake, workspace, memDir := filepath.Join(root, "home"), filepath.Join(root, "workspace"), filepath.Join(root, "memory")
	_ = os.Mkdir(fake, 0o755)
	_ = os.Mkdir(workspace, 0o755)
	overlay := filepath.Join(root, "runtime-overlay.json")
	if err := host.WriteRuntimeOverlay(overlay, host.BuildRuntimeOverlay(host.MemoryAttachment{Executable: bin, MemoryDir: memDir, ApprovedAt: time.Now().UTC().Format(time.RFC3339)})); err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cmd := exec.Command(droidPath, host.DaemonArgs(port, []string{"--parent-pid", fmt.Sprint(os.Getpid())}, overlay)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HOME="+fake, "FACTORY_API_KEY="+key)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			_ = filepath.Walk(filepath.Join(fake, ".factory"), func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() || !strings.HasSuffix(p, ".log") {
					return nil
				}
				b, _ := os.ReadFile(p)
				for _, line := range strings.Split(string(b), "\n") {
					if strings.Contains(line, "droi-memory") || strings.Contains(strings.ToLower(line), "mcp") && strings.Contains(strings.ToLower(line), "fail") {
						t.Logf("%s: %.600s", filepath.Base(p), line)
					}
				}
				return nil
			})
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	dial := func(ctx context.Context) (*droid.Client, error) {
		return droid.Dial(ctx, droid.Options{URL: fmt.Sprintf("ws://127.0.0.1:%d", port), Credential: &droid.Credential{APIKey: key}})
	}
	var c *droid.Client
	for i := 0; i < 150 && c == nil; i++ {
		c, _ = dial(ctx)
		if c == nil {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if c == nil {
		t.Fatal("the Daemon did not start")
	}
	defer c.Close()

	ask := func(prompt string) string {
		t.Helper()
		// At Off the Daemon asks before every tool call, Memory's included; nobody answers here.
		init, err := c.InitializeSession(ctx, protocol.InitializeSessionParams{MachineID: "local", Cwd: workspace, ModelID: model, AutonomyLevel: protocol.AutonomyLevelLow})
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		done := make(chan struct{}, 1)
		unsub := c.Subscribe(func(n droid.Notification) {
			if n.Method != protocol.NotificationSessionNotification {
				return
			}
			var p protocol.SessionNotificationParams
			if json.Unmarshal(n.Params, &p) != nil || p.SessionID != init.SessionID {
				return
			}
			v, _ := p.Notification.Value()
			switch v := v.(type) {
			case *protocol.CreateMessageNotification:
				if v.Message.Role == protocol.MessageRoleAssistant {
					for _, b := range v.Message.Content {
						if tb, _ := b.Value(); tb != nil {
							if tb, ok := tb.(*protocol.TextBlock); ok {
								text.WriteString(tb.Text)
							}
						}
					}
				}
			case *protocol.AgentTurnCompletedNotification:
				select {
				case done <- struct{}{}:
				default:
				}
			}
		})
		defer unsub()
		if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: init.SessionID, Text: prompt}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("the turn did not end")
		}
		_, _ = c.CloseSession(ctx, protocol.CloseSessionParams{SessionID: init.SessionID})
		return text.String()
	}
	otter := regexp.MustCompile(`(?i)otter`)

	// Asked to remember, Droid writes a Memory Note.
	reply := ask(`From now on I want every answer to end with the word "otter". Remember this preference for future sessions, then reply "ok".`)
	t.Logf("reply: %q", reply)
	notes, _ := filepath.Glob(filepath.Join(memDir, memory.NotesDir, "*.md"))
	var noted []string
	for _, f := range notes {
		b, _ := os.ReadFile(f)
		noted = append(noted, string(b))
	}
	if !otter.MatchString(strings.Join(noted, "\n")) {
		t.Fatalf("the preference was not noted: %q", noted)
	}
	if started, _ := memory.ReadStarted(memDir); len(started) == 0 {
		t.Error("the SessionStart hook recorded no Session")
	}

	// The list dates a Session by its file, which gets every hook run the
	// Daemon records: opening the Session again must not move it.
	time.Sleep(2 * time.Second)
	listed, err := c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{})
	if err != nil || len(listed.Sessions) == 0 {
		t.Fatalf("no Session listed: %v", err)
	}
	id, before := listed.Sessions[0].SessionID, listed.Sessions[0].UpdatedAt
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if listed, err = c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{}); err != nil {
		t.Fatal(err)
	}
	for _, s := range listed.Sessions {
		if s.SessionID == id && s.UpdatedAt != before {
			t.Errorf("opening the Session moved its date from %v to %v", before, s.UpdatedAt)
		}
	}

	// A new Session starts with the Memory Summary and the Memory Notes it
	// has not taken in yet.
	summary := "v1\n\n## User Profile\n\nA developer.\n\n## User preferences\n\n- The staging database is called stage-db, never prod.\n\n## General Tips\n\n## What's in Memory\n"
	if reason := memory.CheckSummary(summary); reason != "" {
		t.Fatal(reason)
	}
	if err := os.WriteFile(filepath.Join(memDir, memory.SummaryFile), []byte(summary), 0o644); err != nil {
		t.Fatal(err)
	}
	quoted := ask("Without calling any tools, quote verbatim the <memory-context> block you were given when this session started.")
	if !strings.Contains(quoted, "stage-db") {
		t.Errorf("the SessionStart hook did not show the Memory Summary: %q", quoted)
	}
	if !otter.MatchString(quoted) {
		t.Errorf("the SessionStart hook did not show the new Memory Note: %q", quoted)
	}

	// A pass on the real Daemon summarizes those Sessions and folds the
	// summaries and the note into the Memory Summary, in archived Memory
	// Sessions.
	sessionPrompt, err := memorywork.ReadPrompt(memDir, memorywork.PromptSessionSummary)
	if err != nil {
		t.Fatal(err)
	}
	memoryPrompt, err := memorywork.ReadPrompt(memDir, memorywork.PromptMemorySummary)
	if err != nil {
		t.Fatal(err)
	}
	w := memorywork.Work{
		Dir:           memDir,
		Run:           memorywork.NewRunner(dial),
		Models:        memorywork.Models{SessionSummary: model, MemorySummary: model},
		SessionPrompt: sessionPrompt,
		MemoryPrompt:  memoryPrompt,
		Home:          fake,
		Log:           func(s string) { t.Log(s) },
	}
	if err := w.Pass(ctx, 0, true); err != nil {
		t.Fatal(err)
	}
	st := memory.LoadState(memDir)
	for _, r := range st.Runs {
		t.Logf("Memory Session %q on %s: %s", r.Title, r.Model, r.Error)
		if r.Error != "" {
			t.Errorf("%s failed: %s", r.Title, r.Error)
		}
	}
	if written, _ := filepath.Glob(filepath.Join(memDir, memory.SessionSummariesDir, "*.md")); len(written) == 0 {
		t.Error("no Session Summary was written")
	}
	got := memory.ReadSummary(memDir)
	t.Logf("Memory Summary:\n%s", got)
	if reason := memory.CheckSummary(got); reason != "" {
		t.Errorf("the Memory Summary is not valid: %s", reason)
	}
	if !otter.MatchString(got) {
		t.Error("the Memory Summary left out the Memory Note")
	}
	yes := true
	list, err := c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{IncludeArchived: &yes})
	if err != nil {
		t.Fatal(err)
	}
	archived := map[string]bool{}
	for _, s := range list.Sessions {
		var v struct {
			Title      string
			Tags       []struct{ Name string }
			ArchivedAt any `json:"archivedAt"`
		}
		b, _ := json.Marshal(s)
		_ = json.Unmarshal(b, &v)
		for _, tag := range v.Tags {
			if tag.Name == "droi.memory" && v.ArchivedAt != nil {
				switch {
				case strings.HasPrefix(v.Title, "Memory: summarize"):
					archived[memory.RunSessionSummary] = true
				case strings.HasPrefix(v.Title, "Memory: update the Memory Summary"):
					archived[memory.RunMemorySummary] = true
				}
			}
		}
	}
	if !archived[memory.RunSessionSummary] || !archived[memory.RunMemorySummary] {
		b, _ := json.Marshal(list.Sessions)
		t.Errorf("archived Memory Sessions %v: %.1500s", archived, b)
	}
}
