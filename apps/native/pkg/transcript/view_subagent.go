package transcript

import (
	"encoding/json"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// KernelTaskTool is Kernel's tool that starts a subagent: Factory Droid's Task, in Kernel's words. Its row is the
// subagent card, which opens the subagent's conversation.
const KernelTaskTool = "task"

// EntrySubagentDone is the entry a background subagent's end writes into its parent: its answer, for the model and
// the card.
const EntrySubagentDone = "droi.subagent.done"

// KernelSubagent is a task call's subagent as the Kernel reports it: the call's arguments, and the child
// conversation's id and status from the call's details (pi.live's while it runs) or, for a background one, the entry
// its end wrote.
type KernelSubagent struct {
	// ConversationID is the child, 0 until the Kernel has made it.
	ConversationID kernel.ID
	Type           string
	Description    string
	Prompt         string
	// Resume is the task_id the call went on in, "" for a new subagent.
	Resume     string
	Background bool
	// Status is running, completed, failed or stopped; "" before the call started.
	Status string
	// Output is what a background subagent answered, once it ended; a foreground one's answer is the call's result.
	Output string
}

// subagentState is what the Kernel said of a task call's subagent: details, or a background end's data.
type subagentState struct {
	ConversationID int64  `json:"conversationId"`
	SubagentType   string `json:"subagentType"`
	Description    string `json:"description"`
	Status         string `json:"status"`
	Background     bool   `json:"background"`
	CallID         string `json:"callId"`
	Output         string `json:"output"`
}

func subagentStateOf(raw json.RawMessage) (subagentState, bool) {
	var s subagentState
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil || s.ConversationID == 0 {
		return subagentState{}, false
	}
	return s, true
}

// kernelSubagent is call id's subagent, from its arguments and what the Kernel said of it.
func (bl *Builder) kernelSubagent(id string, input map[string]any) *KernelSubagent {
	str := func(key string) string {
		s, _ := input[key].(string)
		return s
	}
	background, _ := input["run_in_background"].(bool)
	sub := &KernelSubagent{Type: str("subagent_type"), Description: str("description"), Prompt: str("prompt"),
		Resume: str("resume"), Background: background}
	state, ok := bl.subagents[id]
	if !ok {
		state, ok = bl.liveSubagents[id]
	}
	if ok {
		sub.ConversationID = kernel.ID(state.ConversationID)
		sub.Status = state.Status
		if state.SubagentType != "" {
			sub.Type = state.SubagentType
		}
	}
	if done, ok := bl.subagentsDone[id]; ok {
		sub.ConversationID = kernel.ID(done.ConversationID)
		sub.Status, sub.Output = done.Status, done.Output
	}
	return sub
}

// liveSubagents are the details of the task calls running now, by call ID: pi.live's tools, which carry the child
// from the moment it exists.
func liveSubagents(view kernel.View) map[string]subagentState {
	raw := view.Doc("pi.live")
	if len(raw) == 0 {
		return nil
	}
	var live struct {
		Tools []struct {
			CallID  string          `json:"callId"`
			Name    string          `json:"name"`
			Details json.RawMessage `json:"details"`
		} `json:"tools"`
	}
	if json.Unmarshal(raw, &live) != nil {
		return nil
	}
	var out map[string]subagentState
	for _, slot := range live.Tools {
		if slot.Name != KernelTaskTool {
			continue
		}
		if s, ok := subagentStateOf(slot.Details); ok {
			if out == nil {
				out = map[string]subagentState{}
			}
			out[slot.CallID] = s
		}
	}
	return out
}

// subagentDoneBlock is the note a background subagent's end draws where it ended.
func subagentDoneBlock(e kernel.Entry) (Block, bool) {
	d, ok := subagentStateOf(e.Data)
	if !ok {
		return Block{}, false
	}
	text := l10n.L("Subagent “%s” finished.", d.Description)
	switch d.Status {
	case "failed":
		text = l10n.L("Subagent “%s” failed.", d.Description)
	case "stopped":
		text = l10n.L("Subagent “%s” was stopped.", d.Description)
	}
	return Block{Kind: Text, ID: entryID(e) + ":0:0", Text: text}, true
}
