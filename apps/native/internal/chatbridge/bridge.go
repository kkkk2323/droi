// Package chatbridge is the Chat Bridge (ADR 0020): the part of the Host
// that keeps one WeCom smart robot's long connection and runs what people
// ask it as Chat Sessions on the Daemon, one per Chat Thread. It is a
// trigger and a set of tools: it sends a Chat Session one message per
// trigger, and Droid reaches the Chat Thread through the droi-chat tools it
// serves to that Session alone.
package chatbridge

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

// Options is what the Chat Bridge needs from the Host.
type Options struct {
	// Settings hold the robot, the Workspace, the allowlist and the System
	// Prompt Addition; the Chat Bridge records refused senders there.
	Settings *host.SettingsStore
	// Daemon is how the Chat Bridge's own Controller reaches the Daemon.
	Daemon controller.Config
	// WebSocketURL is WeCom's long connection, DefaultWebSocketURL when "".
	WebSocketURL string
	// PingInterval is 30 s, ReconnectDelay 1 s and MaxReconnectDelay a
	// minute when zero.
	PingInterval      time.Duration
	ReconnectDelay    time.Duration
	MaxReconnectDelay time.Duration
	// Now is time.Now when nil.
	Now func() time.Time
	// OnChange hears that the Chat Bridge changed the settings or its
	// Status.
	OnChange func()
	Log      func(string)
}

// Bridge is the Chat Bridge.
type Bridge struct {
	o      Options
	ctl    *controller.Controller
	pushes chan frame
	stop   chan struct{}
	done   chan struct{}

	mu      sync.Mutex
	stopped bool
	robot   string
	wecom   *longConnection
	status  Status
	tools   *toolServer
	threads map[string]*thread
	tokens  map[string]*thread
	seen    recent
}

// State is how far the Chat Bridge is connected to WeCom.
type State int

const (
	// Off: no robot is set.
	Off State = iota
	Connecting
	Connected
	// NotConnected: the last attempt failed or the connection dropped; the
	// Chat Bridge tries again.
	NotConnected
)

// Status is the Chat Bridge's connection, for Settings.
type Status struct {
	State State
	// Reason is why it is NotConnected: WeCom's answer or the network's
	// error. It never holds the Secret.
	Reason string
}

// thread is a Chat Thread and its Chat Session.
type thread struct {
	key string
	// userID is the other side of a direct message.
	userID    string
	sessionID string
	token     string
	// reqID is the latest callback's, which replies go under for a day.
	reqID string
	reqAt time.Time
}

const (
	threadKindDirect = "direct"
	replyWindow      = 24 * time.Hour
	deliverTimeout   = 2 * time.Minute
)

// New makes a Chat Bridge; Follow starts it.
func New(o Options) *Bridge {
	if o.WebSocketURL == "" {
		o.WebSocketURL = DefaultWebSocketURL
	}
	if o.PingInterval <= 0 {
		o.PingInterval = defaultPingInterval
	}
	if o.ReconnectDelay <= 0 {
		o.ReconnectDelay = defaultReconnectDelay
	}
	if o.MaxReconnectDelay <= 0 {
		o.MaxReconnectDelay = defaultMaxReconnectDelay
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.OnChange == nil {
		o.OnChange = func() {}
	}
	if o.Log == nil {
		o.Log = func(string) {}
	}
	b := &Bridge{
		o:       o,
		ctl:     controller.New(o.Daemon),
		pushes:  make(chan frame, 256),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		threads: map[string]*thread{},
		tokens:  map[string]*thread{},
	}
	go b.work()
	return b
}

// Follow connects, reconnects or disconnects the robot as the settings now
// name it: an empty Bot ID leaves the Chat Bridge off.
func (b *Bridge) Follow() { b.follow(false) }

// Restart connects again with the settings as they are now, even when the
// robot is the same, rather than waiting out the delay between attempts.
func (b *Bridge) Restart() { b.follow(true) }

// Status is the connection to WeCom now.
func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

func (b *Bridge) follow(again bool) {
	cb := b.o.Settings.Get().ChatBridge
	robot := ""
	if cb.On() {
		robot = cb.BotID + "\x00" + cb.Secret
	}
	b.mu.Lock()
	if b.stopped || robot == b.robot && !again {
		b.mu.Unlock()
		return
	}
	old := b.wecom
	b.robot, b.wecom = robot, nil
	b.status = Status{State: Off}
	if robot != "" {
		if b.tools == nil {
			t, err := startToolServer(b)
			if err != nil {
				b.o.Log("the droi-chat tools cannot listen: " + err.Error())
			} else {
				b.tools = t
			}
		}
		wc := &longConnection{
			url: b.o.WebSocketURL, botID: cb.BotID, secret: cb.Secret,
			ping: b.o.PingInterval, delay: b.o.ReconnectDelay, max: b.o.MaxReconnectDelay,
			onPush: b.push, log: b.o.Log,
		}
		wc.onStatus = func(connected bool, reason string) { b.connectionStatus(wc, connected, reason) }
		b.wecom, b.status = wc, Status{State: Connecting}
		wc.start()
	}
	b.mu.Unlock()
	if old != nil {
		old.close()
	}
	go b.o.OnChange()
}

// connectionStatus records what connection wc reports while it is the
// robot's. OnChange runs on its own goroutine: it may call Follow, which
// waits for wc to end.
func (b *Bridge) connectionStatus(wc *longConnection, connected bool, reason string) {
	b.mu.Lock()
	if b.wecom != wc {
		b.mu.Unlock()
		return
	}
	b.status = Status{State: Connected}
	if !connected {
		b.status = Status{State: NotConnected, Reason: reason}
	}
	b.mu.Unlock()
	go b.o.OnChange()
}

// Stop disconnects the robot and the Daemon.
func (b *Bridge) Stop() {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	b.stopped = true
	wc, tools := b.wecom, b.tools
	b.wecom, b.tools = nil, nil
	b.mu.Unlock()
	if wc != nil {
		wc.close()
	}
	if tools != nil {
		tools.close()
	}
	close(b.stop)
	<-b.done
	_ = b.ctl.Close()
}

func (b *Bridge) push(f frame) {
	select {
	case b.pushes <- f:
	case <-b.stop:
	}
}

// work handles the robot's events one at a time, in the order WeCom sent
// them.
func (b *Bridge) work() {
	defer close(b.done)
	for {
		select {
		case f := <-b.pushes:
			b.handle(f)
		case <-b.stop:
			return
		}
	}
}

// callback is the body of aibot_msg_callback.
type callback struct {
	MsgID    string `json:"msgid"`
	ChatType string `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Voice struct {
		Content string `json:"content"`
	} `json:"voice"`
	Mixed struct {
		Items []struct {
			MsgType string `json:"msgtype"`
			Text    struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"msg_item"`
	} `json:"mixed"`
	// CompatInfo marks a copy WeCom makes for robots written for group chats.
	CompatInfo struct {
		Source string `json:"source"`
	} `json:"compat_info"`
}

func (b *Bridge) handle(f frame) {
	if f.Cmd != "aibot_msg_callback" {
		return
	}
	var m callback
	if json.Unmarshal(f.Body, &m) != nil {
		return
	}
	// Group chats, and the group-chat copy of a Channel post, start nothing.
	if m.ChatType != "single" || m.CompatInfo.Source != "" || m.From.UserID == "" {
		return
	}
	b.mu.Lock()
	dup := !b.seen.add(m.MsgID)
	b.mu.Unlock()
	if dup {
		return
	}
	user := m.From.UserID
	if !slices.Contains(b.o.Settings.Get().ChatBridge.AllowedUsers, user) {
		b.refuse(user)
		return
	}
	b.mu.Lock()
	key := threadKindDirect + ":" + user
	th := b.threads[key]
	if th == nil {
		th = &thread{key: key, userID: user}
		b.threads[key] = th
	}
	th.reqID, th.reqAt = f.Headers.ReqID, b.o.Now()
	b.mu.Unlock()
	b.deliver(th, directTrigger(user, textOf(m)))
}

// refuse records a sender not on the allowlist, for the user to allow.
func (b *Bridge) refuse(user string) {
	b.o.Log("refused a message from " + user + ", who is not on the allowlist")
	if slices.Contains(b.o.Settings.Get().ChatBridge.RefusedUsers, user) {
		return
	}
	_ = b.o.Settings.Update(func(s *host.Settings) {
		if !slices.Contains(s.ChatBridge.RefusedUsers, user) {
			s.ChatBridge.RefusedUsers = append(s.ChatBridge.RefusedUsers, user)
		}
	})
	b.o.OnChange()
}

func textOf(m callback) string {
	switch m.MsgType {
	case "text":
		return m.Text.Content
	case "voice":
		return m.Voice.Content
	case "mixed":
		out := ""
		for _, it := range m.Mixed.Items {
			if out != "" {
				out += "\n"
			}
			if it.MsgType == "text" {
				out += it.Text.Content
			} else {
				out += "[" + it.MsgType + "]"
			}
		}
		return out
	}
	return "[" + m.MsgType + "]"
}

// deliver sends the Chat Thread's Chat Session one message, starting the
// Session first when the Chat Thread has none.
func (b *Bridge) deliver(th *thread, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), deliverTimeout)
	defer cancel()
	go func() {
		select {
		case <-b.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	if err := b.ctl.Connect(ctx); err != nil {
		b.o.Log("cannot reach the Daemon: " + err.Error())
		return
	}
	b.mu.Lock()
	id := th.sessionID
	b.mu.Unlock()
	if id == "" {
		var err error
		if id, err = b.startSession(ctx, th); err != nil {
			b.o.Log("cannot start a Chat Session: " + err.Error())
			return
		}
	}
	if _, err := b.ctl.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: id, Text: text}); err != nil {
		b.o.Log("cannot send the Chat Session its message: " + err.Error())
	}
}

func (b *Bridge) startSession(ctx context.Context, th *thread) (string, error) {
	s := b.o.Settings.Get()
	cb := s.ChatBridge
	if cb.Workspace == "" {
		return "", errors.New("the Chat Bridge has no Workspace")
	}
	b.mu.Lock()
	tools := b.tools
	b.mu.Unlock()
	if tools == nil {
		return "", errors.New("the droi-chat tools are not listening")
	}
	token := newToken()
	server, _ := json.Marshal(map[string]any{
		"type":    "http",
		"name":    host.ChatServerName,
		"url":     tools.url,
		"oauth":   false,
		"headers": []map[string]string{{"name": "Authorization", "value": "Bearer " + token}},
	})
	addition := ""
	if s.AppendSystemPrompt != nil {
		addition = *s.AppendSystemPrompt
	}
	system, _ := json.Marshal(map[string]string{"type": "preset", "preset": "droid", "append": systemPromptAppend(addition, th)})
	yes := true
	res, err := b.ctl.InitializeSession(ctx, protocol.InitializeSessionParams{
		Cwd:            cb.Workspace,
		BlockOnMCPLoad: &yes,
		AutonomyLevel:  protocol.AutonomyLevel(cb.AutonomyLevelOrDefault()),
		ModelID:        cb.Model,
		SystemPrompt:   system,
		Tags:           []protocol.SessionTag{{Name: sessions.ChatTag, Metadata: map[string]string{"kind": threadKindDirect, "userId": th.userID}}},
		MCPServers:     []json.RawMessage{server},
	})
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	th.sessionID, th.token = res.SessionID, token
	b.tokens[token] = th
	b.mu.Unlock()
	return res.SessionID, nil
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// threadOf is the Chat Thread a droi-chat token was given for.
func (b *Bridge) threadOf(token string) *thread {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tokens[token]
}

// recent are the msgids seen lately; WeCom may deliver an event again.
type recent struct {
	set   map[string]bool
	order []string
}

const recentSize = 2000

// add reports whether id is new.
func (r *recent) add(id string) bool {
	if id == "" {
		return true
	}
	if r.set == nil {
		r.set = map[string]bool{}
	}
	if r.set[id] {
		return false
	}
	r.set[id] = true
	r.order = append(r.order, id)
	if len(r.order) > recentSize {
		delete(r.set, r.order[0])
		r.order = r.order[1:]
	}
	return true
}
