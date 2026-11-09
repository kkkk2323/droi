package host

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReview229R4OldRefusalMustNotStopNewGeneration(t *testing.T) {
	w := newWatch()
	first := helperSpawn(t, "GO_KERNEL_REFUSE=1", "GORACE=atexit_sleep_ms=0")
	next := helperSpawn(t, "GORACE=atexit_sleep_ms=0")
	var count atomic.Int32
	s := w.supervisor(func(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
		if count.Add(1) == 1 {
			return first(port, token)
		}
		return next(port, token)
	})
	s.HealthEvery = -1
	checked, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var paused atomic.Bool
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer s.Close()
	defer s.Stop()
	defer unblock()
	s.Log = func(line string) {
		if strings.HasPrefix(line, "kernel exited:") && paused.CompareAndSwap(false, true) {
			close(checked)
			<-release // Scheduler pause after stillOurs, without modifying production.
		}
		if strings.HasPrefix(line, "kernel refused to start;") {
			close(finished)
		}
	}
	s.Start()
	select {
	case <-checked:
	case <-time.After(5 * time.Second):
		t.Fatal("old refusal never reached the post-ownership-check log")
	}
	s.Stop()
	s.Start()
	newState := w.waitState(t, KernelRunning)
	s.mu.Lock()
	second := s.child
	s.mu.Unlock()
	defer func() {
		_ = second.cmd.Process.Kill()
		select {
		case <-second.done:
		case <-time.After(time.Second):
			t.Error("probe cleanup could not reap second generation")
		}
	}()
	unblock()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("old refusal never completed")
	}
	if st := s.State(); st.Status != KernelRunning || st.Generation != newState.Generation {
		// A user sees stopped and asks Start once more. The next launch now
		// overwrites s.child without retiring the healthy second generation.
		s.Start()
		third := w.waitState(t, KernelRunning)
		s.Stop()
		select {
		case <-second.done:
			t.Fatalf("old exit-2 overwrote healthy generation %d/pid %d: %+v", newState.Generation, newState.PID, st)
		default:
			t.Fatalf("old exit-2 overwrote generation %d/pid %d with %+v; Start+Stop of generation %d/pid %d orphaned the still-live second process",
				newState.Generation, newState.PID, st, third.Generation, third.PID)
		}
	}
}

type r4OwnedTree struct {
	mu          sync.Mutex
	closed      bool
	kills       int
	afterClosed bool
}

func (tr *r4OwnedTree) kill() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.kills++
	if tr.closed {
		tr.afterClosed = true
	}
}

func (tr *r4OwnedTree) close() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.closed = true
}

func TestReview229R4HealthMustNotKillARetiredTree(t *testing.T) {
	entered, release, watched := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, portString, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	tr := &r4OwnedTree{}
	ch := &kernelChild{cmd: exec.Command("unused"), tree: tr, done: make(chan struct{}), pumped: make(chan struct{})}
	close(ch.pumped)
	s := &KernelSupervisor{child: ch, running: true, HealthEvery: time.Millisecond, HealthFailures: 1}
	go func() { s.watchHealth(ch, port); close(watched) }()
	<-entered
	close(ch.done) // The process exits while its health call is in flight.
	s.Stop()       // The ordinary retire path kills once and releases the handles.
	close(release)
	<-watched
	s.Close()
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.afterClosed {
		t.Fatalf("in-flight health check killed already-retired tree: kills=%d, closed=%v", tr.kills, tr.closed)
	}
}
func TestReview229R4RealOrderlyShutdownDoesNotFallBack(t *testing.T) {
	w := newWatch()
	s := w.supervisor(helperSpawn(t, "GO_KERNEL_SHUTDOWN_EXITS=1", "GORACE=atexit_sleep_ms=0"))
	s.HealthEvery = -1
	var fallback atomic.Int32
	s.Terminate = func(cmd *exec.Cmd) { fallback.Add(1); signalProcess(cmd) }
	s.Start()
	state := w.waitState(t, KernelRunning)
	defer s.Close()
	defer s.Stop()
	s.mu.Lock()
	ch := s.child
	s.mu.Unlock()
	s.AskShutdown = func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/rpc", state.Port), nil)
		if err != nil {
			return err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		return res.Body.Close()
	}
	s.Stop()
	if fallback.Load() != 0 || ch.err != nil {
		t.Fatalf("orderly close fell back or failed: fallback=%d exit=%v", fallback.Load(), ch.err)
	}
}

func TestReview229R4ExitHelper(t *testing.T) {
	code := os.Getenv("REVIEW229_EXIT")
	if code == "" {
		t.Skip("subprocess helper")
	}
	n, _ := strconv.Atoi(code)
	fmt.Fprintln(os.Stderr, "review229 exit", n)
	os.Exit(n)
}

func TestReview229R4CrashCodesBackOff(t *testing.T) {
	for _, code := range []string{"1", "3"} {
		t.Run(code, func(t *testing.T) {
			w := newWatch()
			var mu sync.Mutex
			var launches []time.Time
			s := w.supervisor(func(int, string) (*exec.Cmd, io.ReadCloser, error) {
				mu.Lock()
				launches = append(launches, time.Now())
				mu.Unlock()
				cmd := exec.Command(selfExe(t), "-test.run=^TestReview229R4ExitHelper$")
				cmd.Env = append(os.Environ(), "REVIEW229_EXIT="+code, "GORACE=atexit_sleep_ms=0")
				return cmd, newStdioPipe(cmd), nil
			})
			s.Backoff = []time.Duration{80 * time.Millisecond, 160 * time.Millisecond}
			defer s.Close()
			defer s.Stop()
			s.Start()
			w.waitState(t, KernelRestarting)
			w.waitState(t, KernelRestarting)
			w.waitState(t, KernelRestarting)
			s.Stop()
			mu.Lock()
			defer mu.Unlock()
			if len(launches) < 3 || launches[1].Sub(launches[0]) < 80*time.Millisecond ||
				launches[2].Sub(launches[1]) < 160*time.Millisecond {
				t.Fatalf("exit %s hot-looped: %v", code, launches)
			}
			t.Logf("exit %s: restart gaps %s, %s", code, launches[1].Sub(launches[0]), launches[2].Sub(launches[1]))
		})
	}
}
