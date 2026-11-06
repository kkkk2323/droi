package sessions

// WorkstreamTag is on every Session of a Workstream (ADR 0036): its metadata
// names the Workstream and the Session's role in it.
const WorkstreamTag = "droi.workstream"

// A Workstream Session's roles.
const (
	WorkstreamOrchestrator = "orchestrator"
	WorkstreamAgent        = "agent"
)

// Workstream is the Workstream a Session belongs to, its role in it, and
// for an agent its orchestrator's Session; "" for a Session of none.
func Workstream(tags []Tag) (id, role, orchestratorID string) {
	for _, t := range tags {
		if t.Name == WorkstreamTag {
			return t.Metadata["workstreamId"], t.Metadata["role"], t.Metadata["orchestratorId"]
		}
	}
	return "", "", ""
}

// WithoutWorkstreamAgents leaves out the agents, which their Workstream's
// page lists; the orchestrator stays.
func WithoutWorkstreamAgents(list []Summary) []Summary {
	var out []Summary
	for _, s := range list {
		if !IsWorkstreamAgent(s.Tags) {
			out = append(out, s)
		}
	}
	return out
}

// IsWorkstreamAgent reports a Workstream agent's Session.
func IsWorkstreamAgent(tags []Tag) bool {
	id, role, _ := Workstream(tags)
	return id != "" && role == WorkstreamAgent
}
