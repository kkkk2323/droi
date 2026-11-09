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
		nested = append(nested, nestedCall(fmt.Sprintf("%s/c%d", parent, c.ID), c.Name, rowArguments(c.Arguments, c.Args), result))
	}
	return nested
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
