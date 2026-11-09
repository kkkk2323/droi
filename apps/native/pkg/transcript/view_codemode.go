package transcript

import (
	"encoding/json"
	"fmt"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// CodemodeTool is Kernel's tool that runs a script calling the conversation's other tools: the Daemon's Script, in
// Kernel's words. Its row is the script card, and the calls the script made hang under it.
const CodemodeTool = "codemode"

// codemodeNested are the calls a finished script made, from its result's details: Pi's row for each call, and the
// arguments and answer Kernel adds (protocol README, "codemode"). parent is the codemode call's ID.
func codemodeNested(parent string, details json.RawMessage) []*ToolCall {
	var d struct {
		Calls []struct {
			ID        int64           `json:"id"`
			Name      string          `json:"name"`
			Args      string          `json:"args"`
			Arguments json.RawMessage `json:"arguments"`
			Status    string          `json:"status"`
			Error     string          `json:"error"`
			Output    string          `json:"output"`
			Truncated bool            `json:"truncated"`
			Diff      string          `json:"diff"`
		} `json:"calls"`
	}
	nested := []*ToolCall{}
	if len(details) == 0 || json.Unmarshal(details, &d) != nil {
		return nested
	}
	for _, c := range d.Calls {
		text := c.Output
		if text == "" {
			text = c.Error
		}
		result := &ToolResultView{Text: withoutTrailingNewlines(text)}
		switch c.Status {
		case "running":
			result.Pending = true
		case "cancelled":
			result.Interrupted = true
		case "error":
			result.IsError = true
		}
		if c.Truncated {
			result.Notes = l10n.L("The output is cut here; the script read all of it.")
		}
		if !result.IsError {
			result.Diff = ParsePiDiff(c.Diff)
		}
		nested = append(nested, nestedCall(fmt.Sprintf("%s/c%d", parent, c.ID), c.Name, rowArguments(c.Arguments, c.Args), result))
	}
	return nested
}

// EntryCodemodeDone is the entry a background script's end writes: its result, for the model and the card.
const EntryCodemodeDone = "droi.codemode.done"

// backgroundJob is the job a codemode result started, from its details' background: 0 for a script that ran here.
func backgroundJob(details json.RawMessage) int64 {
	if len(details) == 0 {
		return 0
	}
	var d struct {
		Background *struct {
			JobID int64 `json:"jobId"`
		} `json:"background"`
	}
	if json.Unmarshal(details, &d) != nil || d.Background == nil {
		return 0
	}
	return d.Background.JobID
}

// jobDone is a background script's end, as its entry carries it.
type jobDone struct {
	JobID   int64           `json:"jobId"`
	CallID  string          `json:"callId"`
	Status  string          `json:"status"`
	Output  string          `json:"output"`
	Details json.RawMessage `json:"details"`
}

func jobDoneOf(e kernel.Entry) (jobDone, bool) {
	var d jobDone
	if len(e.Data) == 0 || json.Unmarshal(e.Data, &d) != nil || d.CallID == "" {
		return jobDone{}, false
	}
	return d, true
}

// jobDoneBlock is the note a background script's end draws where it ended.
func jobDoneBlock(e kernel.Entry) (Block, bool) {
	d, ok := jobDoneOf(e)
	if !ok {
		return Block{}, false
	}
	text := l10n.L("Background script %d finished.", d.JobID)
	switch d.Status {
	case "failed":
		text = l10n.L("Background script %d failed.", d.JobID)
	case "stopped":
		text = l10n.L("Background script %d was stopped.", d.JobID)
	}
	return Block{Kind: Text, ID: entryID(e) + ":0:0", Text: text}, true
}

// liveNested are the calls running scripts are making now, by the script's call ID: pi.live's nestedTools, which
// hold a call until it settles. A script's card shows them until its result lists them all.
func liveNested(view kernel.View) map[string][]*ToolCall {
	raw := view.Doc("pi.live")
	if len(raw) == 0 {
		return nil
	}
	var live struct {
		NestedTools []struct {
			ParentCallID string          `json:"parentCallId"`
			CallID       string          `json:"callId"`
			Name         string          `json:"name"`
			Arguments    json.RawMessage `json:"arguments"`
			Status       string          `json:"status"`
			Output       string          `json:"output"`
			Summary      *struct {
				IsError bool   `json:"isError"`
				Error   string `json:"error"`
			} `json:"summary"`
		} `json:"nestedTools"`
	}
	if json.Unmarshal(raw, &live) != nil || len(live.NestedTools) == 0 {
		return nil
	}
	out := map[string][]*ToolCall{}
	for _, n := range live.NestedTools {
		result := &ToolResultView{Text: withoutTrailingNewlines(n.Output), Pending: n.Status != "done"}
		if n.Summary != nil && n.Summary.IsError {
			result.IsError = true
			if result.Text == "" {
				result.Text = n.Summary.Error
			}
		}
		out[n.ParentCallID] = append(out[n.ParentCallID], nestedCall(n.CallID, n.Name, n.Arguments, result))
	}
	return out
}

// rowArguments are a call's arguments as the card shows them: Kernel's whole object, or else Pi's row's JSON, which
// Pi cuts to 200 characters; a cut one is no object, and shows as the text it is.
func rowArguments(arguments json.RawMessage, preview string) json.RawMessage {
	if len(arguments) > 0 && arguments[0] == '{' {
		return arguments
	}
	if json.Valid([]byte(preview)) {
		return json.RawMessage(preview)
	}
	text, _ := json.Marshal(preview)
	return text
}

func nestedCall(id, name string, args json.RawMessage, result *ToolResultView) *ToolCall {
	return &ToolCall{Use: protocol.ToolUse{Type: "tool_use", ID: id, Name: name},
		Input: decodeInputObject(args), RawInput: args, Keys: orderedKeys(args), ResultView: result}
}
