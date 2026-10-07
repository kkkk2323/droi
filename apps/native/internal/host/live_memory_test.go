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

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
)

// TestLiveMemory is tests/live/memory.spec.ts against the native app: a
// real Daemon started with the Runtime Overlay, the app's own binary as the
// Memory Server and the hook. Only with FACTORY_API_KEY set; DROI_LIVE_MODEL
// picks the model and FACTORY_API_BASE_URL the proxy.
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
	var c *droid.Client
	for i := 0; i < 150 && c == nil; i++ {
		c, _ = droid.Dial(ctx, droid.Options{URL: fmt.Sprintf("ws://127.0.0.1:%d", port), Credential: &droid.Credential{APIKey: key}})
		if c == nil {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if c == nil {
		t.Fatal("the Daemon did not start")
	}
	defer c.Close()

	sawState := false
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
		// SessionEnd removes the hook's per-Session prompt count; look before closing.
		if st, _ := os.ReadDir(filepath.Join(memDir, "state")); len(st) > 0 {
			sawState = true
		}
		_, _ = c.CloseSession(ctx, protocol.CloseSessionParams{SessionID: init.SessionID})
		return text.String()
	}

	reply := ask(`From now on I want every answer to end with the word "otter". Remember this preference for future sessions, then reply "ok".`)
	t.Logf("reply: %q", reply)
	store, err := memory.OpenStore(memDir)
	if err != nil {
		t.Fatal(err)
	}
	global, _ := store.List(memory.GlobalSlot, "")
	project, _ := store.List(memory.ProjectSlot(workspace), "")
	var all []string
	for _, e := range append(global, project...) {
		all = append(all, e.Text)
	}
	if !regexp.MustCompile(`(?i)otter`).MatchString(strings.Join(all, "\n")) {
		t.Fatalf("the preference was not saved: %q", all)
	}
	if _, err := store.Add(memory.ProjectSlot(workspace), memory.CategoryCorrection, "The staging database is called stage-db, never prod."); err != nil {
		t.Fatal(err)
	}
	store.Close()

	quoted := ask("Without calling any tools, quote verbatim the <memory-context> block you were given when this session started.")
	if !strings.Contains(quoted, "stage-db") {
		t.Errorf("the SessionStart hook did not inject the correction: %q", quoted)
	}
	nudged := ask("不对。Without calling any tools, quote verbatim every <memory-context> block attached to this message of mine, not the one from the start of the session.")
	if !strings.Contains(nudged, "record the correction") {
		t.Errorf("no UserPromptSubmit nudge: %q", nudged)
	}
	if !sawState {
		t.Error("the hook kept no per-Session state")
	}

	// A Memory Session consolidates on the real Daemon and is archived.
	slot := memory.ProjectSlot(workspace)
	store, err = memory.OpenStore(memDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Tests run with pnpm test.", "Run the tests with `pnpm test`."} {
		if _, err := store.Add(slot, memory.CategoryConvention, text); err != nil {
			t.Fatal(err)
		}
	}
	prompt, err := memorywork.ReadPrompt(memDir, memorywork.PromptConsolidation)
	if err != nil {
		t.Fatal(err)
	}
	run := memorywork.NewRunner(func(ctx context.Context) (*droid.Client, error) {
		return droid.Dial(ctx, droid.Options{URL: fmt.Sprintf("ws://127.0.0.1:%d", port), Credential: &droid.Credential{APIKey: key}})
	})
	outcomes, err := memorywork.Consolidate(ctx, memorywork.Work{Store: store, Run: run, ModelID: model, Prompt: prompt, Home: fake}, slot)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("consolidation: %+v", outcomes)
	if len(outcomes) != 1 || !outcomes[0].OK {
		t.Errorf("the convention slice was not consolidated: %+v", outcomes)
	}
	yes := true
	list, err := c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{IncludeArchived: &yes})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list.Sessions {
		var v struct {
			Title      string
			Tags       []struct{ Name string }
			ArchivedAt any `json:"archivedAt"`
		}
		b, _ := json.Marshal(s)
		_ = json.Unmarshal(b, &v)
		for _, tag := range v.Tags {
			if tag.Name == "droi.memory" && strings.Contains(v.Title, "Memory: consolidate") && v.ArchivedAt != nil {
				found = true
			}
		}
	}
	if !found {
		b, _ := json.Marshal(list.Sessions)
		t.Errorf("no archived Memory Session: %.1500s", b)
	}
}
