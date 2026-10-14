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
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

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
	options  chatbridge.Options
	b        *chatbridge.Bridge
}

// restart stops the Chat Bridge and starts another on the same Daemon,
// WeCom and settings, as the Host does when it restarts.
func (r *rig) restart() {
	r.t.Helper()
	r.b.Stop()
	r.b = chatbridge.New(r.options)
	r.t.Cleanup(r.b.Stop)
	r.b.Follow()
}

// start runs a Chat Bridge whose settings allow HuangJunXiang; edit changes
// the settings before it starts.
func start(t *testing.T, edit func(*host.Settings)) *rig {
	t.Helper()
	return startWith(t, edit, nil)
}

func startWith(t *testing.T, edit func(*host.Settings), options func(*chatbridge.Options)) *rig {
	t.Helper()
	return startOn(t, fakedaemon.Scenario{}, edit, options)
}

// startTurn is startWith with the Fake Daemon answering every message with
// turn.
func startTurn(t *testing.T, turn fakedaemon.Turn, edit func(*host.Settings), options func(*chatbridge.Options)) *rig {
	t.Helper()
	return startOn(t, fakedaemon.Scenario{Turn: &turn}, edit, options)
}

// startOn runs a Chat Bridge on a Daemon with sc's Sessions and Turn; the
// Turn is a short reply when sc has none.
func startOn(t *testing.T, sc fakedaemon.Scenario, edit func(*host.Settings), options func(*chatbridge.Options)) *rig {
	t.Helper()
	if sc.Turn == nil {
		sc.Turn = &fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}
	}
	sc.ValidDirectories = []string{workspace}
	d := fakedaemon.Start(t, sc)
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
	return &rig{t: t, d: d, w: w, settings: st, file: file, clock: clk, options: o, b: b}
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

// loaded is the nth daemon.load_session the Chat Bridge sent.
func (r *rig) loaded(nth int) initialized {
	r.t.Helper()
	req, err := r.d.WaitForRequest("daemon.load_session", nth)
	if err != nil {
		r.t.Fatalf("no Chat Session loaded: %v", err)
	}
	var p initialized
	if err := json.Unmarshal(req.Params, &p); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// closed is the session closed by the nth daemon.close_session.
func (r *rig) closed(nth int) string {
	r.t.Helper()
	req, err := r.d.WaitForRequest("daemon.close_session", nth)
	if err != nil {
		r.t.Fatalf("no Chat Session closed: %v", err)
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(req.Params, &p)
	return p.SessionID
}

// before reports whether the Daemon got the ith request of method a before
// the jth of method b.
func (r *rig) before(a string, i int, b string, j int) bool {
	r.t.Helper()
	reqs, err := r.d.Requests()
	if err != nil {
		r.t.Fatal(err)
	}
	na, nb := 0, 0
	for _, q := range reqs {
		switch q.Method {
		case a:
			if na++; na == i {
				return nb < j
			}
		case b:
			nb++
		}
	}
	return false
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

// waitForStatus waits until the Chat Bridge reports ok.
func (r *rig) waitForStatus(what string, ok func(chatbridge.Status) bool) chatbridge.Status {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := r.b.Status()
		if ok(s) {
			return s
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("never %s; status %+v", what, s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheStatusSaysConnectedOrWhyNot(t *testing.T) {
	r := start(t, func(s *host.Settings) { s.ChatBridge.Secret = "wrong" })
	s := r.waitForStatus("not connected", func(s chatbridge.Status) bool { return s.State == chatbridge.NotConnected })
	if !strings.Contains(s.Reason, "853000") || !strings.Contains(s.Reason, "invalid bot_id or secret") || strings.Contains(s.Reason, "wrong") {
		t.Errorf("reason %q", s.Reason)
	}

	_ = r.settings.Update(func(s *host.Settings) { s.ChatBridge.Secret = robot.Secret })
	r.b.Follow()
	r.waitForStatus("connected", func(s chatbridge.Status) bool { return s.State == chatbridge.Connected && s.Reason == "" })

	r.w.Drop()
	r.w.WaitForSubscription(2)
	r.waitForStatus("connected again", func(s chatbridge.Status) bool { return s.State == chatbridge.Connected })

	_ = r.settings.Update(func(s *host.Settings) { s.ChatBridge.BotID = "" })
	r.b.Follow()
	r.waitForStatus("off", func(s chatbridge.Status) bool { return s.State == chatbridge.Off })
}

func TestTheStatusChangeIsHeard(t *testing.T) {
	heard := make(chan struct{}, 16)
	r := startWith(t, nil, func(o *chatbridge.Options) {
		o.OnChange = func() { heard <- struct{}{} }
	})
	r.waitForStatus("connected", func(s chatbridge.Status) bool { return s.State == chatbridge.Connected })
	select {
	case <-heard:
	case <-time.After(5 * time.Second):
		t.Fatal("nobody heard the Chat Bridge connect")
	}
}

func TestRestartConnectsAgainWithTheSameRobot(t *testing.T) {
	r := start(t, nil)
	r.w.WaitForSubscription(1)
	r.b.Restart()
	r.w.WaitForSubscription(2)
	if n := r.w.Handshakes(); n != 2 {
		t.Errorf("%d handshakes for one restart", n)
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

func TestAfterTheHostRestartsAChatThreadContinuesItsChatSession(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "First"})
	first := r.initialized(1)
	before := r.droid(first)
	r.message(1)

	r.restart()
	r.w.WaitForSubscription(2)
	reqID := r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Second"})

	if m := r.message(2); m.SessionID != first.SessionID || !strings.Contains(m.Text, "Second") {
		t.Errorf("second message %+v; the Chat Session is %s", m, first.SessionID)
	}
	if n := r.count("daemon.initialize_session"); n != 1 {
		t.Errorf("%d Chat Sessions", n)
	}
	l := r.loaded(1)
	if l.SessionID != first.SessionID {
		t.Errorf("loaded %s, the Chat Session is %s", l.SessionID, first.SessionID)
	}
	after := r.droid(l)
	if after.token == before.token {
		t.Error("the reloaded Chat Session kept the token of a Chat Bridge that stopped")
	}
	if text, isError := after.call("reply", map[string]any{"text": "Still here"}); isError {
		t.Fatalf("reply failed: %s", text)
	}
	if f := r.w.WaitForFrame("aibot_respond_msg", 1); f.ReqID != reqID {
		t.Errorf("reply on %s, the latest message is %s", f.ReqID, reqID)
	}
}

// chatSession is a Chat Session a Chat Bridge started before, for the
// direct message with user.
func chatSession(user string) fakedaemon.SessionSpec {
	return fakedaemon.SessionSpec{
		Title: "Earlier chat", Cwd: workspace,
		Messages: []fakedaemon.Message{{Role: "user", Text: "Hi"}, {Role: "assistant", Text: "Hello"}},
		Extra:    map[string]any{"tags": []map[string]any{{"name": "droi.chat", "metadata": map[string]string{"kind": "direct", "userId": user}}}},
	}
}

func TestAnIdleChatSessionIsClosedAndTheNextMessageLoadsIt(t *testing.T) {
	r := startOn(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{chatSession("HuangJunXiang")}}, nil,
		func(o *chatbridge.Options) { o.IdleTimeout = 100 * time.Millisecond })
	id := r.d.Sessions[0].SessionID
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "First"})
	if m := r.message(1); m.SessionID != id {
		t.Fatalf("first message to %s, the Chat Session is %s", m.SessionID, id)
	}
	if closed := r.closed(1); closed != id {
		t.Fatalf("closed %s, the Chat Session is %s", closed, id)
	}

	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Second"})
	if m := r.message(2); m.SessionID != id || !strings.Contains(m.Text, "Second") {
		t.Errorf("second message %+v; the Chat Session is %s", m, id)
	}
	l := r.loaded(2)
	if l.SessionID != id || !r.before("daemon.load_session", 2, "daemon.add_user_message", 2) {
		t.Errorf("loaded %s again, before the message: %v", l.SessionID, r.before("daemon.load_session", 2, "daemon.add_user_message", 2))
	}
	if text, isError := r.droid(l).call("reply", map[string]any{"text": "Back"}); isError {
		t.Errorf("reply failed: %s", text)
	}
	if old := r.droid(r.loaded(1)); old.token == r.droid(l).token {
		t.Error("the Chat Session was loaded again with the same token")
	} else if status, _ := old.rpc(old.token, "tools/call", map[string]any{"name": "reply", "arguments": map[string]any{"text": "stale"}}); status != http.StatusUnauthorized {
		t.Errorf("the token of the closed load: HTTP %d", status)
	}
	// The turn ran and ended: the Chat Session is closed again.
	if closed := r.closed(2); closed != id {
		t.Errorf("closed %s, the Chat Session is %s", closed, id)
	}
	if n := r.count("daemon.initialize_session"); n != 0 {
		t.Errorf("%d new Chat Sessions", n)
	}
}

// client is another Client of the Daemon, such as the window.
func (r *rig) client() *droid.Client {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := droid.Dial(ctx, droid.Options{URL: r.d.URL, Credential: &droid.Credential{APIKey: "fk-test"}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestAChatSessionArchivedInAClientEndsItsLink(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "First"})
	first := r.initialized(1)
	r.message(1)

	force := true
	if _, err := r.client().ArchiveSession(context.Background(), protocol.ArchiveSessionParams{SessionID: first.SessionID, Force: &force}); err != nil {
		t.Fatal(err)
	}
	// The Chat Bridge lets the archived Chat Session go.
	if id := r.closed(1); id != first.SessionID {
		t.Fatalf("closed %s, the archived Chat Session is %s", id, first.SessionID)
	}
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Second"})

	second := r.initialized(2)
	if second.SessionID == first.SessionID || second.Tags[0].Metadata["userId"] != "HuangJunXiang" {
		t.Errorf("new Chat Session %s with %+v; the archived one is %s", second.SessionID, second.Tags, first.SessionID)
	}
	m := r.message(2)
	if m.SessionID != second.SessionID || !strings.Contains(m.Text, "Second") || !strings.Contains(m.Text, "archived") {
		t.Errorf("first message of the new Chat Session %+v", m)
	}
	if status, _ := r.droid(first).rpc(r.droid(first).token, "tools/call", map[string]any{"name": "reply", "arguments": map[string]any{"text": "late"}}); status != http.StatusUnauthorized {
		t.Errorf("the archived Chat Session's token: HTTP %d", status)
	}
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Third"})
	if m := r.message(3); m.SessionID != second.SessionID || strings.Contains(m.Text, "archived") {
		t.Errorf("third message %+v", m)
	}
}

func TestAChatSessionArchivedWhileTheHostWasDownIsNotContinued(t *testing.T) {
	archived := chatSession("HuangJunXiang")
	archived.Extra["archivedAt"] = "2026-10-13T08:00:00.000Z"
	r := startOn(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{archived}}, nil, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Hello again"})

	p := r.initialized(1)
	if m := r.message(1); m.SessionID != p.SessionID || !strings.Contains(m.Text, "archived") || !strings.Contains(m.Text, "Hello again") {
		t.Errorf("first message %+v to %s", m, p.SessionID)
	}
	if n := r.count("daemon.load_session"); n != 0 {
		t.Errorf("%d loads of an archived Chat Session", n)
	}
}

// archivedSession is the session archived by the nth daemon.archive_session.
func (r *rig) archivedSession(nth int) string {
	r.t.Helper()
	req, err := r.d.WaitForRequest("daemon.archive_session", nth)
	if err != nil {
		r.t.Fatalf("no Chat Session archived: %v", err)
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(req.Params, &p)
	return p.SessionID
}

func TestNewArchivesTheChatSessionAndStartsAnother(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "First"})
	first := r.initialized(1)
	r.message(1)

	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "!new Fix the build"})
	if id := r.archivedSession(1); id != first.SessionID {
		t.Errorf("archived %s, the Chat Session is %s", id, first.SessionID)
	}
	second := r.initialized(2)
	m := r.message(2)
	if m.SessionID != second.SessionID || second.SessionID == first.SessionID {
		t.Errorf("message to %s; Chat Sessions %s then %s", m.SessionID, first.SessionID, second.SessionID)
	}
	if !strings.Contains(m.Text, "Fix the build") || strings.Contains(m.Text, "!new") || strings.Contains(m.Text, "archived") {
		t.Errorf("first message of the new Chat Session:\n%s", m.Text)
	}

	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "!new"})
	if id := r.archivedSession(2); id != second.SessionID {
		t.Errorf("archived %s, the Chat Session is %s", id, second.SessionID)
	}
	third := r.initialized(3)
	if m := r.message(3); m.SessionID != third.SessionID || strings.Contains(m.Text, "Fix the build") {
		t.Errorf("message %+v; the Chat Session is %s", m, third.SessionID)
	}

	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Next"})
	if m := r.message(4); m.SessionID != third.SessionID || strings.Contains(m.Text, "archived") {
		t.Errorf("message %+v; the Chat Session is %s", m, third.SessionID)
	}
	if n := r.count("daemon.initialize_session"); n != 3 {
		t.Errorf("%d Chat Sessions", n)
	}
}

func TestTwoChatThreadsRunAsTwoChatSessionsAtOnce(t *testing.T) {
	r := startOn(t, fakedaemon.Scenario{Turn: &fakedaemon.Turn{Kind: "reply", Deltas: []string{"long", " answer"}, DelayMs: 1000}},
		func(s *host.Settings) { s.ChatBridge.AllowedUsers = []string{"HuangJunXiang", "LiLei"} },
		func(o *chatbridge.Options) { o.IdleTimeout = 50 * time.Millisecond })
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "A long task"})
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "LiLei", Text: "A quick question"})

	first, second := r.message(1), r.message(2)
	if first.SessionID == second.SessionID {
		t.Fatalf("both messages went to %s", first.SessionID)
	}
	// Both turns run for 2 s: no Chat Session is idle yet.
	time.Sleep(500 * time.Millisecond)
	if n := r.count("daemon.close_session"); n != 0 {
		t.Errorf("%d Chat Sessions closed before the turns ended", n)
	}
	closed := map[string]bool{r.closed(1): true, r.closed(2): true}
	if !closed[first.SessionID] || !closed[second.SessionID] {
		t.Errorf("closed %v after the turns", closed)
	}
}

func TestADaemonRestartKeepsAChatThreadsLink(t *testing.T) {
	r := start(t, nil)
	r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "First"})
	p := r.initialized(1)
	r.message(1)

	if err := r.d.GoDown(); err != nil {
		t.Fatal(err)
	}
	if err := r.d.ComeBack(); err != nil {
		t.Fatal(err)
	}
	reqID := r.w.SendDirectMessage(fakewecom.DirectMessage{From: "HuangJunXiang", Text: "Second"})

	if m := r.message(2); m.SessionID != p.SessionID || !strings.Contains(m.Text, "Second") {
		t.Errorf("second message %+v; the Chat Session is %s", m, p.SessionID)
	}
	if n := r.count("daemon.initialize_session"); n != 1 {
		t.Errorf("%d Chat Sessions", n)
	}
	// The Chat Session was loaded on the new connection with droi-chat.
	l := r.loaded(1)
	if l.SessionID != p.SessionID {
		t.Errorf("loaded %s, the Chat Session is %s", l.SessionID, p.SessionID)
	}
	if text, isError := r.droid(l).call("reply", map[string]any{"text": "Still here"}); isError {
		t.Fatalf("reply failed: %s", text)
	}
	if f := r.w.WaitForFrame("aibot_respond_msg", 1); f.ReqID != reqID {
		t.Errorf("reply on %s, the latest message is %s", f.ReqID, reqID)
	}
}

func TestAnotherConnectionTakingTheRobotStopsTheChatBridge(t *testing.T) {
	r := start(t, nil)
	r.waitForStatus("connected", func(s chatbridge.Status) bool { return s.State == chatbridge.Connected })

	// Another Host subscribes the same robot.
	other := chatbridge.New(r.options)
	t.Cleanup(other.Stop)
	other.Follow()
	r.w.WaitForSubscription(2)
	s := r.waitForStatus("stopped", func(s chatbridge.Status) bool { return s.State == chatbridge.NotConnected })
	if !strings.Contains(s.Reason, "another connection took over the robot") {
		t.Errorf("status %+v after another connection took the robot", s)
	}
	time.Sleep(300 * time.Millisecond)
	if n := r.w.Subscriptions(); n != 2 {
		t.Errorf("%d subscriptions: the Chat Bridge fought for the robot", n)
	}
	if s := other.Status(); s.State != chatbridge.Connected {
		t.Errorf("the other Host's status %+v", s)
	}

	// Restart takes the robot back.
	r.b.Restart()
	r.w.WaitForSubscription(3)
	r.waitForStatus("connected again", func(s chatbridge.Status) bool { return s.State == chatbridge.Connected })
}
