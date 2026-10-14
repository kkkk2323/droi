package chatbridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge"
	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

const workspace = "/Users/dev/acme"

var robot = fakewecom.Robot{BotID: "aib-test-robot", Secret: "s3cret"}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// rig is the Chat Bridge between a Fake WeCom and the Fake Daemon.
type rig struct {
	t        *testing.T
	d        *fakedaemon.Daemon
	w        *fakewecom.WeCom
	settings *host.SettingsStore
	file     string
	clock    *clock
	b        *chatbridge.Bridge
}

// start runs a Chat Bridge whose settings allow HuangJunXiang; edit changes
// the settings before it starts.
func start(t *testing.T, edit func(*host.Settings)) *rig {
	t.Helper()
	return startWith(t, edit, nil)
}

func startWith(t *testing.T, edit func(*host.Settings), options func(*chatbridge.Options)) *rig {
	t.Helper()
	return startTurn(t, &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}, edit, options)
}

// startTurn runs a Chat Bridge whose Chat Sessions' turns go as turn says.
func startTurn(t *testing.T, turn *fakedaemon.Turn, edit func(*host.Settings), options func(*chatbridge.Options)) *rig {
	t.Helper()
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Turn:             turn,
		ValidDirectories: []string{workspace},
	})
	w := fakewecom.Start(t, robot)
	file := filepath.Join(t.TempDir(), "settings.json")
	st := host.OpenSettings(file, func(string) string { return "" })
	_ = st.Update(func(s *host.Settings) {
		s.ChatBridge = host.ChatBridgeSettings{
			BotID: robot.BotID, Secret: robot.Secret, Workspace: workspace,
			AllowedUsers: []string{"HuangJunXiang"},
		}
		if edit != nil {
			edit(s)
		}
	})
	clk := &clock{now: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)}
	w.SetClock(clk.Now)
	o := chatbridge.Options{
		Settings: st,
		Daemon: controller.Config{
			URL:        d.URL,
			Credential: func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-test"}, nil },
		},
		WebSocketURL:   w.URL,
		APIURL:         w.APIURL,
		ReconnectDelay: 20 * time.Millisecond,
		Now:            clk.Now,
		Log:            func(s string) { t.Log(s) },
	}
	if options != nil {
		options(&o)
	}
	b := chatbridge.New(o)
	t.Cleanup(b.Stop)
	b.Follow()
	return &rig{t: t, d: d, w: w, settings: st, file: file, clock: clk, b: b}
}

// initialized is the nth daemon.initialize_session the Chat Bridge sent.
type initialized struct {
	SessionID     string `json:"sessionId"`
	Cwd           string `json:"cwd"`
	AutonomyLevel string `json:"autonomyLevel"`
	ModelID       string `json:"modelId"`
	// BlockOnMCPLoad keeps the first turn until droi-chat is connected.
	BlockOnMCPLoad bool `json:"blockOnMcpLoad"`
	SystemPrompt   struct {
		Type   string `json:"type"`
		Preset string `json:"preset"`
		Append string `json:"append"`
	} `json:"systemPrompt"`
	Tags []struct {
		Name     string            `json:"name"`
		Metadata map[string]string `json:"metadata"`
	} `json:"tags"`
	MCPServers []struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		URL     string `json:"url"`
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
	} `json:"mcpServers"`
}

func (r *rig) initialized(nth int) initialized {
	r.t.Helper()
	req, err := r.d.WaitForRequest("daemon.initialize_session", nth)
	if err != nil {
		r.t.Fatalf("no Chat Session started: %v", err)
	}
	var p initialized
	if err := json.Unmarshal(req.Params, &p); err != nil {
		r.t.Fatal(err)
	}
	return p
}

type message struct {
	SessionID      string `json:"sessionId"`
	Text           string `json:"text"`
	QueuePlacement string `json:"queuePlacement"`
}

func (r *rig) message(nth int) message {
	r.t.Helper()
	req, err := r.d.WaitForRequest("daemon.add_user_message", nth)
	if err != nil {
		r.t.Fatalf("no message %d reached the Daemon: %v", nth, err)
	}
	var m message
	_ = json.Unmarshal(req.Params, &m)
	return m
}

func (r *rig) count(method string) int {
	r.t.Helper()
	reqs, err := r.d.Requests()
	if err != nil {
		r.t.Fatal(err)
	}
	n := 0
	for _, q := range reqs {
		if q.Method == method {
			n++
		}
	}
	return n
}

// droid plays Droid in a Chat Session: it calls the droi-chat server the
// Session was given.
type droidSide struct {
	t     *testing.T
	url   string
	token string
}

func (r *rig) droid(p initialized) droidSide {
	r.t.Helper()
	for _, s := range p.MCPServers {
		if s.Name != "droi-chat" {
			continue
		}
		for _, h := range s.Headers {
			if h.Name == "Authorization" {
				return droidSide{t: r.t, url: s.URL, token: strings.TrimPrefix(h.Value, "Bearer ")}
			}
		}
	}
	r.t.Fatalf("no droi-chat server in %+v", p.MCPServers)
	return droidSide{}
}

// rpc posts one JSON-RPC request and answers the HTTP status and the result
// or error.
func (d droidSide) rpc(token, method string, params any) (int, map[string]any) {
	d.t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest(http.MethodPost, d.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// call calls a droi-chat tool and answers its text and whether it is a tool
// error.
func (d droidSide) call(name string, args map[string]any) (string, bool) {
	d.t.Helper()
	status, out := d.rpc(d.token, "tools/call", map[string]any{"name": name, "arguments": args})
	if status != http.StatusOK {
		d.t.Fatalf("tools/call %s: HTTP %d %v", name, status, out)
	}
	res, _ := out["result"].(map[string]any)
	if res == nil {
		d.t.Fatalf("tools/call %s: %v", name, out)
	}
	var text strings.Builder
	content, _ := res["content"].([]any)
	for _, c := range content {
		if m, ok := c.(map[string]any); ok && m["type"] == "text" {
			text.WriteString(m["text"].(string))
		}
	}
	isError, _ := res["isError"].(bool)
	return text.String(), isError
}

func TestADirectMessageStartsAChatSession(t *testing.T) {
	r := start(t, func(s *host.Settings) {
		s.AppendSystemPrompt = host.OptString("Answer in English.")
		s.ChatBridge.Model = "glm-5.3-flash"
	})
	r.w.WaitForSubscription(1)
	if r.w.Refused() != 0 {
		t.Errorf("WeCom refused %d handshakes for their header spelling", r.w.Refused())
	}
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "你好, what is in the README?"})

	p := r.initialized(1)
	if p.Cwd != workspace || p.AutonomyLevel != "low" || p.ModelID != "glm-5.3-flash" {
		t.Errorf("cwd %q, autonomy %q, model %q", p.Cwd, p.AutonomyLevel, p.ModelID)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "droi.chat" || p.Tags[0].Metadata["kind"] != "direct" || p.Tags[0].Metadata["userId"] != "HuangJunXiang" {
		t.Errorf("tags %+v", p.Tags)
	}
	sp := p.SystemPrompt
	if sp.Type != "preset" || sp.Preset != "droid" || !strings.HasPrefix(sp.Append, "Answer in English.\n\n") ||
		!strings.Contains(sp.Append, "HuangJunXiang") || !strings.Contains(sp.Append, "reply") {
		t.Errorf("system prompt %+v", sp)
	}
	if !p.BlockOnMCPLoad {
		t.Error("the first turn may start before droi-chat is connected")
	}
	if len(p.MCPServers) != 1 || p.MCPServers[0].Type != "http" || !strings.HasPrefix(p.MCPServers[0].URL, "http://127.0.0.1:") {
		t.Errorf("mcpServers %+v", p.MCPServers)
	}
	r.droid(p)

	m := r.message(1)
	if m.SessionID != p.SessionID {
		t.Errorf("message to %s, the Chat Session is %s", m.SessionID, p.SessionID)
	}
	for _, want := range []string{"HuangJunXiang", "direct message", "你好, what is in the README?"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("trigger message without %q:\n%s", want, m.Text)
		}
	}
}

func TestReplyAnswersTheDirectMessage(t *testing.T) {
	r := start(t, nil)
	reqID := r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	droid := r.droid(r.initialized(1))

	if text, isError := droid.call("reply", map[string]any{"text": "**Hello** from Droid"}); isError {
		t.Fatalf("reply failed: %s", text)
	}
	f := r.w.WaitForFrame("aibot_respond_msg", 1)
	var body struct {
		MsgType  string `json:"msgtype"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	_ = json.Unmarshal(f.Body, &body)
	if f.ReqID != reqID || body.MsgType != "markdown" || body.Markdown.Content != "**Hello** from Droid" {
		t.Errorf("respond_msg on %s (the message's is %s): %s", f.ReqID, reqID, f.Body)
	}
}

func TestReplyADayLaterIsSentAsANewMessage(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	droid := r.droid(r.initialized(1))
	r.message(1)
	r.clock.Add(25 * time.Hour)

	if text, isError := droid.call("reply", map[string]any{"text": "Done."}); isError {
		t.Fatalf("reply failed: %s", text)
	}
	f := r.w.WaitForFrame("aibot_send_msg", 1)
	var body struct {
		ChatID   string `json:"chatid"`
		ChatType int    `json:"chat_type"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	_ = json.Unmarshal(f.Body, &body)
	if body.ChatID != "HuangJunXiang" || body.ChatType != 1 || body.Markdown.Content != "Done." {
		t.Errorf("send_msg %s", f.Body)
	}
	if n := len(r.w.Frames("aibot_respond_msg")); n != 0 {
		t.Errorf("%d replies on a day-old req_id", n)
	}
}

func TestATokenReachesOnlyItsOwnChatThread(t *testing.T) {
	r := start(t, func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} })
	huang := r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	first := r.initialized(1)
	r.message(1)
	li := r.w.SendDirectMessage(fakewecom.DirectMessage{From: "LiLei", Text: "Hello"})
	second := r.initialized(2)
	if first.SessionID == second.SessionID {
		t.Fatal("two users share a Chat Session")
	}

	if _, isError := r.droid(second).call("reply", map[string]any{"text": "For Li Lei"}); isError {
		t.Fatal("reply failed")
	}
	if f := r.w.WaitForFrame("aibot_respond_msg", 1); f.ReqID != li || f.ReqID == huang {
		t.Errorf("Li Lei's Session replied on %s; Li Lei's message is %s", f.ReqID, li)
	}

	droid := r.droid(first)
	for _, token := range []string{"", "not-a-session-token"} {
		if status, _ := droid.rpc(token, "tools/call", map[string]any{"name": "reply", "arguments": map[string]any{"text": "sneak"}}); status != http.StatusUnauthorized {
			t.Errorf("token %q: HTTP %d", token, status)
		}
	}
	if n := len(r.w.Frames("aibot_respond_msg")) + len(r.w.Frames("aibot_send_msg")); n != 1 {
		t.Errorf("%d messages reached WeCom", n)
	}
}

func TestAWeComErrorComesBackAsAToolError(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	droid := r.droid(r.initialized(1))
	r.w.Fail("aibot_respond_msg", 45009, "api freq out of limit")

	text, isError := droid.call("reply", map[string]any{"text": "Too fast"})
	if !isError || !strings.Contains(text, "45009") || !strings.Contains(text, "api freq out of limit") {
		t.Errorf("reply answered %q (error %v)", text, isError)
	}
	if text, isError := droid.call("reply", map[string]any{"text": "Slower"}); isError {
		t.Errorf("the next reply failed: %s", text)
	}
}

func TestTheDroiChatServerListsItsTools(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})
	droid := r.droid(r.initialized(1))
	status, out := droid.rpc(droid.token, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "droid", "version": "1"}})
	if status != http.StatusOK || out["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: HTTP %d %v", status, out)
	}
	_, out = droid.rpc(droid.token, "tools/list", map[string]any{})
	list, _ := out["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, x := range list {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "reply,read_thread,react" {
		t.Errorf("tools %v", names)
	}
}

func TestASenderNotOnTheAllowlistStartsNothingAndIsRecorded(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "wo-encrypted-7f3a", Text: "Run rm -rf /"})
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "wo-encrypted-7f3a", Text: "Again"})
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hi"})

	p := r.initialized(1)
	if p.Tags[0].Metadata["userId"] != "HuangJunXiang" {
		t.Errorf("a Chat Session for %v", p.Tags)
	}
	if m := r.message(1); !strings.Contains(m.Text, "Hi") {
		t.Errorf("first message %q", m.Text)
	}
	if n := r.count("daemon.initialize_session"); n != 1 {
		t.Errorf("%d Chat Sessions", n)
	}
	if got := host.OpenSettings(r.file, nil).Get().ChatBridge.RefusedUsers; len(got) != 1 || got[0] != "wo-encrypted-7f3a" {
		t.Errorf("refused %v", got)
	}
}

func TestARepeatedMsgidStartsNothingTwice(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{MsgID: "m-1", From: "HuangJunXiang", Text: "Once"})
	r.w.SendDirectMessage(fakewecom.DirectMessage{MsgID: "m-1", From: "HuangJunXiang", Text: "Once"})
	r.w.SendDirectMessage(fakewecom.DirectMessage{MsgID: "m-2", From: "HuangJunXiang", Text: "Twice"})

	if m := r.message(2); !strings.Contains(m.Text, "Twice") {
		t.Errorf("second message %q", m.Text)
	}
	if n := r.count("daemon.add_user_message"); n != 2 {
		t.Errorf("%d messages for two msgids", n)
	}
}

func TestAFurtherDirectMessageGoesToTheSameChatSession(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "First"})
	p := r.initialized(1)
	r.message(1)
	reqID := r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Second"})

	if m := r.message(2); m.SessionID != p.SessionID || !strings.Contains(m.Text, "Second") {
		t.Errorf("second message %+v to %s", m, p.SessionID)
	}
	if n := r.count("daemon.initialize_session"); n != 1 {
		t.Errorf("%d Chat Sessions", n)
	}
	// A reply goes under the latest message.
	r.droid(p).call("reply", map[string]any{"text": "ok"})
	if f := r.w.WaitForFrame("aibot_respond_msg", 1); f.ReqID != reqID {
		t.Errorf("reply on %s, the latest message is %s", f.ReqID, reqID)
	}
}

func TestALongMessageIsCutAt2000Characters(t *testing.T) {
	r := start(t, nil)
	long := strings.Repeat("字", 2500)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: long})

	m := r.message(1)
	if strings.Contains(m.Text, strings.Repeat("字", 2001)) || !strings.Contains(m.Text, strings.Repeat("字", 2000)) {
		t.Errorf("the text is not cut at 2000 characters")
	}
	if !strings.Contains(m.Text, "cut") {
		t.Errorf("no marker says the text was cut:\n%s", m.Text[len(m.Text)-200:])
	}
}

func TestTheLongConnectionPingsAndReconnectsAfterADrop(t *testing.T) {
	r := start(t, nil)
	r.w.WaitForSubscription(1)
	sub := r.w.WaitForFrame("aibot_subscribe", 1)
	var body map[string]string
	_ = json.Unmarshal(sub.Body, &body)
	if body["bot_id"] != robot.BotID || body["secret"] != robot.Secret {
		t.Errorf("subscribe %s", sub.Body)
	}
	r.w.Drop()
	r.w.WaitForSubscription(2)
	// The Chat Bridge still answers on the new connection.
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Back?"})
	r.message(1)
}

func TestTheLongConnectionKeepsPinging(t *testing.T) {
	r := startWith(t, nil, func(o *chatbridge.Options) { o.PingInterval = 30 * time.Millisecond })
	r.w.WaitForSubscription(1)
	r.w.WaitForFrame("ping", 3)
	if n := r.w.Subscriptions(); n != 1 {
		t.Errorf("%d subscriptions", n)
	}
}

func TestAnEmptyBotIDLeavesTheChatBridgeOff(t *testing.T) {
	r := start(t, func(s *host.Settings) { s.ChatBridge.BotID = "" })
	time.Sleep(300 * time.Millisecond)
	if n := r.w.Handshakes(); n != 0 {
		t.Fatalf("%d connection attempts while off", n)
	}

	_ = r.settings.Update(func(s *host.Settings) { s.ChatBridge.BotID = robot.BotID })
	r.b.Follow()
	r.w.WaitForSubscription(1)

	_ = r.settings.Update(func(s *host.Settings) { s.ChatBridge.BotID = "" })
	r.b.Follow()
	r.w.WaitForDisconnect()
}
