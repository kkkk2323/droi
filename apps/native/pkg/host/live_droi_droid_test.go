package host

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// TestLiveDroisDroid turns the Key Pool on as Settings does and runs one
// turn on a real Daemon: Droi prepares Factory's newest droid (verifying the
// hook against a local receiver), starts the Daemon with the hook and checks
// it in, and the turn's LLM requests go out with the pool's key, not the
// login. Only with FACTORY_API_KEY (the login) and DROI_LIVE_POOL_KEY (a key
// of another account) set; DROI_LIVE_MODEL picks the model.
func TestLiveDroisDroid(t *testing.T) {
	login, poolKey := os.Getenv("FACTORY_API_KEY"), os.Getenv("DROI_LIVE_POOL_KEY")
	if login == "" || poolKey == "" {
		t.Skip("FACTORY_API_KEY and DROI_LIVE_POOL_KEY are not set")
	}
	model := os.Getenv("DROI_LIVE_MODEL")
	if model == "" {
		model = "glm-5.3-flash"
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home, data, workspace := filepath.Join(root, "home"), filepath.Join(root, "Application Support", "Droi"), filepath.Join(root, "workspace")
	os.MkdirAll(home, 0o700)
	os.MkdirAll(workspace, 0o755)
	h := New(Config{UserData: data, Home: home, FactoryRoot: home, Env: func(k string) string {
		if k == "PATH" {
			return os.Getenv(k)
		}
		return ""
	}})
	// The proxy's logs stay open until the Key Pool stops, and Windows keeps
	// an open file from being removed with the temporary folder.
	t.Cleanup(func() {
		h.Stop()
		h.KeyPool().Stop()
	})
	h.Settings.Update(func(s *Settings) { s.APIKey = OptString(login) })
	if err := h.AddPoolKey(context.Background(), poolKey); err != nil {
		t.Fatal(err)
	}
	// A download of droid takes its own time.
	h.prepareDroiDroid(false)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	st := h.DroiDroidState()
	if !st.Enabled || st.Err != "" || st.Version == "" {
		t.Fatalf("Droi's droid: %+v", st)
	}
	t.Logf("Droi's droid %s at %s", st.Version, h.DroidPath())
	h.Start()
	url, err := h.WaitForDaemonURL(ctx)
	if err != nil {
		t.Fatalf("%v: %s", err, ReadDaemonLogTail(h.DaemonLogPath()))
	}
	pid := h.Daemon.State().PID
	cred, _ := h.Credential(ctx)
	c, err := droid.Dial(ctx, droid.Options{URL: url, Credential: cred})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	init, err := c.InitializeSession(ctx, protocol.InitializeSessionParams{MachineID: "local", Cwd: workspace, ModelID: model, AutonomyLevel: protocol.AutonomyLevelLow})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	done := make(chan struct{}, 1)
	unsub := c.Subscribe(func(n droid.Notification) {
		var p protocol.SessionNotificationParams
		if n.Method != protocol.NotificationSessionNotification || json.Unmarshal(n.Params, &p) != nil || p.SessionID != init.SessionID {
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
	// As the window does: droid 1.0 takes a message for a loaded Session.
	if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: init.SessionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: init.SessionID, Text: "Without calling any tools, reply with the word pelican and nothing else."}); err != nil {
		t.Fatalf("%v; Daemon %+v (was %d): %s", err, h.Daemon.State(), pid, ReadDaemonLogTail(h.DaemonLogPath()))
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("the turn did not end: %s", ReadDaemonLogTail(h.DaemonLogPath()))
	}
	if !strings.Contains(strings.ToLower(text.String()), "pelican") {
		t.Errorf("reply %q", text.String())
	}
	if got := h.Daemon.State().PID; got != pid {
		t.Errorf("the Daemon restarted: %d, then %d", pid, got)
	}
	// The proxy logs every LLM request with the key it carried.
	time.Sleep(500 * time.Millisecond)
	f, err := os.Open(h.KeyPool().RequestLog())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ok, other := 0, 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r struct {
			Key    string `json:"key"`
			Status int    `json:"status"`
			Model  string `json:"model"`
		}
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if r.Key == keys.MaskKey(poolKey) && r.Status == 200 {
			ok++
		} else {
			other++
			t.Logf("request %s with %s: %d", r.Model, r.Key, r.Status)
		}
	}
	for _, e := range h.KeyPool().State().Events {
		if e.Kind == "llm_blocked" {
			t.Errorf("the proxy stopped a request: %+v", e)
		}
	}
	if ok == 0 || other > 0 {
		t.Fatalf("LLM requests with the pool's key: %d, others: %d", ok, other)
	}
	t.Logf("reply %q; %d LLM requests with the pool's key; Daemon %d", text.String(), ok, pid)
}
