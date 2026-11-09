package sessionview

import (
	"context"
	"regexp"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/subagents"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// taskIDTrailer is the line a foreground subagent's answer ends with for the model: how to resume it. The card has
// its own Open button for that.
var taskIDTrailer = regexp.MustCompile(`\n*\(task_id: \d+; pass it as resume to go on in this subagent\.\)\s*$`)

// kernelLink is a Kernel task call as the subagent card draws it: the call's arguments, the child the Kernel made,
// and how it stands.
func (v *View) kernelLink(call *transcript.ToolCall) subagents.Link {
	s := call.Subagent
	link := subagents.Link{Request: subagents.Request{SubagentType: s.Type, Description: s.Description,
		Prompt: s.Prompt, Resume: s.Resume}}
	if s.ConversationID != 0 {
		link.SessionID = v.sessionOf(s.ConversationID)
	}
	result := transcript.ReadResult(call)
	switch {
	case s.Status == "completed" || s.Status == "failed":
		link.State = subagents.TaskState(s.Status)
	case s.Status == "stopped" || (result.Interrupted && s.Status == "running" && !s.Background):
		link.State = "cancelled"
	case result.IsError && !result.Pending:
		link.State = "failed"
	case s.Status == "running":
		link.State = "running"
	default:
		link.State = "pending"
	}
	if s.Background {
		link.Report = s.Output
	} else if !result.Pending {
		link.Report = taskIDTrailer.ReplaceAllString(result.Text, "")
	}
	return link
}

// sessionOf is the window's id for conversation conv: Env.SessionOf's, else the id as the contract writes it.
func (v *View) sessionOf(conv kernel.ID) string {
	if v.env.SessionOf != nil {
		return v.env.SessionOf(conv)
	}
	return conv.String()
}

func (v *View) canStopSubagent(call *transcript.ToolCall) bool {
	if call.Subagent != nil {
		return v.env.Kernel != nil
	}
	return v.env.Controller != nil
}

// stopSubagent stops a subagent's run, as task_stop does: the Kernel's child, or the Daemon's Session.
func (v *View) stopSubagent(call *transcript.ToolCall, sessionID string) {
	if call.Subagent == nil {
		StopSubagent(v.env.Ctx, v.env.Controller, sessionID)
		return
	}
	ctx := v.env.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	conv := call.Subagent.ConversationID
	k := v.env.Kernel
	go func() { _ = k.Abort(ctx, kernel.AbortParams{ID: conv}) }()
}
