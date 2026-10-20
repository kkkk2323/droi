package transcript

import (
	"encoding/json"
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
