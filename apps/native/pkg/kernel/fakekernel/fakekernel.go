// Package fakekernel runs a real Kernel against a scripted model, for Go tests.
//
// There is no test-only mode in the Kernel: this starts the production sidecar and points its provider at
// an OpenAI-compatible endpoint the test itself serves. So a test drives the real protocol — real
// conversations, real transcripts, real tool execution, real view pushes — and the model is the only fake
// part. The script is Go, which means the compiler checks it and the test can read back what the Kernel
// sent.
//
// The binary comes from DROI_KERNEL_BIN, or else the build in apps/native/assets/kernel/<goos>-<goarch>
// that `pnpm build:kernel` writes. Without one the test is skipped, because `go test ./apps/native/...`
// has to work on a checkout with no Kernel built; set DROI_FAKEKERNEL_REQUIRED=1 to turn that skip into a
// failure, since a skipped test proves nothing. The `native-packages` CI job builds the host Kernel and sets
// that variable (see `.github/workflows/pr.yml`).
//
// A test's Kernel gets its own world: a temporary HOME, a temporary working directory, and only a
// whitelisted slice of the parent's environment. A test must never read or write the real ~, and must never
// inherit a key.
package fakekernel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// ProviderID and ModelID are what a scripted Kernel's model is called.
const (
	ProviderID = "fake"
	ModelID    = "fake-1"
)

const (
	// shutdownAskTimeout bounds the dial and the `kernel.shutdown` call: a Kernel that stopped answering
	// must cost a Close a few seconds, not the whole test run.
	shutdownAskTimeout = 3 * time.Second
	// shutdownWaitTimeout is how long a Kernel gets to finish its own cleanup after saying it will stop.
	shutdownWaitTimeout = 15 * time.Second
)

// RequiredEnv turns a missing Kernel binary from a skip into a failure. CI sets it: a package whose tests
// all skip is a package with no tests.
const RequiredEnv = "DROI_FAKEKERNEL_REQUIRED"

// Call is one tool the model asks the Kernel to run.
type Call struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	ID        string         `json:"id,omitempty"`
}

// Failure is a model request that fails the way a provider does.
type Failure struct {
	// Status is the HTTP status to answer with; 0 closes the connection instead, which is what a network
	// drop looks like to the Kernel.
	Status int    `json:"status,omitempty"`
	Body   string `json:"body,omitempty"`
}

// The header a model.complete request carries, and the value it carries. pi-ai streams every text call
// (model.complete is stream().result()), so a body alone cannot tell that call from a conversation turn: a
// build without the header would hand its title to a Step, or record the miss the Step existed to avoid.
// The header is what `Completions` is keyed on.
const (
	CallHeader        = "X-Droi-Call"
	CallModelComplete = "model.complete"
)

// Completion is one answer to a model.complete call: the model request a Host makes beside a conversation,
// such as the one that names it. It is recognised by the CallHeader header, or by an unstreamed body, so a
// build that sends neither is still answered here rather than taking a Step.
//
// It is not a Step on purpose. A Step carries a tool call, chunks and a delay because a conversation's
// reply is streamed into a view; a completion has none of those, and letting one take a Step would hand the
// reply a test is waiting for to a call it never meant.
type Completion struct {
	// Match is a substring of the request's whole prompt, system message included; "" matches any
	// completion. The system message is where an instruction such as "You name a chat" lives.
	Match string `json:"match,omitempty"`
	// Text is the answer. A caller that sent a schema reads it as JSON, so it must be a JSON document.
	Text string `json:"text,omitempty"`
	// Failure, when set, makes this request fail the way a provider does.
	Failure *Failure `json:"failure,omitempty"`
}

// Step is one model answer, matched to the request that asks for it.
type Step struct {
	// Match is a substring of the last user message; "" matches any request. A request takes the first step
	// that matches it, and never takes it again.
	//
	// Note the one rule that surprises people: a tool result is a `tool` message, not a user one, so the
	// step that follows a tool call matches the same prompt as the step that asked for it.
	Match string `json:"match,omitempty"`
	// Text is the reply, streamed as deltas.
	Text string `json:"text,omitempty"`
	// Chunks splits Text into these deltas instead of a fixed 40-character split, so a test can assert what
	// a viewer sees between deltas.
	Chunks []string `json:"chunks,omitempty"`
	// Call, when set, is a tool call instead of text.
	Call *Call `json:"call,omitempty"`
	// Failure, when set, makes this request fail (status, or a dropped connection).
	Failure *Failure `json:"failure,omitempty"`
	// Delay is waited before each delta: a slow stream, for a test of incremental rendering.
	Delay time.Duration `json:"delay,omitempty"`
}

// Scenario is everything the scripted model answers, in order.
type Scenario struct {
	Steps []Step `json:"steps,omitempty"`
	// AllowUnmatched accepts a model request that no step matched, answering it with Fallback. It is off by
	// default and the test **fails** instead: an unscripted request means the test is driving a path its
	// script does not describe, and its assertions would be checking the nothing that came back.
	AllowUnmatched bool `json:"allowUnmatched,omitempty"`
	// Fallback is the text for an unmatched request when AllowUnmatched is set, and for a model.complete that
	// no Completion matched; "" answers with a line that says the request was not scripted, so a test that
	// forgot AllowUnmatched sees why.
	Fallback string `json:"fallback,omitempty"`

	// Completions answer the model.complete calls, in order: the requests that carry CallHeader, or that are
	// unstreamed. They are a separate list from Steps because a completion is not the conversation's next
	// turn, and an unscripted completion is not a miss: once the Kernel implements model.complete every test
	// that creates a conversation makes one, and a miss would fail tests that never asked about titles.
	Completions []Completion `json:"completions,omitempty"`

	// Autonomy is the approval level Start pushes, as a Host does right after it connects: "ask", "edits",
	// "safe" or "all". "" is "all", so a scripted tool call runs without a card; a test of approvals asks for
	// a lower level.
	Autonomy string `json:"-"`

	// MemoryDir, when set, starts the Kernel with Memory on that folder, as a Host with Memory on does.
	MemoryDir string `json:"-"`
}

// FakeTitleAnswer is what an unscripted model.complete answers: a valid title for the schema the Kernel's
// title call sends. It is a constant rather than a message about a missing script because a completion is
// expected, not a mistake.
const FakeTitleAnswer = `{"title":"Fake title"}`

// Request is one model request, as the Kernel sent it.
type Request struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
	// Headers are the request's HTTP headers, with Go's lower-cased keys; they are not part of the body.
	// A test reads CallHeader here to assert that a completion was marked as one.
	Headers map[string]string `json:"headers,omitempty"`
}

// Header is one request header, by any spelling of its name.
func (r Request) Header(name string) string {
	for key, value := range r.Headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

// Prompt is the text of the last user message: what a Step's Match is tested against.
func (r Request) Prompt() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role != "user" {
			continue
		}
		return textOf(r.Messages[i].Content)
	}
	return ""
}

// FullPrompt is every message's text in order, the system message first: what a Completion's Match is
// tested against. Prompt is the last user message, which is what a Step's Match is tested against.
func (r Request) FullPrompt() string {
	parts := make([]string, 0, len(r.Messages))
	for _, message := range r.Messages {
		if text := textOf(message.Content); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// Kernel is a running Kernel and the scripted model it talks to.
type Kernel struct {
	// Port is the port the Kernel reported in its handshake.
	Port int
	// Token is the launch token; Dial passes it.
	Token string
	// DataDir is the throwaway data directory, kept until the test ends.
	DataDir string
	// HomeDir is the Kernel's HOME, and its tools'; never the user's.
	HomeDir string
	// Workspace is the default working directory for conversations and tools.
	Workspace string
	// UserDir is the Kernel's DROI_USER_DIR: this test's own directory for
	// personal skills, mcp.json and AGENTS.md, never the user's real one.
	UserDir string
	// Version is the Kernel's version, from its handshake.
	Version string

	model     *scriptedModel
	t         testing.TB
	cmd       *exec.Cmd
	tree      processTree
	logs      *lockedBuffer
	wait      chan struct{}
	closeOnce sync.Once
}

// Start runs a Kernel with the scenario's model and stops both when the test ends.
//
// It skips the test when no Kernel binary is available — a checkout without `pnpm build:kernel` — unless
// DROI_FAKEKERNEL_REQUIRED=1, which makes that a failure instead: CI must not pass because every test in
// the package skipped.
func Start(t testing.TB, sc Scenario) *Kernel {
	t.Helper()
	bin := binary(t)
	if bin == "" {
		if os.Getenv(RequiredEnv) != "" {
			t.Fatalf("fakekernel: no Kernel binary and %s is set: run pnpm build:kernel, or set DROI_KERNEL_BIN", RequiredEnv)
		}
		t.Skip("no Kernel binary: set DROI_KERNEL_BIN or run pnpm build:kernel")
	}

	model := startScriptedModel(t, sc)
	data := t.TempDir()
	home := t.TempDir()
	workspace := t.TempDir()
	token, err := kernel.NewToken()
	if err != nil {
		t.Fatalf("fakekernel: token: %v", err)
	}

	cmd := exec.Command(bin, "--data", data, "--port", "0", "--host", "127.0.0.1")
	// A whole temporary environment: the Kernel and every tool it runs see this HOME, these temp
	// directories and this PATH, and nothing else from the parent — no FACTORY_*, no *_API_KEY.
	cmd.Env = isolatedEnv(home)
	// userDir is this test's own: a Kernel started here must never read, or
	// write, the user's real ~/.droi — its skills, mcp.json and AGENTS.md.
	userDir := t.TempDir()
	cmd.Env = append(cmd.Env,
		kernel.TokenEnv+"="+token,
		kernel.UserDirEnv+"="+userDir,
		"DROI_KERNEL_PROVIDERS="+providersJSON(model.baseURL),
		"FAKE_PROVIDER_KEY=fake-key",
	)
	if sc.MemoryDir != "" {
		cmd.Env = append(cmd.Env, kernel.MemoryDirEnv+"="+sc.MemoryDir)
	}
	// Start it in the workspace, not in this package's source directory: a tool that runs `pwd` or writes a
	// relative path must land in a throwaway directory.
	cmd.Dir = workspace
	prepareProcess(cmd)
	logs := &lockedBuffer{}
	cmd.Stderr = logs
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("fakekernel: stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("fakekernel: start %s: %v", bin, err)
	}

	k := &Kernel{
		Token: token, DataDir: data, HomeDir: home, Workspace: workspace, UserDir: userDir,
		model: model, cmd: cmd, logs: logs, wait: make(chan struct{}), t: t,
	}
	tree, err := newProcessTree(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("fakekernel: process tree: %v", err)
	}
	k.tree = tree
	go func() {
		_ = cmd.Wait()
		close(k.wait)
	}()
	t.Cleanup(k.Close)

	// The handshake is the first line of stdout: the port is the Kernel's to choose, so no test has to probe
	// for a free one (and race whoever takes it next).
	handshake, err := readHandshake(stdout, 30*time.Second)
	if err != nil {
		t.Fatalf("fakekernel: %v\n%s", err, logs.String())
	}
	k.Port = handshake.Port
	k.Version = handshake.Version
	k.pushPolicy(t, sc.Autonomy)
	return k
}

// pushPolicy stands in for the Host's first policy.set: until one arrives the Kernel lets no tool above "ask" run.
// A Kernel without approvals answers CodeUnsupported, and runs every tool anyway.
func (k *Kernel) pushPolicy(t testing.TB, autonomy string) {
	t.Helper()
	if autonomy == "" {
		autonomy = "all"
	}
	client := k.Dial(t)
	defer client.Close()
	ctx, cancel := k.context(t, 30*time.Second)
	defer cancel()
	_, err := client.PolicySet(ctx, kernel.PolicySetParams{Policy: kernel.PolicyChange{Autonomy: autonomy}})
	if err != nil && !kernel.Is(err, kernel.CodeUnsupported) {
		t.Fatalf("fakekernel: policy.set: %v\n%s", err, k.Logs())
	}
}

// Close stops the Kernel, everything it started, and the scripted model. It runs when the test ends; calling
// it twice is harmless.
//
// The order is ask, wait, then escalate: `kernel.shutdown` is the path the Host takes, and the Kernel ends
// the tools it started on its way out. Signalling before that finishes would race the Kernel's own cleanup,
// and the tree below is the fallback for a Kernel that will not stop — not the normal route.
func (k *Kernel) Close() {
	k.closeOnce.Do(func() {
		// Ask first, the way the Host does: `kernel.shutdown` closes the harness, ends the tools the Kernel
		// started, and closes the storage, so a test's Close exercises the same path a stop does. Both the
		// dial and the call are bounded: a Kernel that accepts TCP and then stops answering — SIGSTOP is how
		// the review reproduced it — would otherwise block Close for ever, and go test's hard timeout would
		// kill the run with the cleanup half done.
		if client := k.dialForShutdown(); client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), shutdownAskTimeout)
			_ = client.Send(ctx, "kernel.shutdown", map[string]any{})
			cancel()
			_ = client.Close()
			// Then wait for it: the Kernel finishes its own cleanup and exits. A second signal now would race
			// that cleanup — main.ts answers SIGINT by closing and exiting, and the ordering between the two
			// is what decides whether a tool's group is ended at all.
			select {
			case <-k.wait:
				k.tree.kill()
				k.tree.close()
				k.model.stop()
				return
			case <-time.After(shutdownWaitTimeout):
			}
		}
		// It did not close in time, or never answered: stop it the hard way. One short grace for a SIGINT
		// that might still land, then SIGKILL — Close must not spend a test run waiting on a wedged Kernel.
		if k.cmd.Process != nil {
			_ = k.cmd.Process.Signal(os.Interrupt)
		}
		select {
		case <-k.wait:
		case <-time.After(time.Second):
		}
		k.tree.kill()
		k.tree.close()
		if k.cmd.Process != nil {
			_ = k.cmd.Process.Kill()
		}
		select {
		case <-k.wait:
		case <-time.After(10 * time.Second):
		}
		k.model.stop()
	})
}

// dialForShutdown connects for the shutdown request, or answers nil when the Kernel will not have it.
func (k *Kernel) dialForShutdown() *kernel.Client {
	ctx, cancel := k.context(k.t, shutdownAskTimeout)
	defer cancel()
	client, err := kernel.Dial(ctx, kernel.Options{
		URL:            kernel.URL(k.Port),
		Token:          k.Token,
		RequestTimeout: shutdownAskTimeout,
	})
	if err != nil {
		return nil
	}
	return client
}

// Logs is what the Kernel wrote to stderr so far; a failing assertion prints it.
func (k *Kernel) Logs() string { return k.logs.String() }

// Requests are the model requests the Kernel made, in order.
func (k *Kernel) Requests() []Request { return k.model.requests() }

// Unmatched is how many requests no step matched. A strict scenario reports them as a test failure on its
// own; this is for a test that wants to say something more specific.
func (k *Kernel) Unmatched() int { return k.model.unmatched() }

// Completions are the model.complete requests the Kernel made, in order: one per title, and any other call
// a Host makes beside a conversation. A test reads them to assert what the Host asked, such as the model
// that named a conversation or the language it was told to answer in.
func (k *Kernel) Completions() []Request { return k.model.completions() }

// Dial connects a client to the Kernel, waits for its health check, and closes the client when the test
// ends.
func (k *Kernel) Dial(t testing.TB) *kernel.Client {
	t.Helper()
	ctx, cancel := k.context(t, 60*time.Second)
	defer cancel()
	for {
		if _, err := kernel.CheckHealth(ctx, k.Port); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("fakekernel: no health answer in 60 s:\n%s", k.Logs())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("fakekernel: the test ended before the Kernel answered: %v\n%s", ctx.Err(), k.Logs())
		case <-time.After(20 * time.Millisecond):
		}
	}
	client, err := kernel.Dial(ctx, kernel.Options{
		URL:            kernel.URL(k.Port),
		Token:          k.Token,
		RequestTimeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("fakekernel: dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// context is what every RPC and every wait uses: bounded by the caller's `within` **and** by the test's own
// deadline, whichever is sooner. Without the second, a stalled RPC can outlive the test, and the panic the
// runner raises for a dead test is what skips a cleanup that was about to assert something.
func (k *Kernel) context(t testing.TB, within time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	limit := within
	// testing.TB has no Deadline; testing.T does, and it is what a test runner passes.
	if withDeadline, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		deadline, ok := withDeadline.Deadline()
		if !ok {
			return context.WithTimeout(context.Background(), limit)
		}
		// Leave a moment to report: a context that expires with the test fails the test with a signal
		// instead of a message from our own assertion. When that margin is already gone, the right answer is
		// an expired context — the wait gives up at once and the test's cleanups still run — not the hard
		// deadline, which spends the last second and then panics through the cleanups.
		remaining := time.Until(deadline) - time.Second
		if remaining <= 0 {
			expired, cancel := context.WithCancel(context.Background())
			cancel()
			return expired, func() {}
		}
		if remaining < limit {
			limit = remaining
		}
	}
	return context.WithTimeout(context.Background(), limit)
}

// ── the scripted model ────────────────────────────────────────────────────────────────────────────

type scriptedModel struct {
	scenario Scenario
	server   *http.Server
	listener net.Listener
	baseURL  string

	// closed stops a handler that is sleeping between deltas; handlers runs the count Close waits on.
	closed   chan struct{}
	closeOne sync.Once
	handlers sync.WaitGroup

	mu      sync.Mutex
	steps   []Step
	seen    []Request
	misses  []string
	answers []Completion
	taken   []Request
	t       testing.TB
}

func startScriptedModel(t testing.TB, sc Scenario) *scriptedModel {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakekernel: listen: %v", err)
	}
	m := &scriptedModel{
		scenario: sc,
		steps:    append([]Step(nil), sc.Steps...),
		answers:  append([]Completion(nil), sc.Completions...),
		listener: listener,
		closed:   make(chan struct{}),
		t:        t,
	}
	m.baseURL = "http://" + listener.Addr().String() + "/v1"
	m.server = &http.Server{Handler: http.HandlerFunc(m.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = m.server.Serve(listener) }()
	t.Cleanup(m.stop)
	if !sc.AllowUnmatched {
		// Strict by default: a request the script does not describe is a test that is not testing what it
		// says. Reported here rather than inside the handler so the message lands on the test that started
		// the Kernel, with its name.
		t.Cleanup(func() {
			for _, prompt := range m.unmatchedList() {
				t.Errorf("fakekernel: no Step matched the prompt %q; add a step, or set Scenario.AllowUnmatched", prompt)
			}
		})
	}
	return m
}

// stop cancels every handler and waits for it. A handler is a goroutine sleeping between deltas; returning
// before it has noticed would leave it writing to a closed server.
func (m *scriptedModel) stop() {
	m.closeOne.Do(func() { close(m.closed) })
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
	joined := make(chan struct{})
	go func() {
		m.handlers.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
	}
}

func (m *scriptedModel) requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.seen...)
}

func (m *scriptedModel) unmatched() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.misses)
}

func (m *scriptedModel) unmatchedList() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.misses...)
}

// take picks the first step whose Match the prompt satisfies, and consumes it.
func (m *scriptedModel) take(prompt string) (Step, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, step := range m.steps {
		if step.Match != "" && !strings.Contains(prompt, step.Match) {
			continue
		}
		m.steps = append(m.steps[:i], m.steps[i+1:]...)
		return step, true
	}
	m.misses = append(m.misses, prompt)
	return Step{Text: m.scenario.Fallback}, false
}

func (m *scriptedModel) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	var request Request
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(r.Header) > 0 {
		request.Headers = make(map[string]string, len(r.Header))
		for key := range r.Header {
			request.Headers[key] = r.Header.Get(key)
		}
	}
	m.mu.Lock()
	m.seen = append(m.seen, request)
	m.mu.Unlock()

	// A completion is marked by its header, which is how the Kernel sends one: pi-ai streams every text
	// call, so an unstreamed body is only kept as a second way to recognise it.
	if !request.Stream || r.Header.Get(CallHeader) == CallModelComplete {
		m.serveCompletion(r.Context(), w, request)
		return
	}

	step, matched := m.take(request.Prompt())
	if !matched && !m.scenario.AllowUnmatched && step.Text == "" {
		// Say it in the answer too: a test that asserts on the reply then sees the reason, instead of an
		// empty assistant message it might accept.
		step.Text = fmt.Sprintf("[fakekernel: the prompt %q matched no Step]", request.Prompt())
	}
	if step.Failure != nil {
		m.fail(w, step.Failure)
		return
	}
	m.handlers.Add(1)
	defer m.handlers.Done()
	m.stream(r.Context(), w, step)
}

// serveCompletion answers a model.complete request from Scenario.Completions, and never from Steps: a
// completion is the call beside a conversation, and taking a Step for it would give the reply a test is
// waiting for to a call it never meant. One that matches no Completion is answered anyway and is not a
// miss — once the Kernel implements model.complete, every conversation makes one.
//
// A streamed request is answered as a stream, because that is the shape the Kernel reads: the same SSE
// chunks a conversation's reply arrives in. The answer's text is the whole title, so a caller collecting the
// stream gets it in one piece.
func (m *scriptedModel) serveCompletion(ctx context.Context, w http.ResponseWriter, request Request) {
	matched := m.takeCompletion(request)
	if matched.Failure != nil {
		m.fail(w, matched.Failure)
		return
	}
	text := matched.Text
	if text == "" {
		text = m.scenario.Fallback
	}
	if text == "" {
		text = FakeTitleAnswer
	}
	m.handlers.Add(1)
	defer m.handlers.Done()
	if request.Stream {
		m.stream(ctx, w, Step{Text: text})
		return
	}
	m.whole(w, Step{Text: text})
}

// takeCompletion records the request and removes the first Completion whose Match its prompt satisfies, under
// one lock, so Completions() is the order they were answered in.
func (m *scriptedModel) takeCompletion(request Request) Completion {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.taken = append(m.taken, request)
	prompt := request.FullPrompt()
	for i, completion := range m.answers {
		if completion.Match != "" && !strings.Contains(prompt, completion.Match) {
			continue
		}
		m.answers = append(m.answers[:i], m.answers[i+1:]...)
		return completion
	}
	return Completion{}
}

// completions are the model.complete requests answered so far, in order.
func (m *scriptedModel) completions() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.taken...)
}

// fail answers a request the way a provider does, and closes the connection when Status is 0, which is what
// a network drop looks like to the Kernel.
func (m *scriptedModel) fail(w http.ResponseWriter, failure *Failure) {
	status := failure.Status
	if status == 0 {
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		// Nothing to drop: answer a failed request rather than writing a status of 0, which the writer
		// rejects.
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	text := failure.Body
	if text == "" {
		text = http.StatusText(status)
	}
	_, _ = io.WriteString(w, `{"error":{"message":`+quote(text)+`}}`)
}

// whole answers without SSE, which a Kernel that asks for stream=false reads.
func (m *scriptedModel) whole(w http.ResponseWriter, step Step) {
	message := map[string]any{"role": "assistant", "content": step.Text}
	finish := "stop"
	if step.Call != nil {
		args, _ := json.Marshal(step.Call.Arguments)
		message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
			"id": callID(step), "type": "function",
			"function": map[string]any{"name": step.Call.Name, "arguments": string(args)},
		}}}
		finish = "tool_calls"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-fake", "object": "chat.completion", "created": 1, "model": ModelID,
		"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": message}},
		"usage":   usage(),
	})
}

// stream writes the reply as the SSE chunks pi-ai parses, stopping the moment the request or the model is
// cancelled — a delay is a sleep between deltas, and a sleep nobody can interrupt is a goroutine that
// outlives its test.
func (m *scriptedModel) stream(ctx context.Context, w http.ResponseWriter, step Step) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	failed := false
	send := func(delta map[string]any, finish any, extra map[string]any) {
		if failed {
			return
		}
		chunk := map[string]any{
			"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": 1, "model": ModelID,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		for key, value := range extra {
			chunk[key] = value
		}
		encoded, _ := json.Marshal(chunk)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
			failed = true
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	// pause is the step's delay, or a stop: the Kernel closed the connection, the test ended, or the model
	// was closed. A cancelled stream returns at once instead of finishing its script.
	pause := func() bool {
		if step.Delay <= 0 {
			select {
			case <-ctx.Done():
				return false
			case <-m.closed:
				return false
			default:
				return true
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-m.closed:
			return false
		case <-time.After(step.Delay):
			return true
		}
	}

	if !pause() {
		return
	}
	send(map[string]any{"role": "assistant", "content": ""}, nil, nil)
	if step.Call != nil {
		args, _ := json.Marshal(step.Call.Arguments)
		if !pause() {
			return
		}
		send(map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": callID(step), "type": "function",
			"function": map[string]any{"name": step.Call.Name, "arguments": string(args)},
		}}}, nil, nil)
		if !pause() {
			return
		}
		send(map[string]any{}, "tool_calls", nil)
	} else {
		pieces := step.Chunks
		if len(pieces) == 0 {
			pieces = split(step.Text, 40)
		}
		for _, piece := range pieces {
			if !pause() {
				return
			}
			if piece == "" {
				continue
			}
			send(map[string]any{"content": piece}, nil, nil)
		}
		send(map[string]any{}, "stop", nil)
	}
	// Usage comes in a chunk with no choices, which is where OpenAI puts it.
	send(map[string]any{}, nil, map[string]any{"choices": []any{}, "usage": usage()})
	if !failed {
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func callID(step Step) string {
	if step.Call != nil && step.Call.ID != "" {
		return step.Call.ID
	}
	return "call_fake"
}

func usage() map[string]any {
	return map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
}

func providersJSON(baseURL string) string {
	encoded, _ := json.Marshal([]map[string]any{{
		"id":        ProviderID,
		"name":      "Fake provider",
		"baseUrl":   baseURL,
		"api":       "openai-completions",
		"apiKeyEnv": "FAKE_PROVIDER_KEY",
		"models": []any{map[string]any{
			"id": ModelID, "name": "Fake one", "contextWindow": 200_000, "maxTokens": 8_000,
			"input": []string{"text", "image"},
		}},
	}})
	return string(encoded)
}

// isolatedEnv is the environment a test's Kernel gets: a whitelist, plus a HOME and temp directories of its
// own. A whitelist rather than a blocklist, because the thing to avoid is not one variable but the class of
// them — a key in FACTORY_API_KEY, in *_API_KEY, in a config path that points at the user's real files.
//
// PATH, SHELL and the Windows system variables stay because a tool needs to start a shell; LANG and TERM
// because output should not depend on the locale of whoever ran the test.
func isolatedEnv(home string) []string {
	passthrough := []string{
		"PATH", "SHELL", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "TZ",
		"SystemRoot", "SystemDrive", "windir", "COMSPEC", "PATHEXT", "PROCESSOR_ARCHITECTURE",
		// Where Windows installs Git Bash, which the Kernel discovers to offer a bash tool
		// (apps/kernel/src/extensions/coding.ts). They name installation roots, not secrets, and a Kernel
		// that cannot see them offers no bash at all.
		"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432",
		// Windows tools keep their own state here; the Kernel's temp HOME is not a substitute for it, and
		// Git Bash under %LOCALAPPDATA%\Programs would be invisible if this were replaced.
		"LOCALAPPDATA",
	}
	env := make([]string, 0, len(passthrough)+10)
	for _, key := range passthrough {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			env = append(env, key+"="+value)
		}
	}
	temp := filepath.Join(home, "tmp")
	_ = os.MkdirAll(temp, 0o700)
	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"APPDATA="+filepath.Join(home, "AppData", "Roaming"),
		"TMPDIR="+temp,
		"TMP="+temp,
		"TEMP="+temp,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
	_ = os.MkdirAll(filepath.Join(home, ".config"), 0o700)
	_ = os.MkdirAll(filepath.Join(home, ".local", "share"), 0o700)
	_ = os.MkdirAll(filepath.Join(home, ".cache"), 0o700)
	return env
}

// ── the binary ───────────────────────────────────────────────────────────────────────────────────

type handshake struct {
	Port    int    `json:"port"`
	Pid     int    `json:"pid"`
	Version string `json:"version"`
}

// Available says whether Start has a Kernel to run, or would skip the test (fail it, under RequiredEnv). A harness
// that can also run without a Kernel asks this first instead of being skipped.
func Available() bool { return findKernel() != "" }

// Binary is the Kernel a test runs: DROI_KERNEL_BIN, or the build in apps/native/assets/kernel; "" when there is
// none. For a test that starts it some other way than Start, as a Host does.
func Binary() string { return findKernel() }

// binary is the Kernel to run: DROI_KERNEL_BIN, or the build the release packs.
func binary(t testing.TB) string {
	t.Helper()
	return findKernel()
}

func findKernel() string {
	if bin := os.Getenv("DROI_KERNEL_BIN"); bin != "" {
		return bin
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return findBinary(wd, runtime.GOOS, runtime.GOARCH)
}

// platformDir is the name `apps/kernel/build.ts` writes for a Go platform. The two disagree on one
// architecture and it is the common one: Go says `amd64`, the build says `x64`. Without this mapping every
// Intel Mac, and Windows and Linux x64 with it, found no binary and silently skipped.
func platformDir(goos, goarch string) (string, bool) {
	arch := goarch
	switch goarch {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		return "", false
	}
	switch goos {
	case "darwin", "windows", "linux":
		return goos + "-" + arch, true
	}
	return "", false
}

// findBinary walks up from start looking for apps/native's build of this platform, and stops at the
// filesystem root.
//
// The root test is `filepath.Dir(dir) == dir`, not a string comparison against "/": on Windows the walk
// arrives at `C:\`, whose parent is itself, and comparing to "/" there never matched — the loop spun
// forever instead of skipping.
func findBinary(start, goos, goarch string) string {
	platform, ok := platformDir(goos, goarch)
	if !ok {
		return ""
	}
	exe := "kernel"
	if goos == "windows" {
		exe += ".exe"
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, "assets", "kernel", platform, exe)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func readHandshake(stdout io.Reader, within time.Duration) (handshake, error) {
	type result struct {
		h   handshake
		err error
	}
	done := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "{") {
				var h handshake
				if err := json.Unmarshal([]byte(trimmed), &h); err != nil {
					done <- result{err: fmt.Errorf("handshake is not JSON: %w", err)}
					return
				}
				done <- result{h: h}
				return
			}
			if err != nil {
				done <- result{err: fmt.Errorf("the kernel printed no handshake: %w", err)}
				return
			}
		}
	}()
	select {
	case r := <-done:
		return r.h, r.err
	case <-time.After(within):
		return handshake{}, fmt.Errorf("no handshake in %s", within)
	}
}

func textOf(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// split cuts text into pieces of at most width runes.
func split(text string, width int) []string {
	var pieces []string
	runes := []rune(text)
	for len(runes) > width {
		pieces = append(pieces, string(runes[:width]))
		runes = runes[width:]
	}
	if len(runes) > 0 {
		pieces = append(pieces, string(runes))
	}
	return pieces
}

func quote(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

type lockedBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// errClosed is what a helper reports when the model went away before the Kernel asked it anything.
var errClosed = errors.New("fakekernel: the scripted model is closed")
