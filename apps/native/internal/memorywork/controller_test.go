package memorywork

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

// The interface Settings → Memory uses.
var _ interface {
	Overview() (Overview, error)
	Consolidate(workspace string) (Result, error)
	ResetPrompts() error
	Folder() string
} = (*Controller)(nil)

type controllerFixture struct {
	root, memoryDir string
	c               *Controller
	mu              sync.Mutex
	requests        []Request
	changes         atomic.Int32
	daemonUp        atomic.Bool
}

func newControllerFixture(t *testing.T) *controllerFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &controllerFixture{root: root, memoryDir: filepath.Join(root, "memory")}
	f.daemonUp.Store(true)
	run := func(_ context.Context, r Request) (json.RawMessage, error) {
		f.mu.Lock()
		f.requests = append(f.requests, r)
		f.mu.Unlock()
		if strings.HasPrefix(r.Title, "Memory: extract") {
			return json.RawMessage(`{"entries":[{"scope":"project","category":"insight","text":"learned"}]}`), nil
		}
		var input struct{ Entries []struct{ ID string } }
		_ = json.Unmarshal([]byte(r.Input), &input)
		keep := []string{}
		for _, e := range input.Entries {
			keep = append(keep, e.ID)
		}
		return json.Marshal(map[string]any{"keep": keep, "rewrite": []any{}, "remove": []any{}, "merge": []any{}})
	}
	f.c = New(Options{
		MemoryDir: f.memoryDir,
		ModelID:   func() string { return "glm-5.3-flash" },
		OnChange:  func() { f.changes.Add(1) },
		Runner: func() Run {
			if f.daemonUp.Load() {
				return run
			}
			return nil
		},
	})
	t.Cleanup(f.c.Stop)
	return f
}

func (f *controllerFixture) seed(t *testing.T, workspace string, count int) {
	t.Helper()
	store, err := memory.OpenStore(f.memoryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := range count {
		if _, err := store.Add(memory.Slot{Scope: memory.ScopeProject, Workspace: workspace}, "convention", "fact "+string(rune('0'+i))); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *controllerFixture) writeRequest(t *testing.T, sessionID string) string {
	t.Helper()
	transcript := filepath.Join(f.root, sessionID+".jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`), 0o666); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.memoryDir, "requests")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, sessionID+".json")
	body, _ := json.Marshal(map[string]any{"sessionId": sessionID, "transcriptPath": transcript, "cwd": f.root, "event": "SessionEnd", "requestedAt": ""})
	if err := os.WriteFile(file, body, 0o666); err != nil {
		t.Fatal(err)
	}
	return file
}

func (f *controllerFixture) titles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, r := range f.requests {
		out = append(out, r.Title)
	}
	return out
}

func gone(file string) bool {
	_, err := os.Stat(file)
	return os.IsNotExist(err)
}

func poll(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestControllerListsEveryProjectMemoryAndTheGlobalMemoryWithItsLimits(t *testing.T) {
	f := newControllerFixture(t)
	f.seed(t, "/Users/dev/app", 3)
	got, err := f.c.Overview()
	if err != nil {
		t.Fatal(err)
	}
	want := Overview{Rows: []Row{
		{Workspace: "/Users/dev/app", Entries: 3, Chars: 18, SoftLimit: 200_000, NeverFound: 3},
		{Workspace: "", SoftLimit: 40_000},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("overview %+v", got)
	}
}

func TestControllerConsolidatesOneMemoryWithTheChosenModel(t *testing.T) {
	f := newControllerFixture(t)
	f.seed(t, "/Users/dev/app", 3)
	result, err := f.c.Consolidate("/Users/dev/app")
	if err != nil || result != (Result{Applied: 1}) {
		t.Fatalf("result %+v %v", result, err)
	}
	if r := f.requests[0]; r.ModelID != "glm-5.3-flash" || !strings.Contains(r.Prompt, "consolidating") {
		t.Errorf("request %+v", r)
	}
	if o, _ := f.c.Overview(); o.Rows[0].LastConsolidated == "" {
		t.Error("not marked consolidated")
	}
	if n := f.changes.Load(); n < 2 {
		t.Errorf("%d changes", n)
	}
}

func TestControllerRefusesToConsolidateWithoutADaemon(t *testing.T) {
	f := newControllerFixture(t)
	f.daemonUp.Store(false)
	if _, err := f.c.Consolidate(""); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("error %v", err)
	}
}

func TestControllerRefusesASecondConsolidationOfTheSameMemory(t *testing.T) {
	f := newControllerFixture(t)
	f.seed(t, "/Users/dev/app", 2)
	release := make(chan struct{})
	started := make(chan struct{})
	f.c.o.Runner = func() Run {
		return func(ctx context.Context, r Request) (json.RawMessage, error) {
			close(started)
			<-release
			return json.RawMessage(`"prose"`), nil
		}
	}
	done := make(chan error, 1)
	go func() { _, err := f.c.Consolidate("/Users/dev/app"); done <- err }()
	<-started
	if o, _ := f.c.Overview(); !o.Rows[0].Consolidating {
		t.Error("the row is not shown as consolidating")
	}
	if _, err := f.c.Consolidate("/Users/dev/app"); err == nil || err.Error() != "This Memory is already being consolidated." {
		t.Errorf("error %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if o, _ := f.c.Overview(); o.Rows[0].Consolidating {
		t.Error("still consolidating")
	}
}

func TestControllerRunsAnExtractionForEveryRequestFileThenRemovesIt(t *testing.T) {
	f := newControllerFixture(t)
	file := f.writeRequest(t, "s-1")
	f.c.WatchRequests()
	poll(t, "the first request", func() bool { return gone(file) })
	if got := f.titles(); !reflect.DeepEqual(got, []string{"Memory: extract from s-1"}) {
		t.Errorf("titles %q", got)
	}
	later := f.writeRequest(t, "s-2")
	poll(t, "the later request", func() bool { return gone(later) })
	if got := f.titles(); len(got) != 2 {
		t.Errorf("titles %q", got)
	}
	o, err := f.c.Overview()
	if err != nil || o.Rows[0].Workspace != f.root || o.Rows[0].Entries != 2 {
		t.Errorf("overview %+v %v", o, err)
	}
}

func TestControllerLeavesRequestsWaitingWhileThereIsNoDaemon(t *testing.T) {
	f := newControllerFixture(t)
	f.daemonUp.Store(false)
	file := f.writeRequest(t, "s-3")
	f.c.ProcessRequests()
	if gone(file) {
		t.Fatal("the request was handled without a Daemon")
	}
	f.daemonUp.Store(true)
	f.c.ProcessRequests()
	if !gone(file) {
		t.Error("the request is still waiting")
	}
}
