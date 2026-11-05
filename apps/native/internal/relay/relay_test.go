package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const testID = "abcdefghijklmnopqrstuvwx"

// target stands in for the Gateway.
type target struct {
	*httptest.Server
	release  chan struct{}
	canceled chan struct{}
}

func newTarget(t *testing.T) *target {
	t.Helper()
	tg := &target{release: make(chan struct{}), canceled: make(chan struct{}, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("/meta", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Back", "yes")
		_ = json.NewEncoder(w).Encode(map[string]string{"query": r.URL.RawQuery, "asked": r.Header.Get("X-Asked"), "method": r.Method})
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "first;")
		w.(http.Flusher).Flush()
		select {
		case <-tg.release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "second")
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		h := sha256.New()
		n, _ := io.Copy(h, r.Body)
		fmt.Fprintf(w, "%d %s", n, hex.EncodeToString(h.Sum(nil)))
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		tg.canceled <- struct{}{}
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(64 << 20)
		ctx := context.Background()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if string(data) == "close-me" {
				_ = c.Close(4001, "asked to")
				return
			}
			_ = c.Write(ctx, typ, data)
		}
	})
	tg.Server = httptest.NewServer(mux)
	t.Cleanup(tg.Close)
	return tg
}

// swappable serves whichever Server it holds, so a test can restart one.
type swappable struct{ s atomic.Pointer[Server] }

func (h *swappable) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.s.Load().ServeHTTP(w, r) }

func newRelay(t *testing.T, o ServerOptions) (*httptest.Server, *swappable) {
	t.Helper()
	if o.Logf == nil {
		o.Logf = t.Logf
	}
	h := &swappable{}
	h.s.Store(NewServer(o))
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		h.s.Load().Close()
		srv.Close()
	})
	return srv, h
}

func startAgent(t *testing.T, o AgentOptions) (*Agent, context.CancelFunc) {
	t.Helper()
	if o.ID == "" {
		o.ID = testID
	}
	a := NewAgent(o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return a, cancel
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func connected(t *testing.T, a *Agent) {
	t.Helper()
	waitFor(t, "the Agent to connect", func() bool { return a.State().Status == Connected })
}

func setup(t *testing.T) (base string, tg *target, a *Agent) {
	t.Helper()
	tg = newTarget(t)
	relay, _ := newRelay(t, ServerOptions{})
	a, _ = startAgent(t, AgentOptions{URL: relay.URL, Target: tg.URL, Name: "test", Version: "1.0.0"})
	connected(t, a)
	return ClientBase(relay.URL, testID), tg, a
}

func TestHTTPGoesThroughTheRelay(t *testing.T) {
	base, _, _ := setup(t)
	req, _ := http.NewRequest(http.MethodGet, base+"/meta?token=abc&x=1", nil)
	req.Header.Set("X-Asked", "hello")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Header.Get("X-Back") != "yes" {
		t.Fatalf("status %d, headers %v", resp.StatusCode, resp.Header)
	}
	if got["query"] != "token=abc&x=1" || got["asked"] != "hello" || got["method"] != "GET" {
		t.Fatalf("the Gateway got %v", got)
	}
}

func TestAResponseStreamsAsItComes(t *testing.T) {
	base, tg, _ := setup(t)
	resp, err := http.Get(base + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 6)
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "first;" {
		t.Fatalf("first part %q, %v", buf, err)
	}
	close(tg.release)
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != "second" {
		t.Fatalf("rest %q", rest)
	}
}

func TestALargeUploadArrivesWhole(t *testing.T) {
	base, _, _ := setup(t)
	body := make([]byte, 10<<20+123)
	_, _ = rand.Read(body)
	sum := sha256.Sum256(body)
	resp, err := http.Post(base+"/echo", "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	want := fmt.Sprintf("%d %s", len(body), hex.EncodeToString(sum[:]))
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAWebSocketGoesThroughTheRelay(t *testing.T) {
	base, _, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/ws?token=abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 20)
	big := bytes.Repeat([]byte("x"), 3<<20)
	for _, m := range []struct {
		typ  websocket.MessageType
		data []byte
	}{{websocket.MessageText, []byte(`{"jsonrpc":"2.0"}`)}, {websocket.MessageBinary, []byte{0, 1, 2}}, {websocket.MessageText, big}} {
		if err := c.Write(ctx, m.typ, m.data); err != nil {
			t.Fatal(err)
		}
		typ, data, err := c.Read(ctx)
		if err != nil || typ != m.typ || !bytes.Equal(data, m.data) {
			t.Fatalf("echo: %v %d bytes %v", typ, len(data), err)
		}
	}
	_ = c.Write(ctx, websocket.MessageText, []byte("close-me"))
	_, _, err = c.Read(ctx)
	if code := websocket.CloseStatus(err); code != 4001 {
		t.Fatalf("closed with %d (%v), want the Gateway's 4001", code, err)
	}
}

func TestAPhoneThatGoesAwayCancelsItsRequest(t *testing.T) {
	base, tg, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/slow", nil)
	if _, err := http.DefaultClient.Do(req); err == nil {
		t.Fatal("the request did not time out")
	}
	select {
	case <-tg.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("the Gateway's request was not canceled")
	}
}

func TestAnUnknownComputerIsOffline(t *testing.T) {
	relay, _ := newRelay(t, ServerOptions{})
	resp, err := http.Get(ClientBase(relay.URL, "zzzzzzzzzzzzzzzzzzzzzzzz") + "/meta")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, p := range []string{"/c/short/meta", "/elsewhere"} {
		resp, _ := http.Get(relay.URL + p)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
	resp, _ = http.Get(relay.URL + "/healthz")
	var h map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&h)
	resp.Body.Close()
	if h["ok"] != true || h["protocol"] != float64(2) {
		t.Errorf("healthz %v", h)
	}
}

func TestTheRegistrationKey(t *testing.T) {
	tg := newTarget(t)
	relay, _ := newRelay(t, ServerOptions{Key: "right"})
	wrong, _ := startAgent(t, AgentOptions{URL: relay.URL, Target: tg.URL, Key: "wrong"})
	waitFor(t, "a refusal", func() bool { return wrong.State().Status == Refused })
	if s := wrong.State(); !strings.Contains(s.Err, "registration key") || s.Next.Before(time.Now().Add(time.Minute)) {
		t.Fatalf("state %+v", s)
	}
	right, _ := startAgent(t, AgentOptions{URL: relay.URL, Target: tg.URL, Key: "right", ID: "another-computer-id-1234"})
	connected(t, right)
}

func TestASecondComputerWithTheSameIdReplacesTheFirst(t *testing.T) {
	tg := newTarget(t)
	relay, _ := newRelay(t, ServerOptions{})
	first, _ := startAgent(t, AgentOptions{URL: relay.URL, Target: tg.URL})
	connected(t, first)
	second, _ := startAgent(t, AgentOptions{URL: relay.URL, Target: tg.URL})
	connected(t, second)
	waitFor(t, "the first to be refused", func() bool { return first.State().Status == Refused })
	if s := first.State(); !strings.Contains(s.Err, "another computer") {
		t.Fatalf("state %+v", s)
	}
}

func TestTheAgentComesBackAfterTheRelayRestarts(t *testing.T) {
	tg := newTarget(t)
	relay, h := newRelay(t, ServerOptions{})
	a, _ := startAgent(t, AgentOptions{URL: relay.URL, Target: tg.URL})
	connected(t, a)
	old := h.s.Load()
	h.s.Store(NewServer(ServerOptions{Logf: t.Logf}))
	old.Close()
	waitFor(t, "the Agent to notice", func() bool { return a.State().Status != Connected })
	a.Kick()
	connected(t, a)
	resp, err := http.Get(ClientBase(relay.URL, testID) + "/meta")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestStoppingTheAgentTakesTheComputerOffline(t *testing.T) {
	tg := newTarget(t)
	relay, h := newRelay(t, ServerOptions{})
	a, stop := startAgent(t, AgentOptions{URL: relay.URL, Target: tg.URL})
	connected(t, a)
	stop()
	waitFor(t, "Off", func() bool { return a.State().Status == Off })
	waitFor(t, "the relay to forget it", func() bool { return h.s.Load().Agents() == 0 })
}

func TestPaths(t *testing.T) {
	if u, _ := AgentURL("https://relay.example.com/sub/", testID); u != "wss://relay.example.com/sub/agent?th="+TokenHash(testID) {
		t.Errorf("AgentURL %q", u)
	}
	if b := ClientBase("https://relay.example.com/", testID); b != "https://relay.example.com/c/"+testID {
		t.Errorf("ClientBase %q", b)
	}
	if id, rest, ok := clientPath("/c/" + testID + "/daemon"); !ok || id != testID || rest != "/daemon" {
		t.Errorf("clientPath %q %q %v", id, rest, ok)
	}
	if _, rest, ok := clientPath("/c/" + testID); !ok || rest != "/" {
		t.Errorf("clientPath without rest: %q %v", rest, ok)
	}
	var h headers
	if err := json.Unmarshal([]byte(`{"a":"1","set-cookie":["x","y"]}`), &h); err != nil || len(h["set-cookie"]) != 2 || h["a"][0] != "1" {
		t.Errorf("headers %v %v", h, err)
	}
}

// TestAlmaRelay runs the Agent against alma-relay's own Node server, on
// both of its protocols, when DROI_ALMA_RELAY names a checkout of it with
// its dependencies installed.
func TestAlmaRelay(t *testing.T) {
	dir := os.Getenv("DROI_ALMA_RELAY")
	if dir == "" {
		t.Skip("set DROI_ALMA_RELAY to an alma-relay checkout")
	}
	for _, proto := range []string{"2", "1"} {
		t.Run("protocol "+proto, func(t *testing.T) {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := l.Addr().(*net.TCPAddr).Port
			l.Close()
			cmd := exec.Command("node", filepath.Join(dir, "index.mjs"))
			cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(port), "HOST=127.0.0.1", "RELAY_PROTOCOL="+proto, "RELAY_REGISTRATION_KEY=k")
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
			url := "http://127.0.0.1:" + strconv.Itoa(port)
			waitFor(t, "alma-relay", func() bool {
				resp, err := http.Get(url + "/healthz")
				if err == nil {
					resp.Body.Close()
				}
				return err == nil
			})
			tg := newTarget(t)
			a, _ := startAgent(t, AgentOptions{URL: url, Target: tg.URL, Key: "k"})
			connected(t, a)
			base := ClientBase(url, testID)
			resp, err := http.Get(base + "/meta?x=1")
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(b), `"query":"x=1"`) {
				t.Fatalf("meta %q", b)
			}
			body := bytes.Repeat([]byte("y"), 6<<20)
			sum := sha256.Sum256(body)
			resp, err = http.Post(base+"/echo", "application/octet-stream", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			b, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(b) != fmt.Sprintf("%d %s", len(body), hex.EncodeToString(sum[:])) {
				t.Fatalf("echo %q", b)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/ws", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseNow()
			c.SetReadLimit(64 << 20)
			for _, m := range []string{"hello", strings.Repeat("z", 1<<20)} {
				_ = c.Write(ctx, websocket.MessageText, []byte(m))
				_, data, err := c.Read(ctx)
				if err != nil || string(data) != m {
					t.Fatalf("echo %d bytes, %v", len(data), err)
				}
			}
			wrong, _ := startAgent(t, AgentOptions{URL: url, Target: tg.URL, Key: "nope", ID: "a-different-id-12345678"})
			waitFor(t, "a refusal", func() bool { return wrong.State().Status == Refused })
		})
	}
}
