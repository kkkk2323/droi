package transcript

import (
	"bytes"
	"cmp"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unsafe"

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
	var r JobReader
	return r.Read(messages)
}

// JobReader reads a Session's jobs again and again as its messages change,
// reading each message once: Jobs decoded every tool call of the Session
// each time a message came, which was most of what a long Session's
// window spent while its agent worked.
type JobReader struct {
	seen map[string]*jobMessage
	gen  uint64
	// last are the messages the last Read read, in order: mostly the same
	// ones stand in the same places, found without a lookup.
	last, spare []*jobMessage
}

// jobMessage is what one message says about jobs, kept while its blocks
// stay the same.
type jobMessage struct {
	id     string
	gen    uint64
	blocks []rawRef
	// end is set for the record of a job's end: the job's id and how it ended.
	end *jobEnd
	// steps are its tool calls and the answers that start or end a job, in order.
	steps []jobStep
}

type jobEnd struct {
	id string
	// script: a Script's run ended, with status; else a command, with exitCode.
	script   bool
	exitCode *int
	status   string
}

// jobStep is a tool call (use set) or an answer about a job (started or run set).
type jobStep struct {
	use *protocol.ToolUse
	// tools is what a Script call calls, read once it is asked for.
	tools     *string
	toolUseID string
	started   *Job
	run       string
	status    string
}

// scriptTools is what the Script call s calls.
func (s *jobStep) scriptTools() string {
	if s.tools == nil {
		t := scriptTools(*s.use)
		s.tools = &t
	}
	return *s.tools
}

// rawRef is a block as the Store holds it: the same bytes in the same place.
type rawRef struct {
	typ  string
	data *byte
	n    int
}

func refOf(b protocol.ContentBlock) rawRef {
	return rawRef{typ: b.Type, data: unsafe.SliceData(b.Raw), n: len(b.Raw)}
}

// sameBlocks reports whether content is what refs were made from. The Store
// replaces a block it changes, so the same bytes in the same place are the
// same block.
func sameBlocks(refs []rawRef, content []protocol.ContentBlock) bool {
	if len(refs) != len(content) {
		return false
	}
	for i, b := range content {
		if refs[i] != refOf(b) {
			return false
		}
	}
	return true
}

func readJobMessage(m protocol.FactoryDroidMessage) *jobMessage {
	jm := &jobMessage{id: m.ID, blocks: make([]rawRef, len(m.Content))}
	for i, b := range m.Content {
		jm.blocks[i] = refOf(b)
	}
	switch {
	case strings.HasPrefix(m.ID, executeCompletion):
		jm.end = &jobEnd{id: strings.TrimPrefix(m.ID, executeCompletion)}
		if c := exitCode.FindStringSubmatch(messageText(m)); c != nil {
			n, _ := strconv.Atoi(c[1])
			jm.end.exitCode = &n
		}
		return jm
	case strings.HasPrefix(m.ID, scriptCompletion):
		// script-completion:<session>:<run>:<tool use>
		jm.end = &jobEnd{id: m.ID[strings.LastIndexByte(m.ID, ':')+1:], script: true, status: terminalStatus(messageText(m))}
		return jm
	}
	for _, b := range m.Content {
		switch b.Type {
		case protocol.ContentBlockTypeToolUse:
			if use, ok := decode[protocol.ToolUse](b); ok {
				use = sharedUse(b, use)
				jm.steps = append(jm.steps, jobStep{use: &use})
			}
		case protocol.ContentBlockTypeToolResult:
			if !bytes.Contains(b.Raw, []byte(backgroundStarted)) && !bytes.Contains(b.Raw, []byte("toolCallId")) {
				continue
			}
			r, ok := decode[protocol.ToolResult](b)
			if !ok {
				continue
			}
			if j := startedJob(&r, protocol.ToolUse{}, m.CreatedAt); j != nil {
				jm.steps = append(jm.steps, jobStep{toolUseID: r.ToolUseID, started: j})
				continue
			}
			if run, status := runAnswer(&r); run != "" {
				jm.steps = append(jm.steps, jobStep{toolUseID: r.ToolUseID, run: run, status: status})
			}
		}
	}
	return jm
}

// Read is Jobs(messages), reading again only the messages that changed
// since the last Read.
func (jr *JobReader) Read(messages []protocol.FactoryDroidMessage) []Job {
	if jr.seen == nil {
		jr.seen = map[string]*jobMessage{}
	}
	jr.gen++
	read := slices.Grow(jr.spare[:0], len(messages))[:len(messages)]
	wanted := map[string]bool{}
	kept := 0
	for i, m := range messages {
		var jm *jobMessage
		if i < len(jr.last) && jr.last[i].id == m.ID && m.ID != "" {
			jm = jr.last[i]
		} else {
			jm = jr.seen[m.ID]
		}
		// A message without an id, or with one twice in a list, is read anew.
		if m.ID == "" || jm == nil || jm.gen == jr.gen || !sameBlocks(jm.blocks, m.Content) {
			fresh := readJobMessage(m)
			if m.ID != "" && (jm == nil || jm.gen != jr.gen) {
				jr.seen[m.ID] = fresh
				kept++
			}
			jm = fresh
		} else {
			kept++
		}
		jm.gen = jr.gen
		read[i] = jm
		for _, s := range jm.steps {
			if s.use == nil {
				wanted[s.toolUseID] = true
			}
		}
	}
	if len(jr.seen) > kept {
		for id, jm := range jr.seen {
			if jm.gen != jr.gen {
				delete(jr.seen, id)
			}
		}
	}
	jr.last, jr.spare = read, jr.last

	// As Jobs did: an answer finds the calls that came before it.
	uses := map[string]*jobStep{}
	byID := map[string]*Job{}
	var order []string
	ends := map[string]func(*Job){}
	for i, jm := range read {
		at := messages[i].CreatedAt
		if e := jm.end; e != nil {
			ends[e.id] = func(j *Job) {
				j.Done, j.EndedAt = true, at
				if e.exitCode != nil {
					j.ExitCode = e.exitCode
				}
				if e.script && e.status != "" {
					j.Status = e.status
				}
			}
			continue
		}
		for k, s := range jm.steps {
			switch {
			case s.use != nil:
				if wanted[s.use.ID] {
					uses[s.use.ID] = &jm.steps[k]
				}
			case s.started != nil:
				if byID[s.started.ID] == nil {
					j := *s.started
					if use := uses[s.toolUseID]; use != nil {
						j.withUse(*use.use)
					}
					byID[j.ID] = &j
					order = append(order, j.ID)
				}
			default:
				use := uses[s.toolUseID]
				name := ""
				if use != nil {
					name = use.use.Name
				}
				switch {
				case s.status == "running" && name == ScriptTool && byID[s.run] == nil:
					byID[s.run] = &Job{ID: s.run, Script: true, Summary: use.scriptTools(), StartedAt: at}
					order = append(order, s.run)
				case s.status != "running" && s.status != "stalled" && name == WaitForScriptTool:
					status := s.status
					ends[s.run] = func(j *Job) { j.Done, j.EndedAt, j.Status = true, at, status }
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
	j.withUse(use)
	return j
}

// withUse takes the job's summary and command from the call that started it.
func (j *Job) withUse(use protocol.ToolUse) {
	input := decodeInput(use.Input)
	if s, _ := input["summary"].(string); strings.TrimSpace(s) != "" {
		j.Summary = firstLine(s, 120)
	} else {
		j.Summary = firstLine(j.Command, 120)
	}
	if c, _ := input["command"].(string); c != "" {
		j.Command = c
	}
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
