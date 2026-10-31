package sessionview

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

func TestTheDaemonsBackgroundProcessesJoinTheJobs(t *testing.T) {
	jobs := []transcript.Job{
		{ID: "toolu_a", Summary: "Dev server", PID: 11},
		{ID: "toolu_b", Summary: "Build", Done: true},
	}
	byID := map[string]int{"toolu_a": 0, "toolu_b": 1}
	procs := []protocol.SessionBackgroundProcess{
		{ID: "toolu_a", ToolUseID: "toolu_a", Pid: 11, Command: "npm run dev"},
		{ID: "toolu_b", ToolUseID: "toolu_b", Pid: 12, Command: "make"},
		// Its start is older than the messages a load brought.
		{ID: "toolu_c", ToolUseID: "toolu_c", Pid: 13, Command: "tail -f log\nmore", StartedAtMs: 5, OutputFile: "/tmp/c.log"},
		{ID: "toolu_d", ToolUseID: "toolu_d", Pid: 14, Command: "sleep 9"},
	}
	got := withProcesses(jobs, byID, map[string]bool{"toolu_d": true}, procs)
	if len(got) != 3 {
		t.Fatalf("jobs %+v", got)
	}
	if got[0].ProcessID != "toolu_a" || got[0].Summary != "Dev server" {
		t.Fatalf("the listed job %+v", got[0])
	}
	if got[1].ProcessID != "" {
		t.Fatalf("an ended job can be stopped: %+v", got[1])
	}
	if c := got[2]; c.ID != "toolu_c" || c.ProcessID != "toolu_c" || c.Summary != "tail -f log" || c.PID != 13 || c.OutputFile != "/tmp/c.log" || c.StartedAt != 5 || c.Done {
		t.Fatalf("the job only the Daemon knows %+v", c)
	}
	if jobs[0].ProcessID != "" {
		t.Fatal("withProcesses changed the jobs it was given")
	}
	if got := withProcesses(jobs, byID, nil, nil); &got[0] != &jobs[0] {
		t.Fatal("no processes copied the jobs")
	}
}
