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
	"fmt"
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
	// tracked are the goroutines Stop waits for: the Chat Threads'
	// operation queues and the card clicks being answered.
	tracked sync.WaitGroup

	mu          sync.Mutex
	stopped     bool
	robot       string
	wecom       *longConnection
	status      Status
	api         *wecomAPI
	tools       *toolServer
	chatThreads map[string]*chatThread
	tokens      map[string]*chatThread
	seen        recent
	media       mediaStore
	// synced: the Chat Thread map was rebuilt from the Daemon's Sessions
	// since the Chat Bridge last connected to it.
	synced  bool
	syncMu  sync.Mutex
	prompts promptBook
}

// State is how far the Chat Bridge is connected to WeCom.
type State int

const (
	// Off: no robot is set.
	Off State = iota
	Connecting
	Connected
	// NotConnected: the last attempt failed or the connection dropped; the
	// Chat Bridge tries again, unless another connection took over the
	// robot (Reason says so), when only Restart connects again.
	NotConnected
)

// Status is the Chat Bridge's connection, for Settings.
type Status struct {
	State State
	// Reason is why it is NotConnected: WeCom's answer or the network's
	// error. It never holds the Secret.
	Reason string
}

// chatThread is a Chat Thread and its Chat Session.
type chatThread struct {
	key  string
	kind chatThreadKind
	// userID is the other side of a direct message.
	userID string
	// topicID and postID are a Channel post's; channelName is the
	// Channel's name.
	topicID, postID, channelName string
	// read are the replies under the post read_thread showed Droid.
	read      map[string]bool
	sessionID string
	// loaded: this Chat Bridge started or loaded the Chat Session, so it
	// has the droi-chat server of token.
	loaded bool
	token  string
	// archivedBefore: the Chat Thread's last Chat Session was archived,
	// which its next one is told.
	archivedBefore bool
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

// chatThreadKind is what a Chat Thread is: its droi.chat tag's kind.
type chatThreadKind string

const (
	kindDirectMessage chatThreadKind = "direct"
	kindChannelPost   chatThreadKind = "channel"
)

const (
	replyWindow        = 24 * time.Hour
	defaultIdleTimeout = 15 * time.Second
	deliverTimeout     = 2 * time.Minute
	// maxRefusedUsers is how many refused senders the settings keep, the
	// newest.
	maxRefusedUsers = 50
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
		o:           o,
		ctl:         controller.New(o.Daemon),
		pushes:      make(chan frame, 256),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		chatThreads: map[string]*chatThread{},
		tokens:      map[string]*chatThread{},
	}
	b.ctl.Subscribe(b.onEvent)
	b.ctl.Subscribe(b.promptEvent)
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
	b.robot, b.wecom, b.api = robot, nil, nil
	b.status = Status{State: Off}
	if robot != "" {
		b.api = newWeComAPI(b.o.APIURL, cb.BotID, cb.Secret, b.o.Now)
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
	b.tracked.Wait()
	_ = b.ctl.Close()
}

func (b *Bridge) push(f frame) {
	// A click is answered within WeCom's 5 s, whatever the events before it
	// are waiting for.
	if isCardClick(f) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.stopped {
			b.tracked.Add(1)
			go func() {
				defer b.tracked.Done()
				b.answerClick(f)
			}()
		}
		return
	}
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
	th := b.chatThreadFor(directChatThread(user))
	th.reqID, th.reqAt = f.Headers.ReqID, b.o.Now()
	b.mu.Unlock()
	media := b.keepDirectMedia(th, m.MsgID, f.Body)
	if rest, ok := cutNew(textOf(m)); ok {
		trigger := newDirectTrigger(user, rest) + media
		b.run(th, func() { b.startOver(th, trigger) })
		return
	}
	text := textOf(m)
	trigger := directTrigger(user, text) + media
	b.run(th, func() {
		// A message with files is for Droid, not an answer.
		if media != "" || !b.answerInChatThread(th, user, text) {
			b.deliver(th, trigger)
		}
	})
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
		if n := len(s.ChatBridge.RefusedUsers); n > maxRefusedUsers {
			s.ChatBridge.RefusedUsers = slices.Clone(s.ChatBridge.RefusedUsers[n-maxRefusedUsers:])
		}
	})
	go b.o.OnChange()
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

// working reports whether the Chat Thread's Chat Session is in a turn.
func (b *Bridge) working(th *chatThread) bool {
	b.mu.Lock()
	id := th.sessionID
	b.mu.Unlock()
	if id == "" {
		return false
	}
	s := b.ctl.Store().Session(id)
	return s != nil && s.WorkingState() != protocol.DroidWorkingStateIdle
}

// deliver sends the Chat Thread's Chat Session one message, starting the
// Session first when the Chat Thread has none. A message to a Session in a
// turn goes into that turn.
func (b *Bridge) deliver(th *chatThread, text string) { b.deliverTo(th, text, false) }

// startOver archives the Chat Thread's Chat Session and sends text to a new
// one, for !new.
func (b *Bridge) startOver(th *chatThread, text string) { b.deliverTo(th, text, true) }

func (b *Bridge) deliverTo(th *chatThread, text string, archiveFirst bool) {
	ctx, cancel := b.opContext(deliverTimeout)
	defer cancel()
	if err := b.ready(ctx); err != nil {
		b.notTaken(th, err)
		return
	}
	b.mu.Lock()
	id, loaded := th.sessionID, th.loaded
	th.epoch++
	if th.idle != nil {
		th.idle.Stop()
	}
	b.mu.Unlock()
	if archiveFirst && id != "" {
		b.archiveChatSession(ctx, th, id)
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
			b.notTaken(th, &notTakenError{"the Daemon did not load its Chat Session", fmt.Errorf("cannot load the Chat Session: %w", err)})
			return
		}
	}
	if id == "" {
		var err error
		if id, err = b.startSession(ctx, th); err != nil {
			b.notTaken(th, fmt.Errorf("cannot start a Chat Session: %w", err))
			return
		}
		b.mu.Lock()
		if th.archivedBefore {
			text = archivedNote + text
			th.archivedBefore = false
		}
		b.mu.Unlock()
	}
	p := protocol.AddUserMessageParams{SessionID: id, Text: text}
	if b.working(th) {
		p.QueuePlacement = protocol.QueuePlacementEndOfTurn
	}
	if _, err := b.ctl.AddUserMessage(ctx, p); err != nil {
		b.notTaken(th, fmt.Errorf("cannot send the Chat Session its message: %w", err))
	}
}

// notTakenError is why a Chat Thread's message did not reach its Chat
// Session: err for the log, and why for the Chat Thread, which never sees
// err's details.
type notTakenError struct {
	why string
	err error
}

func (e *notTakenError) Error() string { return e.err.Error() }
func (e *notTakenError) Unwrap() error { return e.err }

// notTaken logs why th's message did not reach its Chat Session and tells
// the Chat Thread in one line.
func (b *Bridge) notTaken(th *chatThread, err error) {
	b.o.Log(err.Error())
	b.mu.Lock()
	stopped := b.stopped
	b.mu.Unlock()
	if stopped {
		return
	}
	why := "the Daemon did not take it"
	var nt *notTakenError
	if errors.As(err, &nt) {
		why = nt.why
	}
	ctx, cancel := b.opContext(fetchTimeout)
	defer cancel()
	if err := b.sendToChatThread(ctx, th, "Droid could not take this message: "+why+"."); err != nil {
		b.o.Log("cannot tell WeCom a message was not taken: " + err.Error())
	}
}

func (b *Bridge) startSession(ctx context.Context, th *chatThread) (string, error) {
	s := b.o.Settings.Get()
	cb := s.ChatBridge
	if cb.Workspace == "" {
		return "", &notTakenError{"the Chat Bridge has no Workspace set", errors.New("the Chat Bridge has no Workspace")}
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
func (th *chatThread) metadata() map[string]string {
	if th.kind == kindChannelPost {
		return map[string]string{"kind": string(kindChannelPost), "topicId": th.topicID, "postId": th.postID, "channel": th.channelName}
	}
	return map[string]string{"kind": string(kindDirectMessage), "userId": th.userID}
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// connection is the robot's HTTPS API and long connection, or
// errNotConnected while no robot is set.
func (b *Bridge) connection() (*wecomAPI, *longConnection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.api == nil || b.wecom == nil {
		return nil, nil, errNotConnected
	}
	return b.api, b.wecom, nil
}

// channelOnly is the error of what works only in a WeCom Channel, nil in
// a Channel's Chat Thread.
func (th *chatThread) channelOnly(what string) error {
	if th.kind == kindChannelPost {
		return nil
	}
	return fmt.Errorf("%s: only in a WeCom Channel, and this Chat Thread is a direct message", what)
}

// chatThreadOf is the Chat Thread a droi-chat token was given for.
func (b *Bridge) chatThreadOf(token string) *chatThread {
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
