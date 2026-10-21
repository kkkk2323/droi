// Package omp talks to oh-my-pi over its stdio JSONL RPC (omp --mode rpc-ui).
// Droi's Host keeps one child process per live OMP Session and fans its
// events out to the window.
package omp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Default binary name on PATH.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "omp.exe"
	}
	return "omp"
}

// Locate finds the omp executable: override, then PATH, then ~/.local/bin.
func Locate(override, pathEnv, home string) string {
	exists := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	if override != "" && exists(override) {
		return override
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, binaryName()); exists(p) {
			return p
		}
	}
	if home != "" {
		if p := filepath.Join(home, ".local", "bin", binaryName()); exists(p) {
			return p
		}
	}
	return ""
}

// ApprovalMode is OMP's tools.approvalMode override.
type ApprovalMode string

const (
	ApprovalAlwaysAsk       ApprovalMode = "always-ask"
	ApprovalAlwaysAskCached ApprovalMode = "always-ask-cached"
	ApprovalWrite           ApprovalMode = "write"
	ApprovalYolo            ApprovalMode = "yolo"
)

// Options start an OMP RPC child for one Session.
type Options struct {
	// Path is the omp executable; Locate when empty.
	Path string
	// Cwd is the Session's working directory.
	Cwd string
	// Mode is "rpc" or "rpc-ui"; rpc-ui is the default for interactive hosts.
	Mode string
	// ApprovalMode is passed as --approval-mode when set.
	ApprovalMode ApprovalMode
	// Model is a fuzzy model id for --model when set.
	Model string
	// Resume is a session id, path, or prefix for --resume.
	Resume string
	// SessionDir overrides PI_CODING_AGENT_DIR / --session-dir.
	SessionDir string
	// NoSession runs with --no-session (tests / throwaways).
	NoSession bool
	// Env is extra environment for the child; nil uses os.Environ.
	Env []string
	// ExtraArgs are appended after the mode flags.
	ExtraArgs []string
	// Spawn starts the child; nil uses exec.CommandContext.
	Spawn func(ctx context.Context, path string, args []string, env []string) (Process, error)
}

// Process is the stdio handle of one omp child.
type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() error
	Kill() error
}

type execProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (p *execProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *execProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *execProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *execProcess) Wait() error           { return p.cmd.Wait() }
func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func defaultSpawn(ctx context.Context, path string, args []string, env []string) (Process, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

// Ready is the first frame omp writes after start.
type Ready struct {
	ProtocolVersion           int   `json:"protocolVersion"`
	SupportedProtocolVersions []int `json:"supportedProtocolVersions"`
	MaxFrameBytes             int   `json:"maxFrameBytes"`
	MaxReassembledFrameBytes  int   `json:"maxReassembledFrameBytes"`
}

// Image is an inline image attached to a prompt.
type Image struct {
	Type     string `json:"type"` // "image"
	Data     string `json:"data"` // base64
	MimeType string `json:"mimeType,omitempty"`
}

// UIRequest is an extension_ui_request the host must answer or ignore.
type UIRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Raw    json.RawMessage `json:"-"`
}

// Message is a transcript message from OMP events.
type Message struct {
	ID        string
	Role      string
	Text      string
	Thinking  string
	Timestamp int64
	Streaming bool
	ToolName  string
	ToolID    string
	ToolArgs  string
	ToolOut   string
	IsError   bool
}

// Event is a normalized update the Host and UI can subscribe to.
type Event struct {
	Type string // ready, message, ui_request, prompt_result, settled, error, raw
	// Message is set for message events.
	Message *Message
	// UI is set for ui_request.
	UI *UIRequest
	// PromptID / Status for prompt_result.
	PromptID string
	Status   string
	// Err is a transport or protocol failure.
	Err error
	// Raw is the original frame when Type is raw or for debugging.
	Raw json.RawMessage
}

// Session is one live omp --mode rpc-ui child.
type Session struct {
	opts Options
	proc Process

	mu        sync.Mutex
	closed    bool
	ready     Ready
	readyCh   chan struct{}
	nextID    atomic.Uint64
	pending   map[string]chan response
	msgs      map[string]*Message // by messageId
	order     []string
	ui        map[string]UIRequest
	uiOrder   []string
	title     string
	model     string
	working   bool
	err       string
	listeners map[int]func(Event)
	nextLis   int

	cancel context.CancelFunc
	done   chan struct{}
}

type response struct {
	success bool
	data    json.RawMessage
	err     string
	raw     json.RawMessage
}

// Start launches omp and waits for the ready frame, then negotiates protocol v2.
func Start(parent context.Context, opts Options) (*Session, error) {
	if opts.Mode == "" {
		opts.Mode = "rpc-ui"
	}
	path := opts.Path
	if path == "" {
		path = Locate("", os.Getenv("PATH"), homeDir())
	}
	if path == "" {
		return nil, errors.New("omp was not found")
	}
	args := []string{"--mode", opts.Mode, "--no-ui"}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.ApprovalMode != "" {
		args = append(args, "--approval-mode", string(opts.ApprovalMode))
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Resume != "" {
		args = append(args, "--resume", opts.Resume)
	}
	if opts.SessionDir != "" {
		args = append(args, "--session-dir", opts.SessionDir)
	}
	if opts.NoSession {
		args = append(args, "--no-session")
	}
	args = append(args, opts.ExtraArgs...)

	spawn := opts.Spawn
	if spawn == nil {
		spawn = defaultSpawn
	}
	ctx, cancel := context.WithCancel(parent)
	proc, err := spawn(ctx, path, args, opts.Env)
	if err != nil {
		cancel()
		return nil, err
	}
	s := &Session{
		opts:      opts,
		proc:      proc,
		readyCh:   make(chan struct{}),
		pending:   map[string]chan response{},
		msgs:      map[string]*Message{},
		ui:        map[string]UIRequest{},
		listeners: map[int]func(Event){},
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go s.readStdout()
	go s.drain(proc.Stderr())
	select {
	case <-s.readyCh:
	case <-ctx.Done():
		_ = s.Close()
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		_ = s.Close()
		return nil, errors.New("omp did not become ready")
	}
	// Prefer protocol v2 for rpc_chunk reassembly of large frames.
	negCtx, negCancel := context.WithTimeout(ctx, 5*time.Second)
	_, _, negErr := s.call(negCtx, map[string]any{"type": "negotiate_protocol", "protocolVersion": 2})
	negCancel()
	if negErr != nil {
		// v1 still works; hosts just will not see chunked frames.
	}
	return s, nil
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// Close ends the child.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	_ = s.proc.Kill()
	<-s.done
	return nil
}

// Ready is the handshake frame.
func (s *Session) Ready() Ready {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// OnEvent hears every normalized event until unsubscribe.
func (s *Session) OnEvent(fn func(Event)) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextLis
	s.nextLis++
	s.listeners[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

func (s *Session) emit(ev Event) {
	s.mu.Lock()
	fns := make([]func(Event), 0, len(s.listeners))
	for _, fn := range s.listeners {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// Messages is a snapshot of the live transcript, oldest first.
func (s *Session) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, 0, len(s.order))
	for _, id := range s.order {
		if m := s.msgs[id]; m != nil {
			out = append(out, *m)
		}
	}
	return out
}

// PendingUI are unanswered extension_ui_request frames.
func (s *Session) PendingUI() []UIRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]UIRequest, 0, len(s.uiOrder))
	for _, id := range s.uiOrder {
		if u, ok := s.ui[id]; ok {
			out = append(out, u)
		}
	}
	return out
}

// Working reports an in-flight turn.
func (s *Session) Working() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.working
}

// Title is the last known session title.
func (s *Session) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// Model is the last known model id.
func (s *Session) Model() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

// LastError is the last transport or prompt error string.
func (s *Session) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Prompt submits a user message. It returns once omp acknowledges the
// command; completion arrives as a prompt_result event.
func (s *Session) Prompt(ctx context.Context, message string, images []Image) (string, error) {
	req := map[string]any{"type": "prompt", "message": message}
	if len(images) > 0 {
		req["images"] = images
	}
	s.mu.Lock()
	s.working = true
	s.mu.Unlock()
	id, raw, err := s.call(ctx, req)
	if err != nil {
		s.mu.Lock()
		s.working = false
		s.mu.Unlock()
		return id, err
	}
	_ = raw
	return id, nil
}

// Abort stops the current run.
func (s *Session) Abort(ctx context.Context) error {
	_, _, err := s.call(ctx, map[string]any{"type": "abort"})
	return err
}

// Steer queues a steering message into the active run.
func (s *Session) Steer(ctx context.Context, message string, images []Image) error {
	req := map[string]any{"type": "steer", "message": message}
	if len(images) > 0 {
		req["images"] = images
	}
	_, _, err := s.call(ctx, req)
	return err
}

// FollowUp queues a follow-up for after the current run.
func (s *Session) FollowUp(ctx context.Context, message string, images []Image) error {
	req := map[string]any{"type": "follow_up", "message": message}
	if len(images) > 0 {
		req["images"] = images
	}
	_, _, err := s.call(ctx, req)
	return err
}

// GetState returns the raw get_state data object.
func (s *Session) GetState(ctx context.Context) (json.RawMessage, error) {
	_, data, err := s.call(ctx, map[string]any{"type": "get_state"})
	return data, err
}

// GetMessages returns every message via get_messages.
func (s *Session) GetMessages(ctx context.Context) (json.RawMessage, error) {
	_, data, err := s.call(ctx, map[string]any{"type": "get_messages"})
	return data, err
}

// ModelInfo is one entry from get_available_models / get_state.
type ModelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// Reasoning reports whether the model supports thinking levels.
	Reasoning bool     `json:"reasoning"`
	Input     []string `json:"input,omitempty"`
}

// GetAvailableModels lists models OMP can run.
func (s *Session) GetAvailableModels(ctx context.Context) ([]ModelInfo, error) {
	_, data, err := s.call(ctx, map[string]any{"type": "get_available_models"})
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}
	return wrap.Models, nil
}

// SetModel selects a model by provider and id when both are known.
func (s *Session) SetModel(ctx context.Context, provider, modelID string) error {
	_, _, err := s.call(ctx, map[string]any{
		"type":     "set_model",
		"provider": provider,
		"modelId":  modelID,
	})
	if err == nil {
		s.mu.Lock()
		s.model = modelID
		s.mu.Unlock()
	}
	return err
}

// SetModelByID finds provider from the available list when only id is known.
func (s *Session) SetModelByID(ctx context.Context, modelID string) error {
	if modelID == "" {
		return errors.New("model id required")
	}
	models, err := s.GetAvailableModels(ctx)
	if err != nil {
		return err
	}
	for _, m := range models {
		if m.ID == modelID {
			provider := m.Provider
			if provider == "" {
				provider = "unknown"
			}
			return s.SetModel(ctx, provider, m.ID)
		}
	}
	// Fall back: try common bare form provider/id.
	if i := strings.IndexByte(modelID, '/'); i > 0 {
		return s.SetModel(ctx, modelID[:i], modelID[i+1:])
	}
	return fmt.Errorf("model not found: %s", modelID)
}

// AnswerUI replies to an extension_ui_request. payload is one of the
// documented response variants (value, confirmed, answers, cancel, …).
func (s *Session) AnswerUI(ctx context.Context, id string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["type"] = "extension_ui_response"
	payload["id"] = id
	if err := s.write(payload); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.ui, id)
	s.uiOrder = removeID(s.uiOrder, id)
	s.mu.Unlock()
	return nil
}

// CancelUI dismisses a UI request without answering it.
func (s *Session) CancelUI(ctx context.Context, targetID string) error {
	return s.AnswerUI(ctx, targetID, map[string]any{"cancel": true})
}

func removeID(ids []string, id string) []string {
	out := ids[:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func (s *Session) call(ctx context.Context, body map[string]any) (id string, data json.RawMessage, err error) {
	id = strconv.FormatUint(s.nextID.Add(1), 10)
	body["id"] = id
	ch := make(chan response, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return id, nil, errors.New("omp session closed")
	}
	s.pending[id] = ch
	s.mu.Unlock()
	if err := s.write(body); err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return id, nil, err
	}
	select {
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return id, nil, ctx.Err()
	case resp := <-ch:
		if !resp.success {
			msg := resp.err
			if msg == "" {
				msg = "omp command failed"
			}
			return id, resp.data, errors.New(msg)
		}
		return id, resp.data, nil
	case <-s.done:
		return id, nil, errors.New("omp session ended")
	}
}

func (s *Session) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("omp session closed")
	}
	_, err = s.proc.Stdin().Write(b)
	return err
}

func (s *Session) drain(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		// stderr is diagnostic only
	}
}

func (s *Session) readStdout() {
	defer close(s.done)
	defer func() {
		s.mu.Lock()
		for id, ch := range s.pending {
			ch <- response{success: false, err: "omp session ended"}
			delete(s.pending, id)
		}
		s.mu.Unlock()
	}()

	type chunkMeta struct {
		total int
		parts []string
	}
	chunks := map[string]*chunkMeta{}

	sc := bufio.NewScanner(s.proc.Stdout())
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(line, &probe); err != nil {
			s.emit(Event{Type: "error", Err: fmt.Errorf("invalid omp frame: %w", err), Raw: append(json.RawMessage(nil), line...)})
			continue
		}
		if typ, _ := rawString(probe["type"]); typ == "rpc_chunk" {
			id, _ := rawString(probe["chunkId"])
			idx, _ := rawInt(probe["index"])
			count, ok := rawInt(probe["count"])
			if !ok {
				count, _ = rawInt(probe["total"])
			}
			dataB64, _ := rawString(probe["data"])
			m := chunks[id]
			if m == nil {
				m = &chunkMeta{total: count, parts: make([]string, count)}
				chunks[id] = m
			}
			if idx >= 0 && idx < len(m.parts) {
				m.parts[idx] = dataB64
			}
			complete := m.total > 0
			for _, p := range m.parts {
				if p == "" {
					complete = false
					break
				}
			}
			if !complete {
				continue
			}
			delete(chunks, id)
			var joined strings.Builder
			for _, p := range m.parts {
				joined.WriteString(p)
			}
			decoded, err := decodeBase64(joined.String())
			if err != nil {
				// Some builds may send raw UTF-8 segments; accept either.
				decoded = []byte(joined.String())
			}
			line = decoded
			probe = nil
			if err := json.Unmarshal(line, &probe); err != nil {
				s.emit(Event{Type: "error", Err: fmt.Errorf("invalid reassembled omp frame: %w", err)})
				continue
			}
		}
		s.handleFrame(append(json.RawMessage(nil), line...), probe)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		s.mu.Lock()
		s.err = err.Error()
		s.mu.Unlock()
		s.emit(Event{Type: "error", Err: err})
	}
	_ = s.proc.Wait()
}

func rawString(r json.RawMessage) (string, bool) {
	if len(r) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s, true
	}
	return "", false
}

func rawInt(r json.RawMessage) (int, bool) {
	if len(r) == 0 {
		return 0, false
	}
	var n int
	if json.Unmarshal(r, &n) == nil {
		return n, true
	}
	return 0, false
}

func rawBool(r json.RawMessage) (bool, bool) {
	if len(r) == 0 {
		return false, false
	}
	var b bool
	if json.Unmarshal(r, &b) == nil {
		return b, true
	}
	return false, false
}

func (s *Session) handleFrame(raw json.RawMessage, probe map[string]json.RawMessage) {
	typ, _ := rawString(probe["type"])
	switch typ {
	case "ready":
		var r Ready
		_ = json.Unmarshal(raw, &r)
		s.mu.Lock()
		s.ready = r
		select {
		case <-s.readyCh:
		default:
			close(s.readyCh)
		}
		s.mu.Unlock()
		s.emit(Event{Type: "ready", Raw: raw})
	case "response":
		id, _ := rawString(probe["id"])
		success, _ := rawBool(probe["success"])
		errMsg, _ := rawString(probe["error"])
		s.mu.Lock()
		ch := s.pending[id]
		delete(s.pending, id)
		s.mu.Unlock()
		if ch != nil {
			ch <- response{success: success, data: probe["data"], err: errMsg, raw: raw}
		}
	case "prompt_result":
		id, _ := rawString(probe["id"])
		status, _ := rawString(probe["status"])
		settled, _ := rawBool(probe["sessionSettled"])
		s.mu.Lock()
		if settled || status == "completed" || status == "aborted" || status == "error" {
			// May still get session_settled; mark idle only when settled or terminal without pending bg.
			if settled {
				s.working = false
			}
		}
		if status == "error" {
			if e, ok := rawString(probe["error"]); ok {
				s.err = e
			}
		}
		s.mu.Unlock()
		s.emit(Event{Type: "prompt_result", PromptID: id, Status: status, Raw: raw})
	case "session_settled":
		s.mu.Lock()
		s.working = false
		s.mu.Unlock()
		s.emit(Event{Type: "settled", Raw: raw})
	case "agent_start", "turn_start":
		s.mu.Lock()
		s.working = true
		s.mu.Unlock()
		s.emit(Event{Type: "raw", Raw: raw})
	case "agent_end", "turn_end":
		s.emit(Event{Type: "raw", Raw: raw})
	case "message_start", "message_update", "message_end":
		s.applyMessage(typ, raw, probe)
	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		s.applyTool(typ, raw, probe)
	case "extension_ui_request":
		id, _ := rawString(probe["id"])
		method, _ := rawString(probe["method"])
		// Ignore pure chrome widgets the Host cannot draw yet.
		if method == "setWidget" || method == "setStatus" || method == "setTitle" || method == "setEditorText" || method == "notify" || method == "openUrl" {
			s.emit(Event{Type: "raw", Raw: raw})
			return
		}
		req := UIRequest{ID: id, Method: method, Raw: raw}
		s.mu.Lock()
		if _, ok := s.ui[id]; !ok {
			s.uiOrder = append(s.uiOrder, id)
		}
		s.ui[id] = req
		s.mu.Unlock()
		s.emit(Event{Type: "ui_request", UI: &req, Raw: raw})
	case "session_info_update":
		if t, ok := rawString(probe["title"]); ok {
			s.mu.Lock()
			s.title = t
			s.mu.Unlock()
		}
		s.emit(Event{Type: "raw", Raw: raw})
	default:
		s.emit(Event{Type: "raw", Raw: raw})
	}
}

func (s *Session) applyMessage(typ string, raw json.RawMessage, probe map[string]json.RawMessage) {
	id, _ := rawString(probe["messageId"])
	if id == "" {
		id = "msg"
	}
	text, thinking, role, ts := parseMessageFields(probe)
	if typ == "message_update" {
		// Prefer assistantMessageEvent deltas when present.
		if evRaw, ok := probe["assistantMessageEvent"]; ok {
			var ev map[string]json.RawMessage
			if json.Unmarshal(evRaw, &ev) == nil {
				et, _ := rawString(ev["type"])
				switch et {
				case "text_delta", "text_end":
					if d, ok := rawString(ev["delta"]); ok && et == "text_delta" {
						text = d
					}
					if c, ok := rawString(ev["content"]); ok && et == "text_end" {
						text = c
					}
					// partial carries full text sometimes
					if p, ok := ev["partial"]; ok {
						if _, t, _, _ := parseContentRole(p); t != "" && et == "text_end" {
							text = t
						}
					}
				case "thinking_delta", "thinking_end":
					if d, ok := rawString(ev["delta"]); ok && et == "thinking_delta" {
						thinking = d
					}
					if c, ok := rawString(ev["content"]); ok && et == "thinking_end" {
						thinking = c
					}
				}
			}
		}
	}
	s.mu.Lock()
	m := s.msgs[id]
	if m == nil {
		m = &Message{ID: id}
		s.msgs[id] = m
		s.order = append(s.order, id)
	}
	if role != "" {
		m.Role = role
	}
	if ts != 0 {
		m.Timestamp = ts
	}
	switch typ {
	case "message_start":
		if text != "" {
			m.Text = text
		}
		if thinking != "" {
			m.Thinking = thinking
		}
		m.Streaming = true
	case "message_update":
		// For deltas, append text_delta; for end events, replace.
		if evRaw, ok := probe["assistantMessageEvent"]; ok {
			var ev map[string]json.RawMessage
			if json.Unmarshal(evRaw, &ev) == nil {
				et, _ := rawString(ev["type"])
				switch et {
				case "text_delta":
					if d, ok := rawString(ev["delta"]); ok {
						m.Text += d
					}
				case "text_end":
					if c, ok := rawString(ev["content"]); ok {
						m.Text = c
					} else if p, ok := ev["partial"]; ok {
						if _, t, _, _ := parseContentRole(p); t != "" {
							m.Text = t
						}
					}
				case "thinking_delta":
					if d, ok := rawString(ev["delta"]); ok {
						m.Thinking += d
					}
				case "thinking_end":
					if c, ok := rawString(ev["content"]); ok {
						m.Thinking = c
					}
				}
			}
		} else if text != "" {
			m.Text = text
		}
		m.Streaming = true
	case "message_end":
		if text != "" {
			m.Text = text
		}
		if thinking != "" {
			m.Thinking = thinking
		}
		// Prefer full message object.
		if msgRaw, ok := probe["message"]; ok {
			if r, t, th, tsv := parseContentRole(msgRaw); true {
				if r != "" {
					m.Role = r
				}
				if t != "" {
					m.Text = t
				}
				if th != "" {
					m.Thinking = th
				}
				if tsv != 0 {
					m.Timestamp = tsv
				}
			}
		}
		m.Streaming = false
	}
	cp := *m
	s.mu.Unlock()
	s.emit(Event{Type: "message", Message: &cp, Raw: raw})
}

func (s *Session) applyTool(typ string, raw json.RawMessage, probe map[string]json.RawMessage) {
	id, _ := rawString(probe["toolCallId"])
	if id == "" {
		id, _ = rawString(probe["id"])
	}
	if id == "" {
		id = "tool"
	}
	name, _ := rawString(probe["toolName"])
	s.mu.Lock()
	m := s.msgs[id]
	if m == nil {
		m = &Message{ID: id, Role: "tool"}
		s.msgs[id] = m
		s.order = append(s.order, id)
	}
	m.Role = "tool"
	if name != "" {
		m.ToolName = name
	}
	m.ToolID = id
	switch typ {
	case "tool_execution_start":
		m.Streaming = true
		if args := probe["args"]; len(args) > 0 {
			m.ToolArgs = string(args)
		}
	case "tool_execution_update":
		m.Streaming = true
	case "tool_execution_end":
		m.Streaming = false
		if out := probe["result"]; len(out) > 0 {
			m.ToolOut = string(out)
		} else if out := probe["output"]; len(out) > 0 {
			m.ToolOut = string(out)
		}
		if e, ok := rawBool(probe["isError"]); ok {
			m.IsError = e
		}
	}
	cp := *m
	s.mu.Unlock()
	s.emit(Event{Type: "message", Message: &cp, Raw: raw})
}

func parseMessageFields(probe map[string]json.RawMessage) (text, thinking, role string, ts int64) {
	if msg, ok := probe["message"]; ok {
		return parseContentRole(msg)
	}
	return "", "", "", 0
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func parseContentRole(msg json.RawMessage) (role, text, thinking string, ts int64) {
	var m struct {
		Role      string `json:"role"`
		Timestamp int64  `json:"timestamp"`
		Content   []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
	}
	if json.Unmarshal(msg, &m) != nil {
		return "", "", "", 0
	}
	role = m.Role
	ts = m.Timestamp
	for _, c := range m.Content {
		switch c.Type {
		case "text":
			text += c.Text
		case "thinking":
			thinking += c.Thinking
		}
	}
	return role, text, thinking, ts
}
