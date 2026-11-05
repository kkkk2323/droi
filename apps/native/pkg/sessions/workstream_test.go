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
