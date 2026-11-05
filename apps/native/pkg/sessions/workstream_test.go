package sessions

import "testing"

func TestTheWorkstreamTagNamesTheWorkstreamAndTheRole(t *testing.T) {
	agent := []Tag{{Name: DraftTag}, {Name: WorkstreamTag, Metadata: map[string]string{"workstreamId": "w1", "role": "agent", "orchestratorId": "o1", "agentModel": "m"}}}
	id, role, orch := Workstream(agent)
	if id != "w1" || role != WorkstreamAgent || orch != "o1" || !IsWorkstreamAgent(agent) {
		t.Fatalf("agent: %q %q %q", id, role, orch)
	}
	lead := []Tag{{Name: WorkstreamTag, Metadata: map[string]string{"workstreamId": "w1", "role": "orchestrator"}}}
	if id, role, orch := Workstream(lead); id != "w1" || role != WorkstreamOrchestrator || orch != "" || IsWorkstreamAgent(lead) {
		t.Fatalf("orchestrator: %q %q %q", id, role, orch)
	}
	if id, role, _ := Workstream([]Tag{{Name: ScratchTag}}); id != "" || role != "" || IsWorkstreamAgent(nil) {
		t.Fatal("a Session of no Workstream")
	}
}

func TestWorkstreamAgentsLeaveTheListAndTheRecentWorkspaces(t *testing.T) {
	tag := func(role string) []Tag {
		return []Tag{{Name: WorkstreamTag, Metadata: map[string]string{"workstreamId": "w1", "role": role}}}
	}
	list := []Summary{
		{SessionID: "lead", Cwd: "/w/pay", UpdatedAt: 10, Tags: tag(WorkstreamOrchestrator)},
		{SessionID: "agent", Cwd: "/w/pay-worktrees/grpc", UpdatedAt: 30, Tags: tag(WorkstreamAgent)},
		{SessionID: "plain", Cwd: "/w/beta", UpdatedAt: 20},
	}
	eq(t, ids(WithoutWorkstreamAgents(list)), []string{"lead", "plain"})
	var paths []string
	for _, w := range RecentWorkspaces(list, nil) {
		paths = append(paths, w.Path)
	}
	eq(t, paths, []string{"/w/beta", "/w/pay"})
}
