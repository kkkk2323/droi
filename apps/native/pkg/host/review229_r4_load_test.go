package host

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// A pending RPC is not itself a reason to restart. Two transient health
// failures followed by success must reset the streak, even with that RPC busy.
func TestReview229R4BusyRPCAndTransientHealthFailures(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var checks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			time.Sleep(2 * time.Millisecond)
			n := checks.Add(1)
			if n%3 != 0 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if _, _, err := conn.Read(context.Background()); err != nil {
			return
		}
		close(entered)
		<-release
	}))
	defer server.Close()
	c, err := kernel.Dial(context.Background(), kernel.Options{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/rpc"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan error, 1)
	go func() { called <- c.Call(ctx, "conversation.entries", map[string]int{"id": 7}, nil) }()
	<-entered
	_, portString, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	tr := &r4OwnedTree{}
	ch := &kernelChild{cmd: exec.Command("unused"), tree: tr, done: make(chan struct{})}
	s := &KernelSupervisor{child: ch, running: true, HealthEvery: time.Millisecond, HealthFailures: 3}
	watched := make(chan struct{})
	go func() { s.watchHealth(ch, port); close(watched) }()
	for deadline := time.Now().Add(2 * time.Second); checks.Load() < 30 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	close(ch.done)
	<-watched
	cancel()
	<-called
	tr.mu.Lock()
	defer tr.mu.Unlock()
	t.Logf("busy RPC overlapped %d health checks; kills=%d", checks.Load(), tr.kills)
	if checks.Load() < 30 || tr.kills != 0 {
		t.Fatalf("transient health failures with pending RPC caused a restart")
	}
}
