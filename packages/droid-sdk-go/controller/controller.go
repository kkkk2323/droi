// Package controller keeps a connection to the Droid Daemon usable and the
// Sessions loaded over it current: it connects and authenticates, reconnects
// after the connection drops and loads the Sessions again, tracks pending
// permission and AskUser prompts (keeping an answer given while a Session's
// worker is gone until the worker is back), and applies every session
// notification to a session.Manager. It is the Go counterpart of the TS
// SDK's DaemonSessionController, for a local Daemon or a Gateway in front of
// one.
//
// Calls that need nothing from the Controller go straight to the generated
// methods of the current droid.Client (see Client).
package controller

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

// Defaults of Config, the TS SDK's for a local Daemon.
const (
	DefaultMaxConnectAttempts   = 15
	DefaultMaxReconnectAttempts = 3
	DefaultReconnectDelay       = time.Second
	DefaultMaxReconnectDelay    = 10 * time.Second
	DefaultReconnectBackoff     = 1.5

	connectPollInterval = time.Second
	// A Daemon that is not listening yet does not use up connect attempts for
	// this long, as it may still be starting.
	preSpawnMaxWait = 2 * time.Minute
)

// Config configures a Controller.
type Config struct {
	// URL of the Daemon's WebSocket (ws://127.0.0.1:<port>) or of a Gateway.
	URL string
	// ResolveURL, when set, is asked for the URL before every connect in
	// place of URL: a Daemon the app supervises moves to a new port when
	// it restarts.
	ResolveURL func(ctx context.Context) (string, error)
	// Credential gives the credential for daemon.authenticate on every
	// connect; it is also the token the Daemon spawns Session workers with.
	// Leave it nil on a trusted transport that needs no authentication.
	Credential func(ctx context.Context) (*droid.Credential, error)
	// MachineID is sent with daemon.initialize_session; "local" by default.
	MachineID string
	// Caller, Surface, Header, RequestTimeout, Record and Logger are passed
	// to every droid.Client (see droid.Options).
	Caller         string
	Surface        string
	Header         http.Header
	RequestTimeout time.Duration
	Record         func(dir droid.Direction, frame []byte)
	Logger         *slog.Logger

	// MaxConnectAttempts bounds Connect, one attempt a second.
	MaxConnectAttempts int
	// After a connection drops, the Controller tries MaxReconnectAttempts
	// times, waiting ReconnectDelay, multiplied by ReconnectBackoff after
	// every attempt up to MaxReconnectDelay. Set MaxReconnectAttempts to -1
	// to not reconnect.
	MaxReconnectAttempts int
	ReconnectDelay       time.Duration
	MaxReconnectDelay    time.Duration
	ReconnectBackoff     float64

	// DefaultMessageLimit is the messageLimit of daemon.load_session when
	// the caller sets none; 0 lets the Daemon choose.
	DefaultMessageLimit int64
}

// Status is the state of the connection.
type Status struct {
	// Connected: the WebSocket is open and, with a Credential, authenticated.
	Connected bool
	// Reconnecting: the connection dropped and the Controller is trying
	// again.
	Reconnecting bool
	// Failure is the last failure, nil once connected.
	Failure *ConnectionError
}

// Controller is safe for use from any goroutine.
type Controller struct {
	cfg    Config
	log    *slog.Logger
	events *eventQueue
	stop   chan struct{}
	// connectMu makes connection attempts one at a time.
	connectMu sync.Mutex

	mu       sync.Mutex
	client   *droid.Client
	cred     *droid.Credential
	gen      int
	status   Status
	closed   bool
	sessions map[string]*tracked
	prompts  prompts
	// store is changed only outside mu: it calls its subscribers on the
	// changing goroutine, and they may call the Controller.
	store        *session.Store
	selfResuming map[string]bool
}

// New makes a Controller; Connect connects it.
func New(cfg Config) *Controller {
	if cfg.MachineID == "" {
		cfg.MachineID = "local"
	}
	if cfg.MaxConnectAttempts <= 0 {
		cfg.MaxConnectAttempts = DefaultMaxConnectAttempts
	}
	if cfg.MaxReconnectAttempts == 0 {
		cfg.MaxReconnectAttempts = DefaultMaxReconnectAttempts
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = DefaultReconnectDelay
	}
	if cfg.MaxReconnectDelay <= 0 {
		cfg.MaxReconnectDelay = DefaultMaxReconnectDelay
	}
	if cfg.ReconnectBackoff < 1 {
		cfg.ReconnectBackoff = DefaultReconnectBackoff
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Controller{
		cfg:          cfg,
		log:          log,
		events:       newEventQueue(),
		stop:         make(chan struct{}),
		sessions:     map[string]*tracked{},
		prompts:      newPrompts(),
		store:        session.NewStore(),
		selfResuming: map[string]bool{},
	}
}

// Subscribe calls fn with every Event, in order, on a goroutine of the
// Controller's own; fn may call the Controller. The returned function
// unsubscribes.
func (c *Controller) Subscribe(fn func(Event)) (unsubscribe func()) {
	return c.events.subscribe(fn)
}

// Store is the state of the Sessions the Controller follows; subscribe to
// it to render them.
func (c *Controller) Store() *session.Store { return c.store }

// Status returns the connection's status.
func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Client returns the current connection, for the methods the Controller
// does not wrap. Session-scoped calls on it first load a Session the
// Controller loaded before and that is not loaded now.
func (c *Controller) Client() (*droid.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClosed
	}
	if c.client == nil {
		return nil, ErrNotConnected
	}
	return c.client, nil
}

// Connect connects and authenticates, trying up to MaxConnectAttempts
// times; it returns at once when connected. A failure that trying again
// cannot fix (see ConnectionError.Retryable) ends it early.
func (c *Controller) Connect(ctx context.Context) error {
	start := time.Now()
	var last *ConnectionError
	for attempt := 0; attempt < c.cfg.MaxConnectAttempts; {
		err := c.connectOnce(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last = err
		if !err.Retryable {
			return err
		}
		if err.Reason != ReasonDaemonNotSpawned || time.Since(start) >= preSpawnMaxWait {
			attempt++
		}
		if attempt < c.cfg.MaxConnectAttempts {
			if err := c.sleep(ctx, connectPollInterval); err != nil {
				return err
			}
		}
	}
	return last
}

func (c *Controller) sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stop:
		return ErrClosed
	}
}

// connectOnce makes one attempt; it is a no-op when connected.
func (c *Controller) connectOnce(ctx context.Context) *ConnectionError {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()
	c.mu.Lock()
	closed, connected := c.closed, c.client != nil
	c.mu.Unlock()
	if closed {
		return &ConnectionError{Reason: ReasonUnknown, Err: ErrClosed}
	}
	if connected {
		return nil
	}
	var cred *droid.Credential
	if c.cfg.Credential != nil {
		var err error
		cred, err = c.cfg.Credential(ctx)
		if err != nil || cred == nil {
			return c.failed(&ConnectionError{Reason: ReasonNoToken, Retryable: true, Err: err})
		}
	}
	url := c.cfg.URL
	if c.cfg.ResolveURL != nil {
		var err error
		if url, err = c.cfg.ResolveURL(ctx); err != nil {
			return c.failed(&ConnectionError{Reason: ReasonDaemonNotSpawned, Retryable: true, Err: err})
		}
	}
	cl, err := droid.Dial(ctx, droid.Options{
		URL:            url,
		Credential:     cred,
		Caller:         c.cfg.Caller,
		Surface:        c.cfg.Surface,
		Header:         c.cfg.Header,
		RequestTimeout: c.cfg.RequestTimeout,
		Record:         c.cfg.Record,
		Logger:         c.cfg.Logger,
		BeforeCall:     c.beforeCall,
	})
	if err != nil {
		return c.failed(classify(err))
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		cl.Close()
		return &ConnectionError{Reason: ReasonUnknown, Err: ErrClosed}
	}
	c.gen++
	gen := c.gen
	c.client, c.cred = cl, cred
	c.status = Status{Connected: true}
	st := c.status
	c.mu.Unlock()

	cl.HandleLater(protocol.ServerRequestRequestPermission, func(r droid.Request) { c.onPermissionRequest(gen, r) })
	cl.HandleLater(protocol.ServerRequestAskUser, func(r droid.Request) { c.onAskUserRequest(gen, r) })
	cl.Subscribe(func(n droid.Notification) { c.onNotification(gen, n) })
	c.events.push(StatusChanged{Status: st})
	go c.watch(gen, cl)
	return nil
}

func (c *Controller) failed(err *ConnectionError) *ConnectionError {
	c.mu.Lock()
	c.status.Failure = err
	st := c.status
	c.mu.Unlock()
	c.events.push(StatusChanged{Status: st})
	return err
}

// current returns the connection if it is still the one of generation gen.
func (c *Controller) current(gen int) *droid.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return nil
	}
	return c.client
}

func (c *Controller) watch(gen int, cl *droid.Client) {
	<-cl.Done()
	c.mu.Lock()
	if c.gen != gen || c.client != cl || c.closed {
		c.mu.Unlock()
		return
	}
	c.client, c.cred = nil, nil
	c.status = Status{
		Reconnecting: c.cfg.MaxReconnectAttempts > 0,
		Failure:      &ConnectionError{Reason: ReasonConnectionLost, Retryable: true, Err: cl.Err()},
	}
	st := c.status
	for _, t := range c.sessions {
		if t.loaded || t.loading != nil {
			t.loaded = false
			t.reload = true
		}
	}
	// Answers to prompts of this connection now wait for the reload.
	c.prompts.markPromptSessionsInactive()
	c.mu.Unlock()
	c.store.MarkAllNotLoaded()
	c.store.ClearAllQueues()
	c.log.Info("droid: connection lost", "err", cl.Err())
	c.events.push(StatusChanged{Status: st})
	if st.Reconnecting {
		go c.reconnect()
	}
}

func (c *Controller) reconnect() {
	ctx, cancel := c.stopContext()
	defer cancel()
	var last *ConnectionError
	for attempt := 0; attempt < c.cfg.MaxReconnectAttempts; attempt++ {
		if c.sleep(ctx, c.reconnectDelay(attempt)) != nil {
			return
		}
		last = c.connectOnce(ctx)
		if last == nil {
			c.reloadSessions()
			return
		}
		c.log.Info("droid: reconnect failed", "attempt", attempt+1, "err", last)
		if !last.Retryable {
			break
		}
	}
	c.mu.Lock()
	if c.client != nil || c.closed {
		c.mu.Unlock()
		return
	}
	c.status.Reconnecting = false
	st := c.status
	c.mu.Unlock()
	c.events.push(StatusChanged{Status: st})
}

func (c *Controller) reconnectDelay(attempt int) time.Duration {
	d := float64(c.cfg.ReconnectDelay) * math.Pow(c.cfg.ReconnectBackoff, float64(attempt))
	return min(time.Duration(d), c.cfg.MaxReconnectDelay)
}

// Close ends the connection and stops reconnecting. Queued events are still
// delivered.
func (c *Controller) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.stop)
	cl := c.client
	c.client = nil
	c.status = Status{}
	c.mu.Unlock()
	var err error
	if cl != nil {
		err = cl.Close()
	}
	c.events.push(StatusChanged{})
	c.events.close()
	return err
}

func (c *Controller) onNotification(gen int, n droid.Notification) {
	if c.current(gen) == nil {
		return
	}
	if n.Method != protocol.NotificationSessionNotification {
		c.events.push(DaemonNotification{Method: n.Method, Params: n.Params})
		return
	}
	var p protocol.SessionNotificationParams
	if err := json.Unmarshal(n.Params, &p); err != nil {
		c.log.Warn("droid: undecodable session notification", "err", err)
		return
	}
	v, err := p.Notification.Value()
	if err != nil {
		c.log.Warn("droid: undecodable session notification", "type", p.Notification.Type, "err", err)
		v = nil
	}
	c.mu.Lock()
	inactive, pending := c.prompts.inactive[p.SessionID], c.prompts.hasPending(p.SessionID)
	c.mu.Unlock()
	c.applyToStore(p, v, inactive, pending)
	c.mu.Lock()
	out := c.applyNotification(p.SessionID, v, p, nil)
	c.mu.Unlock()
	c.push(out)
}

// applyToStore applies a notification to the store. A Session whose worker
// is gone but that still owes the user a prompt stays waiting for it: its
// worker's idle is not the end of the turn.
func (c *Controller) applyToStore(p protocol.SessionNotificationParams, v any, inactive, pending bool) {
	var opts session.HandleOptions
	switch v := v.(type) {
	case *protocol.SessionInactivityNotification, *protocol.SessionProcessExitedNotification:
		opts.PreserveWorkingStateOnInactive = pending
	case *protocol.DroidWorkingStateChangedNotification:
		if v.NewState == protocol.DroidWorkingStateIdle && inactive && pending {
			n, err := protocol.NewSessionNotificationParamsNotification(&protocol.DroidWorkingStateChangedNotification{
				Type: v.Type, NewState: protocol.DroidWorkingStateWaitingForToolConfirmation,
			})
			if err == nil {
				p.Notification = n
			}
		}
	}
	if err := c.store.HandleNotification(p, opts); err != nil {
		c.log.Warn("droid: apply session notification", "err", err)
	}
}
