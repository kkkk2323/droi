package chatbridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

// TestLiveChatBridge runs the Chat Bridge against a real WeCom robot and a
// real Daemon, and waits for people: an @ of the robot in a post of a WeCom
// Channel and a direct message to it, each asking for something that needs
// a permission request, answered in WeCom (a number in the post, a button
// on the card). Nothing is injected: every trigger and every answer comes
// from WeCom. Only with WECOM_BOT_ID, WECOM_SECRET and FACTORY_API_KEY set;
// see the native app's README for the rest of its environment.
func TestLiveChatBridge(t *testing.T) {
	botID, secret, key := os.Getenv("WECOM_BOT_ID"), os.Getenv("WECOM_SECRET"), os.Getenv("FACTORY_API_KEY")
	if botID == "" || secret == "" || key == "" {
		t.Skip("WECOM_BOT_ID, WECOM_SECRET and FACTORY_API_KEY are not all set")
	}
	wait := 10 * time.Minute
	if d, err := time.ParseDuration(os.Getenv("DROI_LIVE_WAIT")); err == nil && d > 0 {
		wait = d
	}
	model := envOr("DROI_LIVE_MODEL", "glm-5.3-flash")
	channelName := envOr("WECOM_CHANNEL", "定时任务")

	root, _ := filepath.EvalSymlinks(t.TempDir())
	fake, ws := filepath.Join(root, "home"), filepath.Join(root, "workspace")
	_ = os.Mkdir(fake, 0o755)
	_ = os.Mkdir(ws, 0o755)
	daemonURL := startLiveDaemon(t, root, fake, key)

	st := host.OpenSettings(filepath.Join(root, "settings.json"), func(string) string { return "" })
	var allowed []string
	for _, u := range strings.Split(os.Getenv("WECOM_ALLOWED_USERS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			allowed = append(allowed, u)
		}
	}
	_ = st.Update(func(s *host.Settings) {
		s.ChatBridge = host.ChatBridgeSettings{BotID: botID, Secret: secret, Workspace: ws, AllowedUsers: allowed, Model: model}
	})
	w := &liveWatch{t: t, resolved: map[string]protocol.ToolConfirmationOutcome{}, idle: map[string]int{}}
	b := New(Options{
		Settings: st,
		Daemon: controller.Config{
			URL:        daemonURL,
			Credential: func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: key}, nil },
			Record:     w.record,
		},
		Log: w.log,
	})
	t.Cleanup(b.Stop)
	b.ctl.Subscribe(w.event)
	b.Follow()
	waitFor(t, time.Minute, "the Chat Bridge to connect to WeCom", func() bool {
		if s := b.Status(); s.State == NotConnected {
			t.Fatalf("not connected to WeCom: %s", s.Reason)
		}
		return b.Status().State == Connected
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	b.mu.Lock()
	api := b.api
	b.mu.Unlock()
	topic := findChannel(ctx, t, api, channelName)
	if len(allowed) == 0 {
		// The Channel's creator is the one person sure to be there.
		_ = st.Update(func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{topic.creator} })
		t.Log("the allowlist is the Channel's creator; set WECOM_ALLOWED_USERS to allow others")
	}

	// A post.
	t.Logf("STEP 1: @ the robot in a new post in the WeCom Channel %q and ask for something that needs a permission request, such as running a shell command", channelName)
	post := w.thread(t, b, wait, "an @ of the robot in a post in "+channelName, func(th *thread) bool {
		return th.kind == threadKindChannel && th.topicID == topic.id
	})
	perm := w.permission(t, wait, post.sessionID)
	t.Logf("STEP 2: Droid asks to run %s; answer in the post with an @ of the robot and the number of an option, such as \"@robot 1\"", toolNames(&perm))
	turns := w.turns(post.sessionID)
	w.answered(t, wait, perm.RequestID)
	waitFor(t, wait, "the post's Chat Session to end its turn", func() bool { return w.turns(post.sessionID) > turns })
	var own []content
	waitFor(t, time.Minute, "the robot's replies under the post", func() bool {
		own = robotReplies(ctx, t, api, botID, post)
		return slices.ContainsFunc(own, func(c content) bool { return strings.Contains(c.Content, "Droid asks to run ") }) &&
			slices.ContainsFunc(own, func(c content) bool { return strings.Contains(c.Content, " answered the permission request to run ") })
	})
	if n := w.toolResults(post.sessionID, "Replied under the post in the WeCom Channel"); n == 0 {
		t.Error("Droid did not reply under the post")
	} else {
		t.Logf("Droid replied under the post %d times; the robot's replies there:", n)
		for _, c := range own {
			t.Logf("  %.300q", c.Content)
		}
	}

	// A direct message.
	t.Log("STEP 3: send the robot a direct message asking for something that needs a permission request, such as running a shell command")
	dm := w.thread(t, b, wait, "a direct message to the robot", func(th *thread) bool { return th.kind == threadKindDirect })
	perm = w.permission(t, wait, dm.sessionID)
	t.Logf("STEP 4: Droid asks to run %s; click a button on the card in the direct message", toolNames(&perm))
	turns = w.turns(dm.sessionID)
	w.answered(t, wait, perm.RequestID)
	b.mu.Lock()
	var card *prompt
	for _, p := range b.prompts.cards {
		if p.requestID == perm.RequestID {
			card = p
		}
	}
	if card == nil || card.by == "" {
		t.Error("the permission request was not answered with the card")
	}
	b.mu.Unlock()
	waitFor(t, wait, "the direct message's Chat Session to end its turn", func() bool { return w.turns(dm.sessionID) > turns })
	if w.logged("cannot update a card in WeCom") {
		t.Error("WeCom did not take the card's update")
	}
	if n := w.toolResults(dm.sessionID, "in WeCom"); n == 0 {
		t.Error("Droid did not reply in the direct message")
	}

	for _, p := range w.allPermissions() {
		if asksForChatTool(p) {
			t.Errorf("a %s call asked for permission: %s", host.ChatServerName, toolDetails(&p))
		}
	}
	if data, _ := os.ReadFile(filepath.Join(fake, ".factory", "logs", "droid-log-single.log")); bytes.Contains(data, []byte("Failed to load runtime settings overlay")) {
		t.Error("the Daemon did not take the Runtime Overlay, so droi-chat is not pre-approved")
	}
	if w.toolResults("", "not sent: ") > 0 {
		t.Error("a reply was not sent; see the tool errors above")
	}

	t.Run("Probes", func(t *testing.T) { liveProbes(ctx, t, b, api, post, dm, ws) })
}

// liveProbes record what WeCom's docs leave open, for the notes; they fail
// nothing but a broken download.
func liveProbes(ctx context.Context, t *testing.T, b *Bridge, api *channelAPI, post, dm *thread, ws string) {
	err := api.post(ctx, "/topic/get_topic_list", "droi-live-invalid-token", map[string]any{"type": 1, "limit": 1}, nil)
	t.Logf("an invalid access_token: %v", err)

	for _, step := range []struct {
		put  bool
		what string
	}{{true, "put"}, {true, "put again"}, {false, "removed"}, {false, "removed again"}} {
		err := api.postEmoji(ctx, post.topicID, post.postID, "", 3, step.put)
		t.Logf("emoji 3 %s on the post: %v; it now has %d of it", step.what, err, emojiCount(ctx, api, post, 3))
	}

	bin := make([]byte, 600<<10)
	_, _ = rand.Read(bin)
	var pic bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(1, 1, color.Black)
	_ = png.Encode(&pic, img)
	_ = os.WriteFile(filepath.Join(ws, "live-probe.bin"), bin, 0o644)
	_ = os.WriteFile(filepath.Join(ws, "live-probe.png"), pic.Bytes(), 0o644)
	out, err := b.reply(ctx, post, "Droi live test: a file of two chunks and an image (please ignore).", []string{"live-probe.bin", "live-probe.png"}, nil)
	t.Logf("a Channel reply with a 600 KB file and an image: %q %v", out, err)
	if m := regexp.MustCompile(`\(reply ([^)]+)\)`).FindStringSubmatch(out); m != nil {
		c, err := api.getReply(ctx, post.topicID, post.postID, m[1])
		if err != nil {
			t.Errorf("cannot read the reply with the files: %v", err)
		} else {
			t.Logf("the reply as read back: %d file(s), %d image(s), content %q", len(c.Files), len(c.Images), c.Content)
			for _, f := range c.Files {
				_, h, err := fetch(ctx, f.URL, "", "")
				t.Logf("the file without its cookie: %v", err)
				_, h, err = fetch(ctx, f.URL, f.CookieName, f.CookieValue)
				if err == nil {
					t.Logf("the file with its cookie: Content-Type %q, Content-Disposition %q", h.Get("Content-Type"), h.Get("Content-Disposition"))
				}
				name, data, err := b.channelAttachment(ctx, post, post.postID+":"+m[1]+":file:"+f.Placeholder)
				if err != nil || !bytes.Equal(data, bin) {
					t.Errorf("download_attachment of the file: %q, %d bytes, same bytes %v, %v", name, len(data), bytes.Equal(data, bin), err)
				} else {
					t.Logf("download_attachment of the file: %q, the same %d bytes", name, len(data))
				}
			}
			if len(c.Images) > 0 {
				name, data, err := b.channelAttachment(ctx, post, post.postID+":"+m[1]+":image:1")
				t.Logf("download_attachment of the image: %q, %d bytes, same bytes %v, %v", name, len(data), bytes.Equal(data, pic.Bytes()), err)
			}
		}
	}

	for i := range 6 {
		out, err := b.searchChannel(ctx, post, []string{"Droi", "live"}, "")
		t.Logf("search_channel for Droi live (try %d): %.600q %v", i+1, out, err)
		if err != nil || strings.Contains(out, "Matches for") {
			break
		}
		time.Sleep(10 * time.Second)
	}

	b.mu.Lock()
	wc, reqID := b.wecom, dm.reqID
	var card map[string]any
	for _, p := range b.prompts.cards {
		if p.th == dm && p.settled {
			card = settledCard(p)
		}
	}
	b.mu.Unlock()
	out, err = b.reply(ctx, dm, "Droi live test: a file (please ignore).", []string{"live-probe.bin"}, nil)
	t.Logf("a direct message with a 600 KB file, on the message's req_id: %q %v", out, err)
	if wc != nil {
		err := b.sendDirect(ctx, directThread(dm.userID), wc, markdown("Droi live test: a message sent with aibot_send_msg (please ignore)."))
		t.Logf("aibot_send_msg with chat_type 1: %v", err)
		if card != nil {
			_, err := wc.call(ctx, "aibot_respond_update_msg", reqID, map[string]any{"response_type": "update_template_card", "template_card": card})
			t.Logf("a card update on the message's req_id, not a click's: %v", err)
		}
	}
}

// asksForChatTool reports whether a permission request is for a droi-chat
// tool, called directly or from a Script.
func asksForChatTool(p controller.Permission) bool {
	isChat := func(name string) bool {
		return strings.HasPrefix(name, host.ChatServerName) || slices.ContainsFunc(toolList, func(t tool) bool { return t.Name == name })
	}
	for _, u := range p.ToolUses {
		if isChat(u.ToolUse.Name) {
			return true
		}
		if sp := transcript.ParseScriptPermission(u.Details.Raw); sp != nil && slices.ContainsFunc(sp.Calls, func(c transcript.ScriptPermissionCall) bool { return isChat(c.Tool) }) {
			return true
		}
	}
	return false
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// startLiveDaemon starts droid's Daemon in a throwaway HOME with the
// Runtime Overlay that pre-approves droi-chat, as the Host does when the
// Chat Bridge is on, and answers its URL.
func startLiveDaemon(t *testing.T, root, home, key string) string {
	t.Helper()
	me, _ := os.UserHomeDir()
	droidPath := filepath.Join(me, ".local", "bin", "droid")
	if p, err := exec.LookPath("droid"); err == nil {
		droidPath = p
	}
	overlay := filepath.Join(root, "runtime-overlay.json")
	if err := host.WriteRuntimeOverlay(overlay, host.ApproveChatServer(map[string]any{}, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))); err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cmd := exec.Command(droidPath, host.DaemonArgs(port, []string{"--parent-pid", fmt.Sprint(os.Getpid())}, overlay)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HOME="+home, "FACTORY_API_KEY="+key)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if !t.Failed() {
			return
		}
		_ = filepath.Walk(filepath.Join(home, ".factory"), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".log") {
				return nil
			}
			data, _ := os.ReadFile(p)
			for _, line := range strings.Split(string(data), "\n") {
				if low := strings.ToLower(line); strings.Contains(line, host.ChatServerName) || strings.Contains(low, "mcp") && strings.Contains(low, "fail") {
					t.Logf("%s: %.600s", filepath.Base(p), line)
				}
			}
			return nil
		})
	})
	url := fmt.Sprintf("ws://127.0.0.1:%d", port)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		c, err := droid.Dial(ctx, droid.Options{URL: url, Credential: &droid.Credential{APIKey: key}})
		if err == nil {
			c.Close()
			return url
		}
		if ctx.Err() != nil {
			t.Fatalf("the Daemon did not start: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type liveChannel struct{ id, creator string }

// findChannel is the WeCom Channel the robot is in with that name.
func findChannel(ctx context.Context, t *testing.T, api *channelAPI, name string) liveChannel {
	t.Helper()
	cursor := ""
	for {
		var res struct {
			Topics []struct {
				TopicID string `json:"topic_id"`
				Name    string `json:"topic_name"`
				Creator member `json:"creator"`
			} `json:"topic_list"`
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		}
		if err := api.call(ctx, "/topic/get_topic_list", map[string]any{"type": 1, "limit": 100, "cursor": cursor}, &res); err != nil {
			t.Fatalf("cannot list the robot's WeCom Channels: %v", err)
		}
		for _, tp := range res.Topics {
			if tp.Name == name {
				return liveChannel{id: tp.TopicID, creator: tp.Creator.UserID}
			}
		}
		if !res.HasMore || res.NextCursor == "" {
			t.Fatalf("the robot is in no WeCom Channel named %q", name)
		}
		cursor = res.NextCursor
	}
}

// robotReplies are the replies the robot wrote under th's post.
func robotReplies(ctx context.Context, t *testing.T, api *channelAPI, botID string, th *thread) []content {
	var out []content
	cursor := ""
	for {
		page, next, err := api.replyPage(ctx, th.topicID, th.postID, cursor, maxReplyPage)
		if err != nil {
			t.Logf("cannot read the replies: %v", err)
			return out
		}
		for _, r := range page {
			if a := r.author(); a.Type == 1 && a.BotID == botID {
				out = append(out, r)
			}
		}
		if next == "" {
			return out
		}
		cursor = next
	}
}

func emojiCount(ctx context.Context, api *channelAPI, th *thread, emoji int) int {
	var res struct {
		Posts []struct {
			Emoji []struct {
				Type  int      `json:"emoji_type"`
				Users []member `json:"user_list"`
			} `json:"emoji_list"`
		} `json:"post_list"`
	}
	if api.call(ctx, "/topic/get_post_list", map[string]any{"topic_id": th.topicID, "type": 0, "post_id_list": []string{th.postID}}, &res) != nil {
		return -1
	}
	n := 0
	for _, p := range res.Posts {
		for _, e := range p.Emoji {
			if e.Type == emoji {
				n += len(e.Users)
			}
		}
	}
	return n
}

func waitFor(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline, next := time.Now().Add(d), time.Now().Add(time.Minute)
	t.Logf("waiting up to %s for %s", d, what)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting for %s", what)
		}
		if time.Now().After(next) {
			t.Logf("still waiting for %s (%s left)", what, time.Until(deadline).Round(time.Second))
			next = time.Now().Add(time.Minute)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// liveWatch follows the Chat Bridge's Controller, the Daemon's frames and
// the Chat Bridge's log.
type liveWatch struct {
	t        *testing.T
	mu       sync.Mutex
	perms    []controller.Permission
	resolved map[string]protocol.ToolConfirmationOutcome
	idle     map[string]int
	// frames are the Daemon's frames that mention WeCom, such as droi-chat
	// tool results.
	frames []string
	logs   []string
}

func (w *liveWatch) log(s string) {
	w.t.Log("[chat] " + s)
	w.mu.Lock()
	w.logs = append(w.logs, s)
	w.mu.Unlock()
}

func (w *liveWatch) logged(part string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.ContainsFunc(w.logs, func(s string) bool { return strings.Contains(s, part) })
}

func (w *liveWatch) record(dir droid.Direction, frame []byte) {
	if dir != droid.Received || !bytes.Contains(frame, []byte("WeCom")) {
		return
	}
	w.mu.Lock()
	w.frames = append(w.frames, string(frame))
	w.mu.Unlock()
}

// toolResults counts the Daemon's frames of session (any when "") that hold
// part, and logs the tool errors among them.
func (w *liveWatch) toolResults(session, part string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, f := range w.frames {
		if strings.Contains(f, part) && strings.Contains(f, session) {
			n++
		}
	}
	return n
}

func (w *liveWatch) event(e controller.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch e := e.(type) {
	case controller.PermissionRequested:
		w.perms = append(w.perms, e.Permission)
		w.t.Logf("permission request %s: %s", e.Permission.RequestID, toolNames(&e.Permission))
	case controller.PermissionResolved:
		w.resolved[e.RequestID] = e.SelectedOption
		w.t.Logf("permission request %s answered: %s", e.RequestID, e.SelectedOption)
	case controller.SessionNotification:
		if v, ok := e.Value.(*protocol.DroidWorkingStateChangedNotification); ok && v.NewState == protocol.DroidWorkingStateIdle {
			w.idle[e.SessionID]++
		}
	}
}

func (w *liveWatch) turns(session string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.idle[session]
}

func (w *liveWatch) allPermissions() []controller.Permission {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.perms)
}

// thread waits for a Chat Thread that match picks to have a Chat Session.
func (w *liveWatch) thread(t *testing.T, b *Bridge, d time.Duration, what string, match func(*thread) bool) *thread {
	t.Helper()
	var found *thread
	waitFor(t, d, what, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, th := range b.threads {
			if th.sessionID != "" && match(th) {
				found = th
			}
		}
		return found != nil
	})
	t.Logf("%s started a Chat Session", what)
	return found
}

// permission waits for Droid to ask for permission in session.
func (w *liveWatch) permission(t *testing.T, d time.Duration, session string) controller.Permission {
	t.Helper()
	var found controller.Permission
	waitFor(t, d, "Droid to ask for permission", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, p := range w.perms {
			if _, done := w.resolved[p.RequestID]; !done && slices.Contains(p.AssociatedSessionIDs, session) {
				found = p
				return true
			}
		}
		return false
	})
	return found
}

func (w *liveWatch) answered(t *testing.T, d time.Duration, requestID string) {
	t.Helper()
	waitFor(t, d, "the permission request to be answered in WeCom", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		_, ok := w.resolved[requestID]
		return ok
	})
}
