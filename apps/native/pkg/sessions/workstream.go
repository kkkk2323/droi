package sessions

import "slices"

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

// WorkstreamSymbols are the icons a Workstream can take, as the Host keeps
// them (Lucide names), in the order the picker lists them.
var WorkstreamSymbols = []string{
	"target", "flag", "sparkles", "bug", "code", "terminal", "git-branch", "git-pull-request",
	"workflow", "layers", "shield", "lock", "flask-conical", "activity", "wrench", "server",
	"cloud", "chart-column", "book", "file-text", "palette", "bot", "factory", "monitor",
}

// DefaultWorkstreamSymbol is the icon of a Workstream that has none.
const DefaultWorkstreamSymbol = "flag"

// WorkstreamSymbol is the icon an orchestrator's tag names, the default for
// a Workstream without one or with one this build has no icon for.
func WorkstreamSymbol(tags []Tag) string {
	for _, t := range tags {
		if t.Name != WorkstreamTag {
			continue
		}
		if s := t.Metadata["symbol"]; slices.Contains(WorkstreamSymbols, s) {
			return s
		}
	}
	return DefaultWorkstreamSymbol
}

// IsWorkstreamOrchestrator reports a Workstream orchestrator's Session.
func IsWorkstreamOrchestrator(tags []Tag) bool {
	id, role, _ := Workstream(tags)
	return id != "" && role == WorkstreamOrchestrator
}

// Orchestrators are the Sessions of list that lead a Workstream.
func Orchestrators(list []Summary) []Summary {
	var out []Summary
	for _, s := range list {
		if IsWorkstreamOrchestrator(s.Tags) {
			out = append(out, s)
		}
	}
	return out
}

// WithoutWorkstreamSessions leaves out every Session of a Workstream: the
// agents show in their Workstream's side panel and the orchestrators in the
// sidebar's Workstreams.
func WithoutWorkstreamSessions(list []Summary) []Summary {
	var out []Summary
	for _, s := range list {
		if id, _, _ := Workstream(s.Tags); id == "" {
			out = append(out, s)
		}
	}
	return out
}

// WithoutWorkstreamAgents leaves out the agents, which their Workstream's
// side panel lists; the orchestrator stays.
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
