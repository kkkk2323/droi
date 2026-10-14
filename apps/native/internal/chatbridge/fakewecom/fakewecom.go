// Package fakewecom is the Fake WeCom: a stand-in for the WeCom smart
// robot's long connection and its HTTPS API that the Chat Bridge's tests,
// and the window's, run against. It speaks the frames the real server does
// (aibot_subscribe, ping, aibot_msg_callback, aibot_event_callback, the
// replies without a cmd) and, like the real server, refuses a handshake
// whose Sec-WebSocket-* header names are not spelt WeCom's way. Its HTTPS
// API (api.go) holds WeCom Channels with their posts and replies.
package fakewecom

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Robot is the robot the Fake WeCom accepts subscriptions for.
type Robot struct {
	BotID  string
	Secret string
}

// Frame is a frame the Chat Bridge sent.
type Frame struct {
	Cmd   string
	ReqID string
	Body  json.RawMessage
}

// DirectMessage is a text a user writes to the robot in a direct message.
// MsgID and ReqID are made up when empty.
type DirectMessage struct {
	MsgID string
	ReqID string
	From  string
	Text  string
}

type failure struct {
	errcode int
	errmsg  string
}

// WeCom is a running Fake WeCom.
type WeCom struct {
	// URL is the long connection's address, for the Chat Bridge's settable
	// WebSocket URL.
	URL string
	// APIURL is the HTTPS API's base, for the Chat Bridge's settable API URL.
	APIURL string

	t     testing.TB
	robot Robot

	mu          sync.Mutex
	changed     chan struct{}
	conn        *websocket.Conn
	handshakes  int
	connections int
	refused     int
	frames      []Frame
	fail        map[string][]failure

	now      func() time.Time
	tokens   map[string]time.Time
	channels map[string]string
	posts    map[string][]*Post
	replies  map[string][]*Post
	calls    []APICall
	apiFail  map[string][]failure
	emoji    map[string][]int
}

// Start runs a Fake WeCom for the test and stops it when the test ends.
func Start(t testing.TB, r Robot) *WeCom {
	t.Helper()
	w := &WeCom{
		t: t, robot: r, changed: make(chan struct{}), fail: map[string][]failure{},
		tokens: map[string]time.Time{}, channels: map[string]string{},
		posts: map[string][]*Post{}, replies: map[string][]*Post{}, apiFail: map[string][]failure{},
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(w.serve))
	srv.Listener = recordingListener{srv.Listener}
	srv.Config.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		return context.WithValue(ctx, connKey{}, c)
	}
	srv.Start()
	w.URL = "ws://" + srv.Listener.Addr().String()
	api := httptest.NewServer(http.HandlerFunc(w.serveAPI))
	w.APIURL = api.URL
	t.Cleanup(func() {
		w.Drop()
		srv.CloseClientConnections()
		srv.Close()
		api.Close()
	})
	return w
}

func (w *WeCom) notify() {
	close(w.changed)
	w.changed = make(chan struct{})
}

// waitFor waits until ok holds, calling it with w.mu held.
func (w *WeCom) waitFor(what string, ok func() bool) {
	w.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		w.mu.Lock()
		done, ch := ok(), w.changed
		w.mu.Unlock()
		if done {
			return
		}
		select {
		case <-ch:
		case <-deadline:
			w.t.Fatalf("fake WeCom: no %s", what)
		}
	}
}

type connKey struct{}

func (w *WeCom) serve(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	w.handshakes++
	w.notify()
	w.mu.Unlock()
	head := ""
	if c, ok := r.Context().Value(connKey{}).(*recordingConn); ok {
		head = c.head()
	}
	// WeCom answers 404 to Go's canonical Sec-Websocket-Key.
	if !strings.Contains(head, "\r\nSec-WebSocket-Key:") || !strings.Contains(head, "\r\nSec-WebSocket-Version:") {
		w.mu.Lock()
		w.refused++
		w.notify()
		w.mu.Unlock()
		http.NotFound(rw, r)
		return
	}
	c, err := websocket.Accept(rw, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 22)
	ctx := context.Background()
	subscribed := false
	defer func() {
		w.mu.Lock()
		if w.conn == c {
			w.conn = nil
			w.notify()
		}
		w.mu.Unlock()
		c.CloseNow()
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var f struct {
			Cmd     string `json:"cmd"`
			Headers struct {
				ReqID string `json:"req_id"`
			} `json:"headers"`
			Body json.RawMessage `json:"body"`
		}
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		w.mu.Lock()
		w.frames = append(w.frames, Frame{Cmd: f.Cmd, ReqID: f.Headers.ReqID, Body: f.Body})
		fail, failed := failure{}, false
		if q := w.fail[f.Cmd]; len(q) > 0 {
			fail, failed = q[0], true
			w.fail[f.Cmd] = q[1:]
		}
		w.notify()
		w.mu.Unlock()
		errcode, errmsg := 0, "ok"
		switch {
		case failed:
			errcode, errmsg = fail.errcode, fail.errmsg
		case f.Cmd == "aibot_subscribe":
			var b struct {
				BotID  string `json:"bot_id"`
				Secret string `json:"secret"`
			}
			_ = json.Unmarshal(f.Body, &b)
			if b.BotID != w.robot.BotID || b.Secret != w.robot.Secret {
				errcode, errmsg = 853000, "invalid bot_id or secret"
			}
		case !subscribed && f.Cmd != "ping":
			errcode, errmsg = 40001, "not subscribed"
		}
		out, _ := json.Marshal(map[string]any{"headers": map[string]string{"req_id": f.Headers.ReqID}, "errcode": errcode, "errmsg": errmsg})
		if err := c.Write(ctx, websocket.MessageText, out); err != nil {
			return
		}
		if f.Cmd == "aibot_subscribe" && errcode == 0 {
			subscribed = true
			w.mu.Lock()
			old := w.conn
			w.conn = c
			w.connections++
			w.notify()
			w.mu.Unlock()
			// One live connection per robot: a new subscription ends the old one.
			if old != nil && old != c {
				old.CloseNow()
			}
		}
	}
}

// Subscriptions is how many times the robot subscribed.
func (w *WeCom) Subscriptions() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connections
}

// Handshakes is how many times anything tried to connect.
func (w *WeCom) Handshakes() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.handshakes
}

// Refused is how many handshakes were refused for their header spelling.
func (w *WeCom) Refused() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.refused
}

// Connected reports a subscribed connection.
func (w *WeCom) Connected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn != nil
}

// WaitForSubscription waits until the robot has subscribed n times in all
// and is connected.
func (w *WeCom) WaitForSubscription(n int) {
	w.t.Helper()
	w.waitFor("subscription", func() bool { return w.connections >= n && w.conn != nil })
}

// WaitForDisconnect waits until no connection is subscribed.
func (w *WeCom) WaitForDisconnect() {
	w.t.Helper()
	w.waitFor("disconnect", func() bool { return w.conn == nil })
}

// Frames are the frames of cmd received so far.
func (w *WeCom) Frames(cmd string) []Frame {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []Frame
	for _, f := range w.frames {
		if f.Cmd == cmd {
			out = append(out, f)
		}
	}
	return out
}

// WaitForFrame waits for the nth (from 1) frame of cmd.
func (w *WeCom) WaitForFrame(cmd string, nth int) Frame {
	w.t.Helper()
	var got Frame
	w.waitFor(cmd+" frame", func() bool {
		n := 0
		for _, f := range w.frames {
			if f.Cmd == cmd {
				if n++; n == nth {
					got = f
					return true
				}
			}
		}
		return false
	})
	return got
}

// Fail answers the next frame of cmd with a WeCom error, such as a rate
// limit, in place of ok.
func (w *WeCom) Fail(cmd string, errcode int, errmsg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fail[cmd] = append(w.fail[cmd], failure{errcode, errmsg})
}

// Drop ends the current connection, as a network drop.
func (w *WeCom) Drop() {
	w.mu.Lock()
	c := w.conn
	w.mu.Unlock()
	if c != nil {
		c.CloseNow()
	}
}

// Push sends a frame on the subscribed connection, waiting for one, and
// returns its req_id (made up when empty).
func (w *WeCom) Push(cmd, reqID string, body any) string {
	w.t.Helper()
	if reqID == "" {
		reqID = randomID()
	}
	w.waitFor("subscribed connection", func() bool { return w.conn != nil })
	w.mu.Lock()
	c := w.conn
	w.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"cmd": cmd, "headers": map[string]string{"req_id": reqID}, "body": body})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		w.t.Fatalf("fake WeCom: push %s: %v", cmd, err)
	}
	return reqID
}

// SendDirectMessage pushes a text direct message from a user, as an
// aibot_msg_callback with chattype single, and returns its req_id.
func (w *WeCom) SendDirectMessage(m DirectMessage) string {
	w.t.Helper()
	if m.MsgID == "" {
		m.MsgID = randomID()
	}
	return w.Push("aibot_msg_callback", m.ReqID, map[string]any{
		"msgid":        m.MsgID,
		"aibotid":      w.robot.BotID,
		"chattype":     "single",
		"from":         map[string]string{"userid": m.From},
		"msgtype":      "text",
		"response_url": "https://qyapi.weixin.qq.com/cgi-bin/aibot/response?response_code=fake",
		"text":         map[string]string{"content": m.Text},
	})
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// recordingListener keeps each connection's request head as it came over
// the wire: net/http canonicalizes header names before a handler sees them.
type recordingListener struct{ net.Listener }

func (l recordingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &recordingConn{Conn: c}, nil
}

type recordingConn struct {
	net.Conn
	mu   sync.Mutex
	buf  bytes.Buffer
	done bool
}

func (c *recordingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.mu.Lock()
	if !c.done && n > 0 {
		c.buf.Write(p[:n])
		if bytes.Contains(c.buf.Bytes(), []byte("\r\n\r\n")) || c.buf.Len() > 64<<10 {
			c.done = true
		}
	}
	c.mu.Unlock()
	return n, err
}

func (c *recordingConn) head() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
