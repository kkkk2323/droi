package droid_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// Live tests run against a real `droid daemon`, only when FACTORY_API_KEY is
// set. They catch protocol drift that the Fake Daemon cannot:
//
//	FACTORY_API_KEY=fk-... go test -run Live -v .
//
// FACTORY_API_BASE_URL routes the Daemon's traffic through a proxy;
// DROID_LIVE_MODEL picks the model (glm-5.3-flash by default);
// DROID_LIVE_RECORD=dir writes every frame there, credentials redacted, for
// replay tests. The Daemon runs in a throwaway HOME.

func liveDaemon(t *testing.T) string { return startLive(t).URL }

// live is a running `droid daemon` in a throwaway HOME.
type live struct {
	URL  string
	t    *testing.T
	path string
	home string
	port int
	cmd  *exec.Cmd
}

func startLive(t *testing.T) *live {
	t.Helper()
	key := os.Getenv("FACTORY_API_KEY")
	if key == "" {
		t.Skip("FACTORY_API_KEY is not set")
	}
	droidPath, err := exec.LookPath("droid")
	if err != nil {
		home, _ := os.UserHomeDir()
		droidPath = filepath.Join(home, ".local", "bin", "droid")
		if _, err := os.Stat(droidPath); err != nil {
			t.Skip("no droid on PATH or in ~/.local/bin")
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	d := &live{URL: fmt.Sprintf("ws://127.0.0.1:%d", port), t: t, path: droidPath, home: t.TempDir(), port: port}
	d.start()
	t.Cleanup(func() {
		d.stop()
		if t.Failed() {
			b, _ := os.ReadFile(filepath.Join(d.home, "daemon.log"))
			t.Logf("daemon log:\n%s", tail(string(b), 4000))
		}
	})
	return d
}

func (d *live) start() {
	d.t.Helper()
	d.cmd = exec.Command(d.path, "daemon", "--host", "127.0.0.1", "--port", fmt.Sprint(d.port), "--parent-pid", fmt.Sprint(os.Getpid()))
	d.cmd.Env = append(os.Environ(), "HOME="+d.home)
	log, _ := os.OpenFile(filepath.Join(d.home, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	d.cmd.Stdout, d.cmd.Stderr = log, log
	if err := d.cmd.Start(); err != nil {
		d.t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", d.port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		if time.Now().After(deadline) {
			d.t.Fatal("daemon did not become healthy")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (d *live) stop() {
	d.cmd.Process.Kill()
	d.cmd.Wait()
}

// restart kills the Daemon and starts another on the same port and HOME,
// as a crash and respawn would.
func (d *live) restart() {
	d.t.Helper()
	d.stop()
	d.start()
}

func newSessionID() string { return uuid.NewString() }

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

var secret = regexp.MustCompile(`("(?:apiKey|token|accessToken|refreshToken|userId|orgId)"\s*:\s*")[^"]*"`)

// recorder writes frames as JSON lines: {"dir":"sent","t":ms,"frame":{...}}.
func recorder(t *testing.T, name string) func(droid.Direction, []byte) {
	dir := os.Getenv("DROID_LIVE_RECORD")
	if dir == "" {
		return nil
	}
	os.MkdirAll(dir, 0o755)
	f, err := os.Create(filepath.Join(dir, name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	var mu sync.Mutex
	start := time.Now()
	return func(d droid.Direction, b []byte) {
		clean := secret.ReplaceAll(b, []byte(`${1}REDACTED"`))
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(f, `{"dir":%q,"t":%d,"frame":%s}`+"\n", d, time.Since(start).Milliseconds(), clean)
	}
}

func TestLiveReply(t *testing.T) {
	url := liveDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c, err := droid.Dial(ctx, droid.Options{URL: url, Credential: &droid.Credential{APIKey: os.Getenv("FACTORY_API_KEY")}, Record: recorder(t, "reply")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	models, err := c.ListModels(ctx, protocol.ListModelsParams{})
	if err != nil {
		t.Fatal("list_models:", err)
	}
	t.Logf("%d models", len(models.Models))
	model := os.Getenv("DROID_LIVE_MODEL")
	if model == "" {
		model = "glm-5.3-flash"
	}

	var mu sync.Mutex
	var text strings.Builder
	var kinds []string
	idle := make(chan struct{}, 1)
	working := false
	var sessionID string
	c.Subscribe(func(n droid.Notification) {
		if n.Method != protocol.NotificationSessionNotification {
			return
		}
		var p protocol.SessionNotificationParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Errorf("decode session notification: %v", err)
			return
		}
		v, err := p.Notification.Value()
		if err != nil {
			t.Errorf("decode %s: %v", p.Notification.Type, err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if p.SessionID != sessionID {
			return
		}
		kinds = append(kinds, p.Notification.Type)
		switch v := v.(type) {
		case nil:
			t.Logf("unknown notification type %q", p.Notification.Type)
		case *protocol.AssistantTextDeltaNotification:
			text.WriteString(v.TextDelta)
		case *protocol.DroidWorkingStateChangedNotification:
			if v.NewState != protocol.DroidWorkingStateIdle {
				working = true
			} else if working {
				select {
				case idle <- struct{}{}:
				default:
				}
			}
		}
	})

	cwd := t.TempDir()
	mu.Lock()
	sessionID = newSessionID()
	mu.Unlock()
	init, err := c.InitializeSession(ctx, protocol.InitializeSessionParams{
		SessionID: sessionID,
		MachineID: "local",
		Cwd:       cwd,
		ModelID:   model,
		Token:     os.Getenv("FACTORY_API_KEY"),
	})
	if err != nil {
		t.Fatal("initialize_session:", err)
	}
	t.Logf("session %s, model %s", init.SessionID, init.Settings.ModelID)
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: sessionID, Text: "Reply with exactly the word pong and nothing else."}); err != nil {
		t.Fatal("add_user_message:", err)
	}
	select {
	case <-idle:
	case <-ctx.Done():
		t.Fatal("turn did not end")
	}
	mu.Lock()
	got, seen := text.String(), strings.Join(kinds, " ")
	mu.Unlock()
	t.Logf("notifications: %s", seen)
	if !strings.Contains(strings.ToLower(got), "pong") {
		t.Fatalf("reply %q", got)
	}

	msgs, err := c.GetSessionMessages(ctx, protocol.GetSessionMessagesParams{SessionID: sessionID})
	if err != nil {
		t.Fatal("get_session_messages:", err)
	}
	if len(msgs.Messages) < 2 {
		t.Fatalf("%d messages after the turn", len(msgs.Messages))
	}
	if _, err := c.CloseSession(ctx, protocol.CloseSessionParams{SessionID: sessionID}); err != nil {
		t.Fatal("close_session:", err)
	}
}

func TestLivePermission(t *testing.T) {
	url := liveDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	key := os.Getenv("FACTORY_API_KEY")
	c, err := droid.Dial(ctx, droid.Options{URL: url, Credential: &droid.Credential{APIKey: key}, Record: recorder(t, "permission")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	model := os.Getenv("DROID_LIVE_MODEL")
	if model == "" {
		model = "glm-5.3-flash"
	}

	asked := make(chan protocol.RequestPermissionParams, 1)
	c.Handle(protocol.ServerRequestRequestPermission, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p protocol.RequestPermissionParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		select {
		case asked <- p:
		default:
		}
		return protocol.RequestPermissionResponse{SelectedOption: protocol.ToolConfirmationOutcomeProceedOnce, SessionID: p.SessionID}, nil
	})
	idle := make(chan struct{}, 1)
	sessionID := newSessionID()
	var mu sync.Mutex
	working := false
	c.Subscribe(func(n droid.Notification) {
		var p protocol.SessionNotificationParams
		if n.Method != protocol.NotificationSessionNotification || json.Unmarshal(n.Params, &p) != nil || p.SessionID != sessionID {
			return
		}
		v, _ := p.Notification.Value()
		if s, ok := v.(*protocol.DroidWorkingStateChangedNotification); ok {
			mu.Lock()
			defer mu.Unlock()
			if s.NewState != protocol.DroidWorkingStateIdle {
				working = true
			} else if working {
				select {
				case idle <- struct{}{}:
				default:
				}
			}
		}
	})
	cwd := t.TempDir()
	if _, err := c.InitializeSession(ctx, protocol.InitializeSessionParams{
		SessionID: sessionID, MachineID: "local", Cwd: cwd, ModelID: model, Token: key,
		AutonomyLevel: protocol.AutonomyLevelOff,
	}); err != nil {
		t.Fatal("initialize_session:", err)
	}
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: sessionID,
		Text: "Use the Execute tool to run exactly this shell command: echo droid-sdk-go > out.txt . Then reply done."}); err != nil {
		t.Fatal("add_user_message:", err)
	}
	select {
	case p := <-asked:
		t.Logf("asked for %d tool use(s), %d options", len(p.ToolUses), len(p.Options))
	case <-ctx.Done():
		t.Fatal("no permission request")
	}
	select {
	case <-idle:
	case <-ctx.Done():
		t.Fatal("turn did not end")
	}
	b, err := os.ReadFile(filepath.Join(cwd, "out.txt"))
	if err != nil || strings.TrimSpace(string(b)) != "droid-sdk-go" {
		t.Fatalf("out.txt: %q %v", b, err)
	}
}
