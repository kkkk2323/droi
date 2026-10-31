package transcript

import (
	"encoding/json"
	"regexp"
	"strings"
)

// SpecState is where an ExitSpecMode call's plan stands.
type SpecState uint8

const (
	// SpecPending waits for the user's answer.
	SpecPending SpecState = iota
	SpecApproved
	// SpecRejected kept Spec mode on, to plan further.
	SpecRejected
)

// SpecPlan is an ExitSpecMode call as its card shows it.
type SpecPlan struct {
	Title, Plan string
	State       SpecState
	// NewSession: approved to be carried out in a new Session.
	NewSession bool
	// Comment is what the user added to the approval.
	Comment string
}

// SpecHandoff is the first message of a Session started from an approved
// spec, as droid writes it: the plan, the note the user approved it with,
// and the file that holds the planning Session's transcript or the spec.
type SpecHandoff struct {
	Note, Plan, Source string
}

const (
	specHandoffOpening = "Implement the following plan:\n\n"
	specHandoffNote    = "User note: "
)

var specHandoffClosing = regexp.MustCompile(`\n\nIf you need specific details from the planning session[^\n]*, read the full (?:transcript|spec) at: ([^\n]+)$`)

// ReadSpecHandoff reads a message's text as a spec handoff. Both its opening
// line and its closing pointer have to be there, as droid's own check asks,
// so a typed message that only starts the same way stays text.
func ReadSpecHandoff(text string) (SpecHandoff, bool) {
	rest, ok := strings.CutPrefix(text, specHandoffOpening)
	if !ok {
		return SpecHandoff{}, false
	}
	at := specHandoffClosing.FindStringSubmatchIndex(rest)
	if at == nil {
		return SpecHandoff{}, false
	}
	h := SpecHandoff{Source: rest[at[2]:at[3]]}
	body := rest[:at[0]]
	if note, plan, ok := strings.Cut(body, "\n\n"); ok && strings.HasPrefix(note, specHandoffNote) {
		h.Note, body = strings.TrimPrefix(note, specHandoffNote), plan
	}
	h.Plan = strings.TrimSpace(body)
	return h, true
}

// PlanTitle is a plan's first heading, else its first line.
func PlanTitle(plan string) string {
	first := ""
	for _, line := range strings.Split(plan, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if heading := strings.TrimLeft(line, "#"); heading != line {
			return strings.TrimSpace(heading)
		}
		if first == "" {
			first = line
		}
	}
	return first
}

// ReadSpec reads an ExitSpecMode call: its plan and title, and from its
// result whether the user approved it. The Daemon's result is JSON on
// approval and an error text when the user asked to plan further.
func ReadSpec(c *ToolCall) SpecPlan {
	s := SpecPlan{}
	s.Plan, _ = c.Input["plan"].(string)
	s.Title, _ = c.Input["title"].(string)
	if c.Result == nil {
		return s
	}
	if c.Result.IsError != nil && *c.Result.IsError {
		s.State = SpecRejected
		return s
	}
	var res struct {
		Approved    *bool  `json:"approved"`
		UserComment string `json:"userComment"`
		Handoff     *struct {
			IsNewSession bool `json:"isNewSession"`
		} `json:"handoff"`
	}
	text := strings.TrimSpace(ResultText(c.Result))
	if json.Unmarshal([]byte(text), &res) != nil || res.Approved == nil {
		s.State = SpecApproved
		return s
	}
	s.State = SpecRejected
	if *res.Approved {
		s.State = SpecApproved
	}
	s.Comment = res.UserComment
	s.NewSession = res.Handoff != nil && res.Handoff.IsNewSession
	return s
}
