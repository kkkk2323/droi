package host

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// TestLiveAddedAccount starts a real Daemon as an added account and checks
// that it reads the droid CLI's AGENTS.md through the copy and writes its
// Session into the CLI's sessions. Only with FACTORY_API_KEY set, which
// the signed-out account runs on; DROI_LIVE_MODEL picks the model and
// FACTORY_API_BASE_URL the proxy.
func TestLiveAddedAccount(t *testing.T) {
	key := os.Getenv("FACTORY_API_KEY")
	if key == "" {
		t.Skip("FACTORY_API_KEY is not set")
	}
	model := os.Getenv("DROI_LIVE_MODEL")
	if model == "" {
		model = "glm-5.3-flash"
	}
	userHome, _ := os.UserHomeDir()
	droidPath := filepath.Join(userHome, ".local", "bin", "droid")
	if p, err := exec.LookPath("droid"); err == nil {
		droidPath = p
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home, data, workspace := filepath.Join(root, "home"), filepath.Join(root, "data"), filepath.Join(root, "workspace")
	shared := filepath.Join(home, ".factory")
	os.MkdirAll(shared, 0o700)
	os.MkdirAll(workspace, 0o755)
	os.WriteFile(filepath.Join(shared, "AGENTS.md"), []byte("The codeword is pelican-42.\n"), 0o600)
	h := New(Config{UserData: data, Home: home, Env: func(k string) string {
		switch k {
		case "FACTORY_API_KEY", "FACTORY_API_BASE_URL", "PATH":
			return os.Getenv(k)
		}
		return ""
	}})
	h.Settings.Update(func(s *Settings) {
		s.DroidPath = OptString(droidPath)
		s.Accounts = []string{"live"}
		s.ActiveAccount = "live"
	})
	h.Start()
	t.Cleanup(h.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	url, err := h.WaitForDaemonURL(ctx)
	if err != nil {
		t.Fatalf("%v: %s", err, ReadDaemonLogTail(h.DaemonLogPath()))
	}
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
	if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: init.SessionID, Text: "Without calling any tools, what is the codeword in your instructions? Reply with the codeword only."}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}
	if !strings.Contains(text.String(), "pelican-42") {
		t.Errorf("the copied AGENTS.md was not read: %q", text.String())
	}
	if FindSessionTranscript(filepath.Join(shared, "sessions"), init.SessionID) == "" {
		t.Error("the Session is not in the droid CLI's sessions")
	}
	if _, err := os.Stat(filepath.Join(shared, "auth.v2.file")); err == nil {
		t.Error("the account wrote into the droid CLI's folder")
	}
	entries, _ := os.ReadDir(h.homeOf("live"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Logf("reply %q; the account's .factory: %v", text.String(), names)
}
