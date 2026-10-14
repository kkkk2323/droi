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
	// APIURL is WeCom's HTTPS API for smart robots, DefaultAPIURL when "".
	APIURL string
	// PingInterval is 30 s, ReconnectDelay 1 s and MaxReconnectDelay a
	// minute when zero.
	PingInterval      time.Duration
	ReconnectDelay    time.Duration
	MaxReconnectDelay time.Duration
	// IdleTimeout is how long after its turn ends an idle Chat Session is
	// closed, 15 s when zero; the next message loads it again.
	IdleTimeout time.Duration
	// Now is time.Now when nil.
	Now func() time.Time
	// OnChange hears that the Chat Bridge changed the settings or stopped
	// (see Status).
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
	// threadOps are the Chat Threads' running operation queues.
	threadOps sync.WaitGroup

	mu      sync.Mutex
	stopped bool
	robot   string
	wecom   *longConnection
	api     *channelAPI
	tools   *toolServer
	threads map[string]*thread
	tokens  map[string]*thread
	seen    recent
	// synced: the Chat Thread map was rebuilt from the Daemon's Sessions
	// since the Chat Bridge last connected to it.
	synced bool
	syncMu sync.Mutex
}

// thread is a Chat Thread and its Chat Session.
type thread struct {
	key  string
	kind string
	// userID is the other side of a direct message.
	userID string
	// topicID and postID are a Channel post's; channel is the Channel's name.
	topicID, postID, channel string
	// read are the replies under the post read_thread showed Droid.
	read      map[string]bool
	sessionID string
	// loaded: this Chat Bridge started or loaded the Chat Session, so it
	// has the droi-chat server of token.
	loaded bool
	token  string
	// archived: the Chat Thread's last Chat Session was archived, which its
	// next one is told.
	archived bool
	// ops run one at a time, in order, so a Chat Thread's messages, its
	// Chat Session's start and its idle close never overlap; other Chat
	// Threads go on meanwhile.
	ops     []func()
	running bool
	// epoch counts the messages delivered, so an idle close armed before
	// the latest one does nothing.
	epoch int
	idle  *time.Timer
	// reqID is the latest callback's, which replies go under for a day.
	reqID string
	reqAt time.Time
}

const (
	threadKindDirect   = "direct"
	threadKindChannel  = "channel"
	replyWindow        = 24 * time.Hour
	defaultIdleTimeout = 15 * time.Second
	deliverTimeout     = 2 * time.Minute
)

// New makes a Chat Bridge; Follow starts it.
func New(o Options) *Bridge {
	if o.WebSocketURL == "" {
		o.WebSocketURL = DefaultWebSocketURL
	}
	if o.APIURL == "" {
		o.APIURL = DefaultAPIURL
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
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = defaultIdleTimeout
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
	b.ctl.Subscribe(b.onEvent)
	go b.work()
	return b
}

// Follow connects, reconnects or disconnects the robot as the settings now
// name it: an empty Bot ID leaves the Chat Bridge off.
func (b *Bridge) Follow() {
	cb := b.o.Settings.Get().ChatBridge
	robot := ""
	if cb.On() {
		robot = cb.BotID + "\x00" + cb.Secret
	}
	b.mu.Lock()
	if b.stopped || robot == b.robot {
		b.mu.Unlock()
		return
	}
	old := b.wecom
	b.robot, b.wecom, b.api = robot, nil, nil
	if robot != "" {
		b.api = newChannelAPI(b.o.APIURL, cb.BotID, cb.Secret, b.o.Now)
		if b.tools == nil {
			t, err := startToolServer(b)
			if err != nil {
				b.o.Log("the droi-chat tools cannot listen: " + err.Error())
			} else {
				b.tools = t
			}
		}
		b.wecom = &longConnection{
			url: b.o.WebSocketURL, botID: cb.BotID, secret: cb.Secret,
			ping: b.o.PingInterval, delay: b.o.ReconnectDelay, max: b.o.MaxReconnectDelay,
			onPush: b.push, onTaken: b.o.OnChange, log: b.o.Log,
		}
		b.wecom.start()
	}
	b.mu.Unlock()
	if old != nil {
		old.close()
	}
}

// Status is the robot's long connection; zero while the Chat Bridge is off.
func (b *Bridge) Status() Status {
	b.mu.Lock()
	wc := b.wecom
	b.mu.Unlock()
	if wc == nil {
		return Status{}
	}
	return wc.status()
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
	b.threadOps.Wait()
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
	if f.Cmd == "aibot_event_callback" {
		b.handleEvent(f)
		return
	}
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
	th := b.threads[directThread(user).key]
	if th == nil {
		th = directThread(user)
		b.threads[th.key] = th
	}
	th.reqID, th.reqAt = f.Headers.ReqID, b.o.Now()
	b.mu.Unlock()
	if rest, ok := cutNew(textOf(m)); ok {
		trigger := newDirectTrigger(user, rest)
		b.run(th, func() { b.startOver(th, trigger) })
		return
	}
	trigger := directTrigger(user, textOf(m))
	b.run(th, func() { b.deliver(th, trigger) })
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
func (b *Bridge) deliver(th *thread, text string) { b.deliverTo(th, text, false) }

// startOver archives the Chat Thread's Chat Session and sends text to a new
// one, for !new.
func (b *Bridge) startOver(th *thread, text string) { b.deliverTo(th, text, true) }

func (b *Bridge) deliverTo(th *thread, text string, fresh bool) {
	ctx, cancel := b.opContext()
	defer cancel()
	if err := b.ready(ctx); err != nil {
		b.o.Log(err.Error())
		return
	}
	b.mu.Lock()
	id, loaded := th.sessionID, th.loaded
	th.epoch++
	if th.idle != nil {
		th.idle.Stop()
	}
	b.mu.Unlock()
	if fresh && id != "" {
		b.archive(ctx, th, id)
		id = ""
	}
	if id != "" && !loaded {
		err := b.loadSession(ctx, th, id)
		if errors.Is(err, controller.ErrSessionNotFound) {
			b.mu.Lock()
			th.sessionID = ""
			b.mu.Unlock()
			id = ""
		} else if err != nil {
			b.o.Log("cannot load the Chat Session: " + err.Error())
			return
		}
	}
	if id == "" {
		var err error
		if id, err = b.startSession(ctx, th); err != nil {
			b.o.Log("cannot start a Chat Session: " + err.Error())
			return
		}
		b.mu.Lock()
		if th.archived {
			text = archivedNote + text
			th.archived = false
		}
		b.mu.Unlock()
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
	token, server, err := b.issueToken(th)
	if err != nil {
		return "", err
	}
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
		Tags:           []protocol.SessionTag{{Name: sessions.ChatTag, Metadata: th.metadata()}},
		MCPServers:     []json.RawMessage{server},
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		delete(b.tokens, token)
		return "", err
	}
	b.useToken(th, token)
	th.sessionID, th.loaded = res.SessionID, true
	return res.SessionID, nil
}

// metadata names the Chat Thread in its Chat Session's droi.chat tag.
func (th *thread) metadata() map[string]string {
	if th.kind == threadKindChannel {
		return map[string]string{"kind": threadKindChannel, "topicId": th.topicID, "postId": th.postID, "channel": th.channel}
	}
	return map[string]string{"kind": threadKindDirect, "userId": th.userID}
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
