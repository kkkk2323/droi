package sessionview

import (
	"context"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// processKernel answers process.list from a function, so a test can hold one answer back.
type processKernel struct {
	*fakeKernel
	list func() []kernel.Process
}

func (p *processKernel) Processes(context.Context, kernel.ID) (kernel.ProcessesResult, error) {
	return kernel.ProcessesResult{Processes: p.list()}, nil
}

// An open reads the conversation's background processes, and of two reads in flight the later one's answer stands,
// whichever answers last: process.changed starts a read per change, and an older list must not undo a newer one.
func TestStoreKeepsTheNewestProcessList(t *testing.T) {
	running := []kernel.Process{{ID: "p-1", PID: 10, Command: "pnpm dev", Status: "running"}}
	stopped := []kernel.Process{{ID: "p-1", PID: 10, Command: "pnpm dev", Status: "stopped"}}
	first := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	k := &processKernel{fakeKernel: &fakeKernel{views: map[kernel.ID]kernel.View{7: {Conversation: kernel.Conversation{ID: 7}}}}}
	k.list = func() []kernel.Process {
		calls++
		switch calls {
		case 1:
			return running
		case 2:
			close(first)
			<-release
			return running
		default:
			return stopped
		}
	}
	st := NewStore(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.Open(ctx, k, 7, kernel.Window{Min: 100, Max: 200}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := st.State(7).Processes(); len(got) != 1 || got[0].Status != "running" {
		t.Fatalf("after the open the state lists %+v", got)
	}

	done := make(chan error)
	go func() { done <- st.ReadProcesses(ctx, k, 0, 7) }()
	<-first
	if err := st.ReadProcesses(ctx, k, 0, 7); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := st.State(7).Processes(); got[0].Status != "stopped" {
		t.Errorf("the older answer replaced the newer one: %+v", got)
	}
}
