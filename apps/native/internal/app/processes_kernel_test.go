//go:build !windows

package app

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// A command the model starts with bash_background is a background task of the conversation: the header counts
// it, its row says it runs, and Stop ends it through Kernel.
func TestAKernelBackgroundProcessStopsFromTheList(t *testing.T) {
	scriptKernel(t, func(sc *fakekernel.Scenario) {
		sc.Steps = append([]fakekernel.Step{
			{Match: "Start the server", Call: &fakekernel.Call{Name: "bash_background", ID: "call_bg", Arguments: map[string]any{"command": "sleep 30"}}},
			{Match: "Start the server", Text: "The server runs."},
		}, sc.Steps...)
	})
	h := kernelHarness(t)
	conv, _ := h.kernel.id(h.d.Sessions[1].SessionID)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	conn := h.testConn()
	if _, err := conn.Configure(ctx, kernel.ConfigureParams{ID: conv, Change: &kernel.AgentChange{Cwd: kernel.Some(t.TempDir())}}); err != nil {
		t.Fatal(err)
	}
	h.openSession("Add dark mode toggle")
	h.send("Start the server")
	// The Session defaults' level asks before a command runs.
	h.until("the approval card", func() bool { return h.hasText("Droi wants to run bash_background") })
	h.click("Allow once")
	h.until("the header's count", func() bool {
		return h.hasText("1 background task") && h.hasText("The server runs.") && h.hasText("Running in background · ")
	})
	listed, err := conn.Processes(ctx, conv)
	if err != nil || len(listed.Processes) != 1 {
		t.Fatalf("process.list: %v %+v", err, listed)
	}
	pid := listed.Processes[0].PID
	if syscall.Kill(pid, 0) != nil {
		t.Fatal("the process is not running")
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	h.click("1 background task")
	h.until("Stop", func() bool { return h.hasButton("Stop sleep 30") })
	h.click("Stop sleep 30")
	h.until("the task stopped", func() bool { return !h.hasText("1 background task") && h.hasText("Stopped") })
	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the process still runs after Stop")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
