package transcript

import (
	"bytes"
	"cmp"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

// Job is work a tool call left running once it answered: a command an
// Execute runs in the background, or a Script still going after its first
// answer. The Daemon tells it in its words to the model, not in the
// protocol, so Jobs reads those words; a Session that says none has none.
type Job struct {
	// ID is the tool use that started it (a Script's is its run).
	ID     string
	Script bool
	// Summary is what the call said it does; for a Script, the tools it calls.
	Summary    string
	Command    string
	PID        int
	OutputFile string
	StartedAt  float64
	Done       bool
	// EndedAt is 0 when the end is known only from the process being gone.
	EndedAt float64
	// ExitCode is a command's, once known: the Daemon tells the end live
	// but its details only in the record a load brings.
	ExitCode *int
	// Status is a Script's end as its run reported it: completed, failed,
	// cancelled; "" while unknown.
	Status string
	// ProcessID is the Daemon's id of the command's process while it lists
	// it among the Session's background processes (droid 1.0), which
	// daemon.kill_background_process stops; "" otherwise.
	ProcessID string
}

// ProcessJob is a running background process as the Daemon lists it.
func ProcessJob(p protocol.SessionBackgroundProcess) Job {
	return Job{ID: cmp.Or(p.ToolUseID, p.ID), ProcessID: p.ID, Summary: firstLine(p.Command, 120), Command: p.Command,
		PID: int(p.Pid), OutputFile: p.OutputFile, StartedAt: p.StartedAtMs}
}

// Failed is whether the job ended badly, as far as is known.
func (j Job) Failed() bool {
	return j.ExitCode != nil && *j.ExitCode != 0 || j.Status == "failed" || j.Status == "cancelled"
}

// RunningJobs counts the jobs not done.
func RunningJobs(jobs []Job) int {
	n := 0
	for _, j := range jobs {
		if !j.Done {
			n++
		}
	}
	return n
}

const (
	executeCompletion = "execute-completion:"
	scriptCompletion  = "script-completion:"
	backgroundStarted = "Background process started"
)

var (
	startedPID   = regexp.MustCompile(`^Background process started \(PID: (\d+)\)`)
	exitCode     = regexp.MustCompile(`(?m)^exit_status: code (-?\d+)`)
	terminalLine = regexp.MustCompile(`reached its terminal state with result (\{.*?\})\.`)
)

// Jobs are the jobs messages started, in the order they started. messages
// are a Session's stored ones, unfiltered: the records of a job's end are
// for the model only.
func Jobs(messages []protocol.FactoryDroidMessage) []Job {
	uses := map[string]protocol.ToolUse{}
	byID := map[string]*Job{}
	var order []string
	ends := map[string]func(*Job){}
	for _, m := range messages {
		switch {
		case strings.HasPrefix(m.ID, executeCompletion):
			text := messageText(m)
			at := m.CreatedAt
			ends[strings.TrimPrefix(m.ID, executeCompletion)] = func(j *Job) {
				j.Done, j.EndedAt = true, at
				if c := exitCode.FindStringSubmatch(text); c != nil {
					n, _ := strconv.Atoi(c[1])
					j.ExitCode = &n
				}
			}
			continue
		case strings.HasPrefix(m.ID, scriptCompletion):
			// script-completion:<session>:<run>:<tool use>
			id := m.ID[strings.LastIndexByte(m.ID, ':')+1:]
			status := terminalStatus(messageText(m))
			at := m.CreatedAt
			ends[id] = func(j *Job) {
				j.Done, j.EndedAt = true, at
				if status != "" {
					j.Status = status
				}
			}
			continue
		}
		for _, b := range m.Content {
			switch b.Type {
			case protocol.ContentBlockTypeToolUse:
				if use, ok := decode[protocol.ToolUse](b); ok {
					uses[use.ID] = use
				}
			case protocol.ContentBlockTypeToolResult:
				if !bytes.Contains(b.Raw, []byte(backgroundStarted)) && !bytes.Contains(b.Raw, []byte("toolCallId")) {
					continue
				}
				r, ok := decode[protocol.ToolResult](b)
				if !ok {
					continue
				}
				if j := startedJob(&r, uses[r.ToolUseID], m.CreatedAt); j != nil {
					if byID[j.ID] == nil {
						byID[j.ID] = j
						order = append(order, j.ID)
					}
					continue
				}
				run, status := runAnswer(&r)
				if run == "" {
					continue
				}
				use := uses[r.ToolUseID]
				switch {
				case status == "running" && use.Name == ScriptTool && byID[run] == nil:
					byID[run] = &Job{ID: run, Script: true, Summary: scriptTools(use), StartedAt: m.CreatedAt}
					order = append(order, run)
				case status != "running" && status != "stalled" && use.Name == WaitForScriptTool:
					at := m.CreatedAt
					ends[run] = func(j *Job) { j.Done, j.EndedAt, j.Status = true, at, status }
				}
			}
		}
	}
	out := make([]Job, 0, len(order))
	for _, id := range order {
		j := byID[id]
		if end := ends[id]; end != nil {
			end(j)
		}
		out = append(out, *j)
	}
	return out
}

// startedJob is the job an Execute's answer says it started in the
// background: `Background process started (PID: 97178)`, then the command
// and the file its output goes to, a line each.
func startedJob(r *protocol.ToolResult, use protocol.ToolUse, at float64) *Job {
	text := strings.TrimSpace(ResultText(r))
	m := startedPID.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	j := &Job{ID: r.ToolUseID, StartedAt: at}
	j.PID, _ = strconv.Atoi(m[1])
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "Command: "); ok && j.Command == "" {
			j.Command = v
		} else if v, ok := strings.CutPrefix(line, "Output: "); ok && j.OutputFile == "" {
			j.OutputFile = v
		}
	}
	input := decodeInput(use.Input)
	if s, _ := input["summary"].(string); strings.TrimSpace(s) != "" {
		j.Summary = firstLine(s, 120)
	} else {
		j.Summary = firstLine(j.Command, 120)
	}
	if c, _ := input["command"].(string); c != "" {
		j.Command = c
	}
	return j
}

// runAnswer is the run a Script or WaitForScript answer is about and the
// status it gave, from its closing JSON.
func runAnswer(r *protocol.ToolResult) (run, status string) {
	parts := textParts(r)
	if len(parts) == 0 {
		return "", ""
	}
	return runStatus(parts[len(parts)-1])
}

func runStatus(text string) (run, status string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "{") {
		return "", ""
	}
	var e struct {
		ToolCallID string `json:"toolCallId"`
		Status     string `json:"status"`
	}
	if json.Unmarshal([]byte(t), &e) != nil || !scriptStatuses[e.Status] {
		return "", ""
	}
	return e.ToolCallID, e.Status
}

func terminalStatus(text string) string {
	m := terminalLine.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	_, status := runStatus(m[1])
	return status
}

func scriptTools(use protocol.ToolUse) string {
	script, _ := decodeInput(use.Input)["script"].(string)
	return strings.Join(ScriptToolNames(script), " · ")
}

func messageText(m protocol.FactoryDroidMessage) string {
	var texts []string
	for _, b := range m.Content {
		if b.Type == protocol.ContentBlockTypeText {
			if t, ok := decode[protocol.TextBlock](b); ok {
				texts = append(texts, t.Text)
			}
		}
	}
	return strings.Join(texts, "\n")
}
