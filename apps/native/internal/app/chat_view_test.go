package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge"
	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

var testRobot = fakewecom.Robot{BotID: "aib-test-robot", Secret: "s3cret"}

const chatWorkspace = "/Users/dev/acme-web"

// chatHarness is the window with this computer's Host and its Chat Bridge
// between a Fake WeCom and the harness's Fake Daemon, as hostrun starts
// it; edit sets the Chat Bridge's settings before it starts.
func chatHarness(t *testing.T, edit func(*host.ChatBridgeSettings)) (*harness, *host.Host, *fakewecom.WeCom) {
	t.Helper()
	h0 := testHost(t)
	_ = h0.Settings.Update(func(s *host.Settings) {
		s.APIKey = host.OptString("fk-test")
		if edit != nil {
			edit(&s.ChatBridge)
		}
	})
	sc := oneSession()
	sc.ValidDirectories = []string{chatWorkspace}
	sc.Turn = &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}
	h := newHarnessWith(t, sc, "", func(cfg *Config) { cfg.Host = h0 })
	w := fakewecom.Start(t, testRobot)
	b := chatbridge.New(chatbridge.Options{
		Settings: h0.Settings,
		Daemon: controller.Config{
			URL:        h.d.URL,
			Credential: func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		},
		WebSocketURL:   w.URL,
		ReconnectDelay: 20 * time.Millisecond,
		OnChange:       h0.Changed,
		Log:            func(s string) { t.Log(s) },
	})
	t.Cleanup(b.Stop)
	h0.OnChange(b.Follow)
	b.Follow()
	h.a.cfg.ChatBridge = b
	return h, h0, w
}

// fill types text into the field labeled label, after what it holds.
func (h *harness) fill(label, text string) {
	h.t.Helper()
	h.click(label)
	h.tt.Type(text)
	h.frame()
}

// clear empties the field labeled label of the n characters it holds.
func (h *harness) clear(label string, n int) {
	h.t.Helper()
	h.click(label)
	h.tt.Key(0, ui.KeyEnd)
	for range n {
		h.tt.Key(0, ui.KeyBackspace)
	}
	h.frame()
}

func TestFillingInTheChatBridgePageConnectsTheRobot(t *testing.T) {
	h, h0, w := chatHarness(t, nil)
	h.openSettingsTab("chat")
	if !h.hasText("Off") {
		t.Fatalf("no Off state: %q", h.tt.Texts())
	}
	h.fill("WeCom Bot ID", testRobot.BotID)
	h.fill("WeCom Secret", testRobot.Secret)
	h.fill("Chat Workspace folder", chatWorkspace)
	h.click("Save robot")
	// The Daemon restarts so that the Runtime Overlay pre-approves droi-chat.
	h.until("the question", func() bool { return h.hasText("The Daemon restarts") })
	if h0.Settings.Get().ChatBridge.On() {
		t.Fatal("saved before the restart was confirmed")
	}
	h.click("Restart Daemon")
	w.WaitForSubscription(1)
	h.until("connected", func() bool { return h.hasText("Connected") })
	cb := h0.Settings.Get().ChatBridge
	if cb.BotID != testRobot.BotID || cb.Secret != testRobot.Secret || cb.Workspace != chatWorkspace {
		t.Errorf("saved %+v", cb)
	}
	if h.hasText(testRobot.Secret) {
		t.Error("the page shows the Secret")
	}
}

func TestAWrongSecretShowsWhatWeComAnswered(t *testing.T) {
	h, h0, _ := chatHarness(t, func(cb *host.ChatBridgeSettings) {
		cb.BotID, cb.Secret, cb.Workspace = testRobot.BotID, "wrong", chatWorkspace
	})
	h.openSettingsTab("chat")
	h.until("the refusal", func() bool { return h.hasText("invalid bot_id or secret") })
	if !h.hasText("Not connected") || !h.hasText("853000") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	// The robot was already on, so a new Secret needs no restart of the
	// Daemon.
	h.fill("WeCom Secret", testRobot.Secret)
	h.click("Save robot")
	h.until("connected", func() bool { return h.hasText("Connected") && !h.hasText("Not connected") })
	if h0.Settings.Get().ChatBridge.Secret != testRobot.Secret {
		t.Error("the new Secret was not saved")
	}
}

func TestAllowingARefusedSenderLetsTheirNextMessageStartAChatSession(t *testing.T) {
	h, h0, w := chatHarness(t, func(cb *host.ChatBridgeSettings) {
		cb.BotID, cb.Secret, cb.Workspace = testRobot.BotID, testRobot.Secret, chatWorkspace
		cb.AllowedUsers = []string{"HuangJunXiang"}
	})
	h.openSettingsTab("chat")
	w.WaitForSubscription(1)
	w.SendDirectMessage(fakewecom.DirectMessage{From: "wo-encrypted-7f3a", Text: "Hi"})
	h.until("the refused sender", func() bool { return h.hasButton("Allow wo-encrypted-7f3a") })
	h.tt.Scroll(refW/2, refH/2, 0, 2000)
	h.settle()
	h.click("Allow wo-encrypted-7f3a")
	cb := h0.Settings.Get().ChatBridge
	if !slices.Contains(cb.AllowedUsers, "wo-encrypted-7f3a") || slices.Contains(cb.RefusedUsers, "wo-encrypted-7f3a") {
		t.Fatalf("allowed %v, refused %v", cb.AllowedUsers, cb.RefusedUsers)
	}
	h.frame()
	if h.hasButton("Allow wo-encrypted-7f3a") || !h.hasText("wo-encrypted-7f3a") {
		t.Errorf("the sender did not move to the allowlist: %q", h.tt.Texts())
	}

	w.SendDirectMessage(fakewecom.DirectMessage{From: "wo-encrypted-7f3a", Text: "Hi again"})
	var chat []string
	h.until("a Chat Session", func() bool {
		chat = nil
		for _, params := range initializeRequests(t, h) {
			if strings.Contains(params, sessions.ChatTag) {
				chat = append(chat, params)
			}
		}
		return len(chat) > 0
	})
	if len(chat) != 1 || !strings.Contains(chat[0], "wo-encrypted-7f3a") {
		t.Errorf("Chat Sessions %v", chat)
	}
}

func TestTheAllowlistIsEditedOnThePage(t *testing.T) {
	h, h0, _ := chatHarness(t, func(cb *host.ChatBridgeSettings) { cb.AllowedUsers = []string{"HuangJunXiang"} })
	h.openSettingsTab("chat")
	h.fill("WeCom user id", "LiLei")
	h.click("Add user")
	if got := h0.Settings.Get().ChatBridge.AllowedUsers; !slices.Equal(got, []string{"HuangJunXiang", "LiLei"}) {
		t.Fatalf("allowlist %v", got)
	}
	h.click("Remove HuangJunXiang")
	if got := h0.Settings.Get().ChatBridge.AllowedUsers; !slices.Equal(got, []string{"LiLei"}) {
		t.Fatalf("allowlist %v", got)
	}
}

func TestClearingTheBotIDTurnsTheChatBridgeOff(t *testing.T) {
	h, h0, w := chatHarness(t, func(cb *host.ChatBridgeSettings) {
		cb.BotID, cb.Secret, cb.Workspace = testRobot.BotID, testRobot.Secret, chatWorkspace
	})
	h.openSettingsTab("chat")
	h.until("connected", func() bool { return h.hasText("Connected") })
	h.clear("WeCom Bot ID", len(testRobot.BotID))
	h.click("Save robot")
	w.WaitForDisconnect()
	if h0.Settings.Get().ChatBridge.On() {
		t.Fatal("the robot is still set")
	}
	h.until("off", func() bool { return h.hasText("Off") && !h.hasText("Connected") })
}

func TestChatSessionsCarryAMarkAndOtherSessionsDoNot(t *testing.T) {
	chat := map[string]any{"tags": []map[string]any{{"name": sessions.ChatTag, "metadata": map[string]string{"kind": "direct", "userId": "HuangJunXiang"}}}}
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Asked from WeCom", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "from chat"}}, Extra: chat},
		{Title: "My own", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "mine"}}},
	}}, "")
	h.until("the Session list", func() bool { return h.hasText("Asked from WeCom") && h.hasText("My own") })
	marks := func() int {
		n := 0
		for _, s := range h.tt.Texts() {
			if s == "Chat Session" {
				n++
			}
		}
		return n
	}
	if n := marks(); n != 1 {
		t.Fatalf("%d marks in the sidebar for one Chat Session", n)
	}
	mark, _ := h.tt.Find("Chat Session")
	row, _ := h.tt.Find("Asked from WeCom")
	if mark.Y+mark.H < row.Y || mark.Y > row.Y+row.H {
		t.Errorf("the mark %+v is not on the Chat Session's row %+v", mark, row)
	}

	h.openSession("Asked from WeCom")
	h.until("its messages", func() bool { return h.hasText("from chat") })
	if n := marks(); n != 2 {
		t.Errorf("%d marks with the Chat Session open; the header has none", n)
	}
	h.openSession("My own")
	h.until("its messages", func() bool { return h.hasText("mine") })
	if n := marks(); n != 1 {
		t.Errorf("%d marks with another Session open", n)
	}
}
