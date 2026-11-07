package host

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// kernelLinkBackoff is how long a failed dial waits before trying again, in
// order: short at first, because a Kernel that just printed its handshake
// answers at once, then long enough that one which cannot be reached is not
// dialled in a loop.
var kernelLinkBackoff = []time.Duration{
	200 * time.Millisecond,
	400 * time.Millisecond,
	800 * time.Millisecond,
	1600 * time.Millisecond,
	3200 * time.Millisecond,
}

// kernelLinkDialTimeout bounds one dial: a Kernel that printed its handshake
// accepts at once, so a dial this slow is one to retry.
const kernelLinkDialTimeout = 30 * time.Second

// KernelLinkOptions are how a KernelLink is made.
type KernelLinkOptions struct {
	// Client is the X-Droi-Client word these connections carry: "window" for
	// the desktop app's own, "phone" for the Phone App's.
	Client string
	// Token answers the live kernel's token, which a Client dials with. It is
	// the Supervisor's Token, which is "" when no kernel runs.
	Token func() string
	// OnClient hears each generation's connection once, after it is dialled.
	OnClient func(c *kernel.Client, generation int)
	// OnGone hears, once per connection, that a generation's connection ended:
	// the Kernel exited, the socket did, a newer generation replaced it, or the
	// Supervisor stopped. It can arrive after OnClient of a later generation, so
	// a consumer compares generations rather than relying on the order.
	OnGone func(generation int, err error)
	// Dial replaces the dial, for a test.
	Dial func(ctx context.Context, generation int, url, token string) (*kernel.Client, error)
	// Backoff replaces kernelLinkBackoff, for a test.
	Backoff []time.Duration
	// Log hears what the link did and why it could not.
	Log func(string)
}

// KernelLink keeps one live kernel.Client for a Supervisor's current generation.
//
// A Client is one connection to one Kernel process — that process's token, the
// port it reported — so a restarted Kernel means a new Client, and nothing
// inside Client can know that: only the Supervisor knows the process changed.
// This is the small part in between. Give it a Supervisor's states
// (s.OnState = l.Use) and it dials each generation once, drops a connection that
// arrives for a generation the Supervisor has already left, and says which
// connection is current.
//
// It does not open conversations. The consumer does — a Store knows which ones
// the window has open, and this does not — and all this guarantees is that what
// it hands over belongs to the current generation and carries the right
// X-Droi-Client.
type KernelLink struct {
	opts KernelLinkOptions

	mu sync.Mutex
	// cur is the live connection and curGen the generation it belongs to.
	// Whoever takes a connection out of cur is the one that reports it gone,
	// which is what makes OnGone fire once per connection.
	cur    *kernel.Client
	curGen int
	// want is the generation the Supervisor last reported running; 0 when it
	// runs none. url and token are what that generation answers on.
	want  int
	url   string
	token string
	// dials is bumped by every dial and by everything that makes a dial in
	// flight stale (a new generation, a stop, Close), so a dial that lands late
	// cannot install itself. attempt counts the failed dials of the wanted
	// generation, for the backoff.
	dials   int
	attempt int
	closed  bool
	retry   *time.Timer
	cancel  context.CancelFunc
}

// NewKernelLink is a link with nothing dialled.
func NewKernelLink(o KernelLinkOptions) *KernelLink {
	if len(o.Backoff) == 0 {
		o.Backoff = kernelLinkBackoff
	}
	return &KernelLink{opts: o}
}

// Use is what a Supervisor's OnState calls: it turns states into connections.
// It never blocks on the network, so the Supervisor's own callbacks are not held
// up by a dial.
//
// Two states matter. A running state of a new generation starts a dial.
// "stopped" ends the connection and waits for the next running state;
// "restarting" leaves it alone, because the Kernel it belonged to is the one
// exiting and its Done already says so.
func (l *KernelLink) Use(st KernelState) {
	switch st.Status {
	case KernelRunning:
		l.useRunning(st)
	case KernelStopped:
		l.stopped()
	}
}

func (l *KernelLink) useRunning(st KernelState) {
	l.mu.Lock()
	if l.closed || st.Port == 0 || st.Generation == 0 || st.Generation == l.want {
		l.mu.Unlock()
		return
	}
	old, oldGen := l.takeLocked()
	l.want = st.Generation
	l.url = st.RPCURL()
	l.token = l.tokenOf()
	l.attempt = 0
	serial := l.invalidateLocked()
	l.mu.Unlock()

	l.hangUp(old, oldGen, nil)
	go l.dial(serial)
}

// stopped is the Supervisor saying no Kernel runs: the connection ends, and the
// link waits for the next running state rather than retrying.
func (l *KernelLink) stopped() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	old, oldGen := l.takeLocked()
	l.want = 0
	l.invalidateLocked()
	l.mu.Unlock()
	l.hangUp(old, oldGen, nil)
}

// takeLocked takes the live connection out of cur, for the caller to report.
func (l *KernelLink) takeLocked() (*kernel.Client, int) {
	c, gen := l.cur, l.curGen
	l.cur, l.curGen = nil, 0
	return c, gen
}

// invalidateLocked makes every dial in flight and every pending retry stale,
// and answers the serial the next dial runs under.
func (l *KernelLink) invalidateLocked() int {
	l.dials++
	if l.retry != nil {
		l.retry.Stop()
		l.retry = nil
	}
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	return l.dials
}

// hangUp closes a connection taken out of cur and reports it gone.
func (l *KernelLink) hangUp(c *kernel.Client, gen int, cause error) {
	if c == nil {
		return
	}
	_ = c.Close()
	if l.opts.OnGone != nil {
		l.opts.OnGone(gen, cause)
	}
}

// tokenOf asks the Supervisor for the live token: a link built before the
// kernel runs still dials the one it is told about.
func (l *KernelLink) tokenOf() string {
	if l.opts.Token == nil {
		return ""
	}
	return l.opts.Token()
}

// dial opens one connection for the generation it was started for, and hands it
// over only if that generation is still the one the Supervisor runs.
func (l *KernelLink) dial(serial int) {
	l.mu.Lock()
	if l.closed || serial != l.dials {
		l.mu.Unlock()
		return
	}
	gen, url, token := l.want, l.url, l.token
	ctx, cancel := context.WithTimeout(context.Background(), kernelLinkDialTimeout)
	l.cancel = cancel
	l.mu.Unlock()
	defer cancel()

	var client *kernel.Client
	var err error
	if l.opts.Dial != nil {
		client, err = l.opts.Dial(ctx, gen, url, token)
	} else {
		client, err = kernel.Dial(ctx, kernel.Options{URL: url, Token: token, Client: l.opts.Client})
	}

	l.mu.Lock()
	stale := l.closed || serial != l.dials
	if !stale {
		l.cancel = nil
	}
	if err != nil || stale {
		if err != nil && !stale {
			l.retryLocked(gen, err)
		}
		l.mu.Unlock()
		if client != nil {
			// A connection for a generation the Supervisor has left: nothing
			// else would ever close it.
			_ = client.Close()
		}
		return
	}
	l.cur, l.curGen = client, gen
	l.mu.Unlock()

	if l.opts.OnClient != nil {
		l.opts.OnClient(client, gen)
	}
	go l.watch(client)
}

// retryLocked dials the same generation again after its next backoff, or stops
// when that generation has used them all: the Supervisor's next "running" is
// what starts again, and it is not this link's job to restart a kernel.
func (l *KernelLink) retryLocked(gen int, cause error) {
	l.attempt++
	l.logf("kernel link: dial for generation %d failed (attempt %d): %v", gen, l.attempt, cause)
	if l.attempt > len(l.opts.Backoff) {
		l.logf("kernel link: generation %d could not be dialled after %d attempts; waiting for the next generation", gen, l.attempt)
		return
	}
	serial := l.dials
	l.retry = time.AfterFunc(l.opts.Backoff[l.attempt-1], func() {
		l.mu.Lock()
		if l.closed || serial != l.dials {
			l.mu.Unlock()
			return
		}
		l.retry = nil
		next := l.invalidateLocked()
		l.mu.Unlock()
		l.dial(next)
	})
}

// watch reports a connection that ended on its own, when it is still current:
// one that a new generation, a stop or Close took out was reported there.
func (l *KernelLink) watch(c *kernel.Client) {
	<-c.Done()
	l.mu.Lock()
	if l.closed || l.cur != c {
		l.mu.Unlock()
		return
	}
	_, gen := l.takeLocked()
	l.mu.Unlock()
	if l.opts.OnGone != nil {
		l.opts.OnGone(gen, c.Err())
	}
}

// Current is the live connection and its generation; nil when there is none.
func (l *KernelLink) Current() (*kernel.Client, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cur, l.curGen
}

// Close ends the link: the connection is closed, no dial is made or retried,
// and no callback starts after this returns. Calling it twice is harmless.
func (l *KernelLink) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	c, _ := l.takeLocked()
	l.want = 0
	l.invalidateLocked()
	l.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

func (l *KernelLink) logf(format string, args ...any) {
	if l.opts.Log != nil {
		l.opts.Log(fmt.Sprintf(format, args...))
	}
}
