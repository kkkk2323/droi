package omp

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProc struct {
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderrR *io.PipeReader
	stderrW *io.PipeWriter
	wait    chan struct{}
	killed  bool
	mu      sync.Mutex
}

func newFakeProc() *fakeProc {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	return &fakeProc{
		stdinR: inR, stdinW: inW,
		stdoutR: outR, stdoutW: outW,
		stderrR: errR, stderrW: errW,
		wait: make(chan struct{}),
	}
}

func (p *fakeProc) Stdin() io.WriteCloser { return p.stdinW }
func (p *fakeProc) Stdout() io.ReadCloser { return p.stdoutR }
func (p *fakeProc) Stderr() io.ReadCloser { return p.stderrR }
func (p *fakeProc) Wait() error {
	<-p.wait
	return nil
}
func (p *fakeProc) Kill() error {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	_ = p.stdinW.Close()
	_ = p.stdoutW.Close()
	_ = p.stderrW.Close()
	select {
	case <-p.wait:
	default:
		close(p.wait)
	}
	return nil
}

func (p *fakeProc) write(v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	_, _ = p.stdoutW.Write(b)
}

func TestSessionPromptAndStream(t *testing.T) {
	fp := newFakeProc()
	var gotArgs []string
	s, err := Start(context.Background(), Options{
		Path: "/bin/true",
		Cwd:  t.TempDir(),
		Spawn: func(ctx context.Context, path string, args []string, env []string) (Process, error) {
			gotArgs = append([]string{}, args...)
			// Serve ready then handle commands.
			go func() {
				fp.write(map[string]any{
					"type":                      "ready",
					"protocolVersion":           1,
					"supportedProtocolVersions": []int{1, 2},
					"maxFrameBytes":             1024,
					"maxReassembledFrameBytes":  4096,
				})
				buf := make([]byte, 0, 4096)
				tmp := make([]byte, 256)
				for {
					n, err := fp.stdinR.Read(tmp)
					if n > 0 {
						buf = append(buf, tmp[:n]...)
						for {
							i := strings.IndexByte(string(buf), '\n')
							if i < 0 {
								break
							}
							line := string(buf[:i])
							buf = buf[i+1:]
							var req map[string]any
							_ = json.Unmarshal([]byte(line), &req)
							id, _ := req["id"].(string)
							typ, _ := req["type"].(string)
							switch typ {
							case "negotiate_protocol":
								fp.write(map[string]any{"id": id, "type": "response", "command": "negotiate_protocol", "success": true, "data": map[string]any{"protocolVersion": 2}})
							case "prompt":
								fp.write(map[string]any{"id": id, "type": "response", "command": "prompt", "success": true})
								fp.write(map[string]any{"type": "agent_start"})
								fp.write(map[string]any{
									"type": "message_start", "messageId": "u1",
									"message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": req["message"]}}, "timestamp": 1},
								})
								fp.write(map[string]any{
									"type": "message_end", "messageId": "u1",
									"message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": req["message"]}}, "timestamp": 1},
								})
								fp.write(map[string]any{
									"type": "message_start", "messageId": "a1",
									"message": map[string]any{"role": "assistant", "content": []map[string]any{}, "timestamp": 2},
								})
								fp.write(map[string]any{
									"type": "message_update", "messageId": "a1",
									"assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "hel"},
								})
								fp.write(map[string]any{
									"type": "message_update", "messageId": "a1",
									"assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "lo"},
								})
								fp.write(map[string]any{
									"type": "message_end", "messageId": "a1",
									"message": map[string]any{"role": "assistant", "content": []map[string]any{{"type": "text", "text": "hello"}}, "timestamp": 2},
								})
								fp.write(map[string]any{"type": "prompt_result", "id": id, "status": "completed", "sessionSettled": true})
								fp.write(map[string]any{"type": "session_settled"})
							case "abort":
								fp.write(map[string]any{"id": id, "type": "response", "command": "abort", "success": true})
							default:
								fp.write(map[string]any{"id": id, "type": "response", "command": typ, "success": true, "data": map[string]any{}})
							}
						}
					}
					if err != nil {
						return
					}
				}
			}()
			return fp, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !containsArg(gotArgs, "--mode", "rpc-ui") {
		t.Fatalf("args %v", gotArgs)
	}
	got := make(chan Event, 16)
	unsub := s.OnEvent(func(e Event) {
		if e.Type == "message" || e.Type == "prompt_result" || e.Type == "settled" {
			got <- e
		}
	})
	defer unsub()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := s.Prompt(ctx, "Say hi", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	var sawResult, sawSettled bool
	for !(sawResult && sawSettled) {
		select {
		case e := <-got:
			switch e.Type {
			case "prompt_result":
				if e.Status != "completed" {
					t.Fatalf("status %s", e.Status)
				}
				sawResult = true
			case "settled":
				sawSettled = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for prompt lifecycle")
		}
	}
	msgs := s.Messages()
	if len(msgs) < 2 {
		t.Fatalf("messages %#v", msgs)
	}
	var asst string
	for _, m := range msgs {
		if m.Role == "assistant" {
			asst = m.Text
		}
	}
	if asst != "hello" {
		t.Fatalf("assistant text %q", asst)
	}
	if s.Working() {
		t.Fatal("still working after settle")
	}
}

func containsArg(args []string, k, v string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == k && args[i+1] == v {
			return true
		}
	}
	return false
}

func TestListSessionsReadsHeaders(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "-tmp-demo")
	if err := writeSessionFile(dir, "2026-01-02T03-04-05-000Z_abc-123.jsonl", `{"type":"title","v":1,"title":"Demo title","updatedAt":"2026-01-02T03:05:00.000Z"}
{"type":"session","version":3,"id":"abc-123","timestamp":"2026-01-02T03:04:05.000Z","cwd":"/tmp/demo"}
`); err != nil {
		t.Fatal(err)
	}
	list, err := ListSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "abc-123" || list[0].Title != "Demo title" || list[0].Cwd != "/tmp/demo" {
		t.Fatalf("%+v", list)
	}
}

func writeSessionFile(dir, name, body string) error {
	if err := mkdirAll(dir); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, name), []byte(body))
}

func mkdirAll(dir string) error {
	return osMkdirAll(dir)
}

func writeFile(path string, b []byte) error {
	return osWriteFile(path, b)
}
