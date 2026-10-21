package host

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/omp"
)

type testProc struct {
	stdinR, stdoutR, stderrR *io.PipeReader
	stdinW, stdoutW, stderrW *io.PipeWriter
	wait                     chan struct{}
}

func newTestProc() *testProc {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	return &testProc{stdinR: inR, stdinW: inW, stdoutR: outR, stdoutW: outW, stderrR: errR, stderrW: errW, wait: make(chan struct{})}
}

func (p *testProc) Stdin() io.WriteCloser { return p.stdinW }
func (p *testProc) Stdout() io.ReadCloser { return p.stdoutR }
func (p *testProc) Stderr() io.ReadCloser { return p.stderrR }
func (p *testProc) Wait() error           { <-p.wait; return nil }
func (p *testProc) Kill() error {
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

func (p *testProc) write(v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	_, _ = p.stdoutW.Write(b)
}

func TestOMPManagerCreateListsAndCloses(t *testing.T) {
	fp := newTestProc()
	var mu sync.Mutex
	started := 0
	m := &OMPManager{
		Home: t.TempDir(),
		Locate: func() string {
			return "/bin/true"
		},
		Spawn: func(ctx context.Context, path string, args []string, env []string) (omp.Process, error) {
			mu.Lock()
			started++
			mu.Unlock()
			go func() {
				fp.write(map[string]any{"type": "ready", "protocolVersion": 1, "supportedProtocolVersions": []int{1, 2}})
				buf := make([]byte, 0, 2048)
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
							line := buf[:i]
							buf = buf[i+1:]
							var req map[string]any
							_ = json.Unmarshal(line, &req)
							id, _ := req["id"].(string)
							typ, _ := req["type"].(string)
							fp.write(map[string]any{"id": id, "type": "response", "command": typ, "success": true, "data": map[string]any{}})
						}
					}
					if err != nil {
						return
					}
				}
			}()
			return fp, nil
		},
		Now: func() time.Time { return time.Unix(1700000000, 0) },
	}
	h, err := m.Create(context.Background(), OMPCreate{Cwd: t.TempDir(), Title: "T", Model: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if h.ID == "" || h.Session == nil {
		t.Fatalf("%+v", h)
	}
	if !IsOMP(m.LiveSummaries()[0]) {
		t.Fatal(m.LiveSummaries())
	}
	if got := m.Get(h.ID); got == nil || got.Title != "T" {
		t.Fatalf("%+v", got)
	}
	if err := m.Close(h.ID); err != nil {
		t.Fatal(err)
	}
	if m.Get(h.ID) != nil {
		t.Fatal("still live")
	}
	mu.Lock()
	n := started
	mu.Unlock()
	if n != 1 {
		t.Fatalf("started %d", n)
	}
}
