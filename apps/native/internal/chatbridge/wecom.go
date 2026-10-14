package chatbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// DefaultWebSocketURL is WeCom's long connection for smart robots.
const DefaultWebSocketURL = "wss://openws.work.weixin.qq.com"

const (
	defaultPingInterval      = 30 * time.Second
	defaultReconnectDelay    = time.Second
	defaultMaxReconnectDelay = time.Minute
	callTimeout              = 10 * time.Second
	dialTimeout              = 15 * time.Second
)

// WeComError is an error WeCom answered a frame with, a rate limit among
// them.
type WeComError struct {
	Code    int
	Message string
}

func (e *WeComError) Error() string { return fmt.Sprintf("WeCom error %d: %s", e.Code, e.Message) }

var errNotConnected = errors.New("not connected to WeCom")

// takenOver is why the Chat Bridge gave up the robot: another connection
// subscribed it, and WeCom keeps one per robot. The Chat Bridge does not
// take it back, which would only take turns with the other.
const takenOver = "another connection took over the robot, so the Chat Bridge stopped; Restart takes it back"

// frame is a frame on the long connection. The server's answer to a frame
// has no cmd, and errcode in place of a body.
type frame struct {
	Cmd     string `json:"cmd,omitempty"`
	Headers struct {
		ReqID string `json:"req_id"`
	} `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	Errcode *int            `json:"errcode,omitempty"`
	Errmsg  string          `json:"errmsg,omitempty"`
}

// exactCase sends the handshake's header names as WeCom's server needs
// them: it matches them case-sensitively and answers 404 to Go's canonical
// Sec-Websocket-Key. Header.Set would canonicalize them again.
type exactCase struct{ next http.RoundTripper }

func (e exactCase) RoundTrip(r *http.Request) (*http.Response, error) {
	for canon, exact := range map[string]string{
		"Sec-Websocket-Key":        "Sec-WebSocket-Key",
		"Sec-Websocket-Version":    "Sec-WebSocket-Version",
		"Sec-Websocket-Extensions": "Sec-WebSocket-Extensions",
	} {
		if v, ok := r.Header[canon]; ok {
			delete(r.Header, canon)
			r.Header[exact] = v
		}
	}
	return e.next.RoundTrip(r)
}

// longConnection keeps the robot's one long connection: it subscribes, pings,
// and connects again with a growing delay after a drop. Frames the server
// pushes go to onPush, in order.
type longConnection struct {
	url, botID, secret string
	ping, delay, max   time.Duration
	onPush             func(frame)
	// onStatus hears the connection come up, or fail and why; not its close.
	onStatus func(connected bool, reason string)
	log      func(string)

	stop chan struct{}
	done chan struct{}

	mu    sync.Mutex
	conn  *websocket.Conn
	taken bool
	// waiting are the answers awaited by req_id; several frames can share one
	// req_id (replies to one callback), and their answers come in order.
	waiting map[string][]chan frame
}

func (l *longConnection) start() {
	l.stop, l.done = make(chan struct{}), make(chan struct{})
	l.waiting = map[string][]chan frame{}
	go l.run()
}

func (l *longConnection) close() {
	close(l.stop)
	<-l.done
}

func (l *longConnection) run() {
	defer close(l.done)
	delay := l.delay
	for {
		if l.connect() {
			delay = l.delay
		}
		l.mu.Lock()
		taken := l.taken
		l.mu.Unlock()
		if taken {
			l.fail(takenOver)
			return
		}
		select {
		case <-l.stop:
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, l.max)
	}
}

// connect runs one connection until it drops; it reports whether the
// subscription succeeded.
func (l *longConnection) connect() bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-l.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
	c, _, err := websocket.Dial(dctx, l.url, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: exactCase{http.DefaultTransport}},
	})
	dcancel()
	if err != nil {
		if ctx.Err() == nil {
			l.fail("cannot reach WeCom: " + err.Error())
		}
		return false
	}
	defer c.CloseNow()
	c.SetReadLimit(4 << 20)
	defer l.dropAnswers(c)
	dropped := make(chan error, 1)
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				dropped <- err
				return
			}
			l.receive(data)
		}
	}()
	if _, err := l.callOn(ctx, c, "aibot_subscribe", "", map[string]string{"bot_id": l.botID, "secret": l.secret}); err != nil {
		if ctx.Err() == nil {
			l.fail("WeCom refused the subscription: " + err.Error())
		}
		return false
	}
	l.mu.Lock()
	l.conn = c
	l.mu.Unlock()
	l.log("connected to WeCom")
	l.onStatus(true, "")
	ticker := time.NewTicker(l.ping)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			go func() {
				if _, err := l.callOn(ctx, c, "ping", "", nil); err != nil && ctx.Err() == nil {
					l.log("ping: " + err.Error())
				}
			}()
		case err := <-dropped:
			l.mu.Lock()
			taken := l.taken
			l.mu.Unlock()
			if ctx.Err() == nil && !taken {
				l.fail("the WeCom connection dropped: " + err.Error())
			}
			return true
		case <-ctx.Done():
			_ = c.Close(websocket.StatusNormalClosure, "")
			return true
		}
	}
}

func (l *longConnection) fail(reason string) {
	l.log(reason)
	l.onStatus(false, reason)
}

func (l *longConnection) receive(data []byte) {
	var f frame
	if json.Unmarshal(data, &f) != nil {
		return
	}
	if f.Cmd == "aibot_event_callback" && isDisconnectedEvent(f.Body) {
		l.mu.Lock()
		l.taken = true
		c := l.conn
		l.mu.Unlock()
		if c != nil {
			c.CloseNow()
		}
		return
	}
	if f.Cmd != "" {
		l.onPush(f)
		return
	}
	l.mu.Lock()
	var ch chan frame
	if q := l.waiting[f.Headers.ReqID]; len(q) > 0 {
		ch = q[0]
		l.waiting[f.Headers.ReqID] = q[1:]
	}
	l.mu.Unlock()
	if ch != nil {
		ch <- f
	}
}

func isDisconnectedEvent(body json.RawMessage) bool {
	var b struct {
		Event struct {
			EventType string `json:"eventtype"`
		} `json:"event"`
	}
	return json.Unmarshal(body, &b) == nil && b.Event.EventType == "disconnected_event"
}

// dropAnswers ends the calls still waiting on connection c.
func (l *longConnection) dropAnswers(c *websocket.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == c {
		l.conn = nil
	}
	for id, q := range l.waiting {
		for _, ch := range q {
			close(ch)
		}
		delete(l.waiting, id)
	}
}

// call sends a frame on the subscribed connection and waits for WeCom's
// answer; reqID "" is a new one.
func (l *longConnection) call(ctx context.Context, cmd, reqID string, body any) (json.RawMessage, error) {
	l.mu.Lock()
	c := l.conn
	l.mu.Unlock()
	if c == nil {
		return nil, errNotConnected
	}
	return l.callOn(ctx, c, cmd, reqID, body)
}

func (l *longConnection) callOn(ctx context.Context, c *websocket.Conn, cmd, reqID string, body any) (json.RawMessage, error) {
	if reqID == "" {
		reqID = uuid.NewString()
	}
	out := map[string]any{"cmd": cmd, "headers": map[string]string{"req_id": reqID}}
	if body != nil {
		out["body"] = body
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	ch := make(chan frame, 1)
	l.mu.Lock()
	l.waiting[reqID] = append(l.waiting[reqID], ch)
	l.mu.Unlock()
	forget := func() {
		l.mu.Lock()
		l.waiting[reqID] = slices.DeleteFunc(l.waiting[reqID], func(x chan frame) bool { return x == ch })
		if len(l.waiting[reqID]) == 0 {
			delete(l.waiting, reqID)
		}
		l.mu.Unlock()
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		forget()
		return nil, err
	}
	select {
	case f, ok := <-ch:
		if !ok {
			return nil, errNotConnected
		}
		if f.Errcode != nil && *f.Errcode != 0 {
			return nil, &WeComError{Code: *f.Errcode, Message: f.Errmsg}
		}
		return f.Body, nil
	case <-ctx.Done():
		forget()
		return nil, fmt.Errorf("WeCom did not answer %s: %w", cmd, ctx.Err())
	}
}
