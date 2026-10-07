package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// DiffLine is one line of an edit; Old/New are 0 where the line has no number.
type DiffLine struct {
	Type    string // unchanged, added, removed
	Content string
	Old     int
	New     int
}

type Diff struct {
	Lines          []DiffLine
	Added, Removed int
}

// ParseDiff reads the Daemon's Edit / Create results, JSON with `diffLines`;
// anything else is shown as it came.
func ParseDiff(text string) *Diff {
	if !strings.HasPrefix(text, "{") || !strings.Contains(text, `"diffLines"`) {
		return nil
	}
	var parsed struct {
		DiffLines []json.RawMessage `json:"diffLines"`
	}
	if json.Unmarshal([]byte(text), &parsed) != nil || parsed.DiffLines == nil {
		return nil
	}
	d := &Diff{Lines: []DiffLine{}}
	for _, raw := range parsed.DiffLines {
		var e struct {
			Type       string `json:"type"`
			Content    any    `json:"content"`
			LineNumber struct {
				Old any `json:"old"`
				New any `json:"new"`
			} `json:"lineNumber"`
		}
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.Type != "unchanged" && e.Type != "added" && e.Type != "removed" {
			continue
		}
		l := DiffLine{Type: e.Type}
		l.Content, _ = e.Content.(string)
		if n, ok := e.LineNumber.Old.(float64); ok {
			l.Old = int(n)
		}
		if n, ok := e.LineNumber.New.(float64); ok {
			l.New = int(n)
		}
		d.add(l)
	}
	return d
}

func (d *Diff) add(l DiffLine) {
	d.Lines = append(d.Lines, l)
	switch l.Type {
	case "added":
		d.Added++
	case "removed":
		d.Removed++
	}
}

type Status struct {
	Success bool
	// The Daemon's own words when it gave any (`message` or `error`).
	Message string
}

// ParseStatus reads a small JSON answer such as
// `{"success":true,"file_path":"..."}` as a status line. Anything with more
// to say is shown as it came.
func ParseStatus(text string) *Status {
	if !strings.HasPrefix(text, "{") || !strings.Contains(text, `"success"`) {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal([]byte(text), &obj) != nil {
		return nil
	}
	ok, isBool := obj["success"].(bool)
	if !isBool {
		return nil
	}
	for k := range obj {
		switch k {
		case "success", "file_path", "path", "message", "error":
		default:
			return nil
		}
	}
	s := &Status{Success: ok}
	for _, k := range []string{"message", "error"} {
		if v, _ := obj[k].(string); strings.TrimSpace(v) != "" {
			s.Message = v
			break
		}
	}
	return s
}

// CreatedFileDiff shows the file a Create wrote, from the call's own input,
// as all-added lines: Create answers with a bare status.
func CreatedFileDiff(c *ToolCall) *Diff {
	content, ok := c.Input["content"].(string)
	if c.Use.Name != "Create" || !ok {
		return nil
	}
	content = strings.TrimSuffix(content, "\n")
	d := &Diff{}
	for i, line := range strings.Split(content, "\n") {
		d.add(DiffLine{Type: "added", Content: line, New: i + 1})
	}
	return d
}

// ToolName splits `<server>___<tool>`, the Daemon's name for an MCP tool;
// Server is empty for the Daemon's own tools.
func ToolName(name string) (server, tool string) {
	at := strings.Index(name, "___")
	if at <= 0 || at+3 >= len(name) {
		return "", name
	}
	return name[:at], name[at+3:]
}

// SummaryPart is a bare value for a well-known input, else `key: value`.
type SummaryPart struct {
	Key   string
	Value string
}

var headlineKeys = []string{"summary", "command", "file_path", "path", "pattern", "url", "query", "skill"}

// SummaryParts is what the row says after the tool's name. A headline input
// stands alone; any other tool shows its first few short inputs as
// `key: value` pairs.
func SummaryParts(c *ToolCall) []SummaryPart {
	for _, k := range headlineKeys {
		if v, ok := c.Input[k].(string); ok && strings.TrimSpace(v) != "" {
			return []SummaryPart{{Value: firstLine(v, 120)}}
		}
	}
	parts := []SummaryPart{}
	for _, k := range c.Keys {
		if len(parts) == 3 {
			break
		}
		switch v := c.Input[k].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				parts = append(parts, SummaryPart{k, firstLine(v, 60)})
			}
		case float64:
			parts = append(parts, SummaryPart{k, jsNumber(v)})
		case bool:
			parts = append(parts, SummaryPart{k, fmt.Sprint(v)})
		}
	}
	return parts
}

func Summary(c *ToolCall) string {
	var out []string
	for _, p := range SummaryParts(c) {
		if p.Key != "" {
			out = append(out, p.Key+": "+p.Value)
		} else {
			out = append(out, p.Value)
		}
	}
	return strings.Join(out, " · ")
}

func jsNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func firstLine(text string, max int) string {
	line, _, _ := strings.Cut(text, "\n")
	if r := []rune(line); len(r) > max {
		return string(r[:max-3]) + "…"
	}
	return line
}

func TruncateLines(text string, max int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= max {
		return text
	}
	return fmt.Sprintf("%s\n… %d more lines", strings.Join(lines[:max], "\n"), len(lines)-max)
}

// InputText is the call's input as the row's detail shows it: the command,
// the path, or the raw input.
func InputText(c *ToolCall) string {
	if cmd, _ := c.Input["command"].(string); cmd != "" {
		return "$ " + cmd
	}
	if p, ok := c.Input["file_path"].(string); ok {
		return p
	}
	if p, ok := c.Input["path"].(string); ok {
		return p
	}
	var b bytes.Buffer
	if json.Indent(&b, c.RawInput, "", "  ") != nil {
		return "{}"
	}
	return b.String()
}

// PermissionDetail is what a permission Prompt shows of the tool it asks
// about: the Daemon's details (the full command, the file) first, else the
// tool's input.
func PermissionDetail(details, input json.RawMessage) string {
	var d, in map[string]any
	_ = json.Unmarshal(details, &d)
	_ = json.Unmarshal(input, &in)
	if s, ok := d["fullCommand"].(string); ok {
		return "$ " + s
	}
	if s, ok := d["filePath"].(string); ok {
		return s
	}
	if s, ok := in["command"].(string); ok {
		return "$ " + s
	}
	var b bytes.Buffer
	if json.Compact(&b, input) != nil {
		return "{}"
	}
	return b.String()
}

type ScriptPermissionCall struct {
	// Where the call sits in the program, 1-based.
	Line   int
	Tool   string
	Detail string
	// The Daemon's impact level for this one call, when it gave one.
	Impact string
}

type ScriptPermission struct {
	Impact string
	Calls  []ScriptPermissionCall
}

// ParseScriptPermission reads the calls a Script asks for at once, each with
// the details its direct call would have asked with.
func ParseScriptPermission(details json.RawMessage) *ScriptPermission {
	var d struct {
		Type        string `json:"type"`
		ImpactLevel any    `json:"impactLevel"`
		Calls       []struct {
			ToolName     any             `json:"toolName"`
			Line         any             `json:"line"`
			ToolInput    json.RawMessage `json:"toolInput"`
			Confirmation json.RawMessage `json:"confirmation"`
		} `json:"calls"`
	}
	if json.Unmarshal(details, &d) != nil || d.Type != "script" || d.Calls == nil {
		return nil
	}
	p := &ScriptPermission{Calls: []ScriptPermissionCall{}}
	p.Impact, _ = d.ImpactLevel.(string)
	for _, c := range d.Calls {
		var conf struct {
			ImpactLevel any `json:"impactLevel"`
		}
		_ = json.Unmarshal(c.Confirmation, &conf)
		input := c.ToolInput
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		call := ScriptPermissionCall{Detail: PermissionDetail(c.Confirmation, input)}
		if n, ok := c.Line.(float64); ok {
			call.Line = int(n)
		}
		name, _ := c.ToolName.(string)
		_, call.Tool = ToolName(name)
		call.Impact, _ = conf.ImpactLevel.(string)
		p.Calls = append(p.Calls, call)
	}
	return p
}

// ClusterLabel is what a tool cluster's header says: the one tool's name or
// how many ran, with a Script counted as the calls it made. Pending while any
// call, or any call a Script made, is still to answer.
func ClusterLabel(calls []*ToolCall) (label string, pending bool) {
	leaves := LeafCalls(calls)
	for _, c := range append(append([]*ToolCall{}, calls...), leaves...) {
		if c.Result == nil {
			pending = true
		}
	}
	var what string
	switch {
	case len(leaves) == 1:
		_, what = ToolName(leaves[0].Use.Name)
	case len(leaves) == 0 && len(calls) > 0:
		_, what = ToolName(calls[0].Use.Name)
	case len(leaves) == 0:
		what = "a tool"
	default:
		what = fmt.Sprintf("%d tools", len(leaves))
	}
	if pending {
		return "Running " + what, true
	}
	return "Used " + what, false
}

// ToolResultView is the result's text and how it reads, for the row and its detail.
type ToolResultView struct {
	Text    string
	Images  []Image
	Pending bool
	IsError bool
	Diff    *Diff
	Status  *Status
}

func ReadResult(c *ToolCall) ToolResultView {
	v := ToolResultView{
		Text:    ResultText(c.Result),
		Images:  ResultImages(c.Result),
		Pending: c.Result == nil,
		IsError: c.Result != nil && c.Result.IsError != nil && *c.Result.IsError,
	}
	v.Diff = ParseDiff(v.Text)
	if v.Diff == nil && !v.IsError {
		v.Diff = CreatedFileDiff(c)
	}
	if v.Diff == nil {
		v.Status = ParseStatus(v.Text)
	}
	return v
}

// LeafCalls are the calls of a run of tool calls, with a Script counted as
// the calls it made.
func LeafCalls(calls []*ToolCall) []*ToolCall {
	out := []*ToolCall{}
	for _, c := range calls {
		if c.Lifecycle {
			out = append(out, c.Nested...)
		} else {
			out = append(out, c)
		}
	}
	return out
}

// ScriptRun is what a Script run shows. The Script's own result is what the
// model read: the program's text() output, a closing line with the run's
// figures, and a status as JSON.
type ScriptRun struct {
	Output string
	Images []Image
	// Empty while the call has no result, or when the result is not a run's.
	Status string
	Error  string
	// The closing line's figures: `3 calls · 86 B in sandbox · 33 B emitted (38%)`.
	Stats   string
	LogPath string
	Value   string
}

var (
	closingLine    = regexp.MustCompile(`^\[Script (?:completed|failed)[^\n]*\]$`)
	scriptStatuses = map[string]bool{"completed": true, "failed": true, "running": true, "stalled": true, "cancelled": true}
)

func ReadScriptRun(c *ToolCall) ScriptRun {
	run := ScriptRun{Images: ResultImages(c.Result)}
	parts := textParts(c.Result)
	if len(parts) == 0 {
		return run
	}
	env := parseEnvelope(parts[len(parts)-1])
	if env == nil {
		// Not a run's answer: the Daemon refused the call before it started.
		if c.Result.IsError != nil && *c.Result.IsError {
			run.Status, run.Error = "failed", strings.Join(parts, "\n")
		} else {
			run.Output = strings.Join(parts, "\n")
		}
		return run
	}
	parts = parts[:len(parts)-1]
	run.Status = env.Status
	run.Error, _ = env.Error.(string)
	if env.Result != nil {
		if s, ok := env.Result.(string); ok {
			run.Value = s
		} else {
			b, _ := json.MarshalIndent(env.Result, "", "  ")
			run.Value = string(b)
		}
	} else if p, ok := env.ResultPath.(string); ok {
		run.Value = "Saved to " + p
	}
	for i, p := range parts {
		if t := strings.TrimSpace(p); closingLine.MatchString(t) {
			run.Stats, run.LogPath = readClosingLine(t)
			parts = append(parts[:i:i], parts[i+1:]...)
			break
		}
	}
	run.Output = strings.Join(parts, "\n")
	return run
}

type envelope struct {
	Status     string `json:"status"`
	Error      any    `json:"error"`
	Result     any    `json:"result"`
	ResultPath any    `json:"resultPath"`
}

func parseEnvelope(text string) *envelope {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "{") || !strings.Contains(t, `"toolCallId"`) {
		return nil
	}
	var e envelope
	if json.Unmarshal([]byte(t), &e) != nil || !scriptStatuses[e.Status] {
		return nil
	}
	return &e
}

// `[Script completed · 3 calls · 86 B in sandbox · retained: r1 … · log: /path]`
func readClosingLine(line string) (stats, logPath string) {
	fields := strings.Split(line[1:len(line)-1], " · ")[1:]
	var kept []string
	for _, f := range fields {
		switch {
		case strings.HasPrefix(f, "log: "):
			logPath = f[5:]
		case strings.HasPrefix(f, "retained: "):
			// The handles name results for the model to look up again; they mean nothing on screen.
		default:
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " · "), logPath
}

func textParts(r *protocol.ToolResult) []string {
	s, parts, isString := resultParts(r)
	if isString {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var out []string
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		}
	}
	return out
}

type ScriptInput struct{ Name, Text string }

// ScriptSource is the program a Script runs and the long literals it reads
// as `inputs.<name>`.
func ScriptSource(c *ToolCall) (script string, inputs []ScriptInput, ok bool) {
	if c.Use.Name != ScriptTool {
		return "", nil, false
	}
	script, ok = c.Input["script"].(string)
	if !ok {
		return "", nil, false
	}
	inputs = []ScriptInput{}
	if raw, has := c.Use.Input["inputs"]; has {
		for _, k := range orderedKeys(raw) {
			if s, isStr := c.Input["inputs"].(map[string]any)[k].(string); isStr {
				inputs = append(inputs, ScriptInput{k, s})
			}
		}
	}
	return script, inputs, true
}

var toolCallPattern = regexp.MustCompile(`\btools\.([A-Za-z_$][\w$]*)(?:\.([A-Za-z_$][\w$]*))?\s*\(`)

// ScriptToolNames are the tools a program calls, in the order they first
// appear; an MCP tool (`tools.droi_memory.memory_search`) by its own name.
func ScriptToolNames(script string) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, m := range toolCallPattern.FindAllStringSubmatch(script, -1) {
		name := m[1]
		if m[2] != "" {
			name = m[2]
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// ScriptSummary is what a Script or WaitForScript row says after its name.
func ScriptSummary(c *ToolCall) string {
	if c.Use.Name == WaitForScriptTool {
		if k, _ := c.Input["kill"].(bool); k {
			return "stopped"
		}
		return "continued"
	}
	script, _, ok := ScriptSource(c)
	if !ok {
		return ""
	}
	return strings.Join(ScriptToolNames(script), " · ")
}

// orderedKeys are an object's keys in the order the JSON has them.
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}
