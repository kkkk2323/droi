// Package fakedaemon runs droi's Fake Daemon (droi/tests/fake-daemon), the
// Node stand-in for `droid daemon` that droi's own tests use, for this
// module's tests. It needs Node and a droi checkout with its dependencies
// installed; DROI_DIR points at the checkout (by default the one holding this
// module).
package fakedaemon

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

//go:embed serve.ts
var serveTS string

// Scenario says what the Fake Daemon answers.
type Scenario struct {
	Sessions         []SessionSpec `json:"sessions,omitempty"`
	Turn             *Turn         `json:"turn,omitempty"`
	ValidDirectories []string      `json:"validDirectories,omitempty"`
	// Input holds more of the Fake Daemon's ScenarioInput (commands, skills,
	// mcpServers, mcpRegistry, files, defaults, opened...), as JSON.
	Input map[string]any `json:"input,omitempty"`
}

// SessionSpec is a Session that exists from the start.
type SessionSpec struct {
	Title    string         `json:"title"`
	Cwd      string         `json:"cwd"`
	Messages []Message      `json:"messages,omitempty"`
	Extra    map[string]any `json:"extra,omitempty"`
}

// Message is a message in a Session's history: Role "user", "assistant" or
// "tool", with Text, or with Content blocks as the Daemon sends them
// (thinking, tool_use, tool_result...) in place of Text.
type Message struct {
	Role    string           `json:"role"`
	Text    string           `json:"text,omitempty"`
	Content []map[string]any `json:"content,omitempty"`
}

// Turn is how the agent answers daemon.add_user_message: Kind is "reply"
// (Deltas streamed), "permission" (asks to run Command, then Reply),
// "askUser" (Question with Options) or "todo".
type Turn struct {
	Kind        string   `json:"kind"`
	Deltas      []string `json:"deltas,omitempty"`
	DelayMs     int      `json:"delayMs,omitempty"`
	Command     string   `json:"command,omitempty"`
	Reply       string   `json:"reply,omitempty"`
	ToolOutput  string   `json:"toolOutput,omitempty"`
	Question    string   `json:"question,omitempty"`
	Options     []string `json:"options,omitempty"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
	Todos       []Todo   `json:"todos,omitempty"`
}

// Todo is an item of a todo turn's list.
type Todo struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// Daemon is a running Fake Daemon.
type Daemon struct {
	// URL is the WebSocket URL to connect to, token included.
	URL string
	// Sessions are the scenario's Sessions with the ids the Daemon gave them.
	Sessions []StartedSession

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex
	nextID int
	wait   map[int]chan json.RawMessage
}

// StartedSession is a scenario Session as the Daemon knows it.
type StartedSession struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
}

var (
	buildOnce sync.Once
	bundle    string
	buildErr  error
)

func droiDir() string {
	if d := os.Getenv("DROI_DIR"); d != "" {
		return d
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// build bundles serve.ts and the Fake Daemon into one script, once per test
// binary.
func build() (string, error) {
	buildOnce.Do(func() {
		droi := droiDir()
		esbuild := filepath.Join(droi, "node_modules", ".bin", "esbuild")
		if _, err := os.Stat(esbuild); err != nil {
			buildErr = fmt.Errorf("no droi checkout with dependencies at %s (set DROI_DIR): %w", droi, err)
			return
		}
		dir, err := os.MkdirTemp("", "droid-fake-daemon")
		if err != nil {
			buildErr = err
			return
		}
		entry := filepath.Join(dir, "serve.ts")
		src := strings.ReplaceAll(serveTS, "'DROI/", "'"+droi+"/")
		if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
			buildErr = err
			return
		}
		bundle = filepath.Join(dir, "serve.mjs")
		// The Fake Daemon finds droi's package.json from its own location.
		self := "file://" + filepath.Join(droi, "tests", "fake-daemon", "fake-daemon.ts")
		cmd := exec.Command(esbuild, entry, "--bundle", "--platform=node", "--format=esm",
			"--outfile="+bundle, "--log-level=error",
			"--define:import.meta.url="+strconvQuote(self),
			"--banner:js=import { createRequire } from 'module'; const require = createRequire("+strconvQuote(self)+");")
		cmd.Dir = filepath.Join(droi, "tests")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("esbuild: %v: %s", err, out)
		}
	})
	return bundle, buildErr
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

// Start runs a Fake Daemon for the test, skipping the test when there is no
// droi checkout, and stops it when the test ends.
func Start(t testing.TB, sc Scenario) *Daemon {
	t.Helper()
	script, err := build()
	if err != nil {
		t.Skip(err)
	}
	spec, _ := json.Marshal(sc)
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "DROID_FAKE_SCENARIO="+string(spec))
	cmd.Stderr = os.Stderr
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{cmd: cmd, stdin: stdin, wait: map[int]chan json.RawMessage{}}
	sc2 := bufio.NewScanner(stdout)
	sc2.Buffer(make([]byte, 1<<20), 64<<20)
	ready := make(chan error, 1)
	go func() {
		first := true
		for sc2.Scan() {
			line := append([]byte(nil), sc2.Bytes()...)
			if first {
				first = false
				ready <- json.Unmarshal(line, &struct {
					URL      *string           `json:"url"`
					Sessions *[]StartedSession `json:"sessions"`
				}{&d.URL, &d.Sessions})
				continue
			}
			var head struct {
				ID int `json:"id"`
			}
			if json.Unmarshal(line, &head) != nil {
				continue
			}
			d.mu.Lock()
			ch := d.wait[head.ID]
			delete(d.wait, head.ID)
			d.mu.Unlock()
			if ch != nil {
				ch <- line
			}
		}
		if first {
			ready <- fmt.Errorf("fake daemon exited before it started")
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("fake daemon did not start")
	}
	t.Cleanup(func() {
		stdin.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
	})
	return d
}

// Command sends a control command and returns its answer.
func (d *Daemon) Command(op string, args map[string]any) (json.RawMessage, error) {
	d.mu.Lock()
	d.nextID++
	id := d.nextID
	ch := make(chan json.RawMessage, 1)
	d.wait[id] = ch
	d.mu.Unlock()
	m := map[string]any{"id": id, "op": op}
	for k, v := range args {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	if _, err := d.stdin.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	select {
	case line := <-ch:
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(line, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("fake daemon: %s", e.Error)
		}
		return line, nil
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("fake daemon: %s: no answer", op)
	}
}

// GoDown drops every connection and refuses new ones, as a dead Daemon.
func (d *Daemon) GoDown() error { _, err := d.Command("goDown", nil); return err }

// ComeBack accepts connections again.
func (d *Daemon) ComeBack() error { _, err := d.Command("comeBack", nil); return err }

// Notify sends a session notification to the Clients attached to a Session.
func (d *Daemon) Notify(sessionID string, notification map[string]any) error {
	_, err := d.Command("notify", map[string]any{"sessionId": sessionID, "notification": notification})
	return err
}

// Request is a request the Daemon received.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Requests returns every request received so far.
func (d *Daemon) Requests() ([]Request, error) {
	line, err := d.Command("requests", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Requests []Request `json:"requests"`
	}
	return out.Requests, json.Unmarshal(line, &out)
}

// WaitForRequest waits for the nth request of a method.
func (d *Daemon) WaitForRequest(method string, nth int) (Request, error) {
	line, err := d.Command("waitForRequest", map[string]any{"method": method, "nth": nth, "timeoutMs": 10000})
	if err != nil {
		return Request{}, err
	}
	var out struct {
		Request Request `json:"request"`
	}
	return out.Request, json.Unmarshal(line, &out)
}
