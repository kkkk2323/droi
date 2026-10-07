// Package subagents is Sessions the Daemon starts for a Task tool call. The
// list carries each one's calling Session and tool call; Clients keep them
// out of the Session list and reach them from the Session that called them
// (a port of the pure parts of subagents.ts; sessions.MainSessions is its
// mainSessions).
package subagents

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

// SessionRef is a crumb to a Session.
type SessionRef struct {
	SessionID string
	Title     string
}

// Of lists the subagents called from any of these Sessions, newest first.
func Of(list []sessions.Summary, callerIDs []string) []sessions.Summary {
	var out []sessions.Summary
	for _, s := range list {
		if s.CallingSessionID != "" && slices.Contains(callerIDs, s.CallingSessionID) {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b sessions.Summary) int { return int(b.UpdatedAt - a.UpdatedAt) })
	return out
}

// compactionChain is every link of the compaction chain a Session belongs
// to, latest first.
func compactionChain(list []sessions.Summary, sessionID string) []string {
	continuedBy := map[string]string{}
	parentOf := map[string]string{}
	for _, s := range list {
		if s.ParentID == "" {
			continue
		}
		continuedBy[s.ParentID] = s.SessionID
		parentOf[s.SessionID] = s.ParentID
	}
	latest := sessionID
	seen := map[string]bool{latest: true}
	for next, ok := continuedBy[latest]; ok && !seen[next]; next, ok = continuedBy[latest] {
		latest = next
		seen[latest] = true
	}
	chain := []string{latest}
	for id := parentOf[latest]; id != "" && !slices.Contains(chain, id); id = parentOf[id] {
		chain = append(chain, id)
	}
	return chain
}

// CallerTrail is the Sessions above a subagent, its main Session first; empty
// for a main Session. A caller that has since been compacted leads to the
// Session that continues it. A caller missing from the list still gets a
// crumb.
func CallerTrail(list []sessions.Summary, sessionID, callingSessionID string) []SessionRef {
	byID := map[string]sessions.Summary{}
	for _, s := range list {
		byID[s.SessionID] = s
	}
	var trail []SessionRef
	seen := map[string]bool{sessionID: true}
	for callerID := callingSessionID; callerID != "" && !seen[callerID]; {
		seen[callerID] = true
		caller, known := byID[callerID]
		latestID := compactionChain(list, callerID)[0]
		latest, hasLatest := byID[latestID]
		title := "Main session"
		if hasLatest {
			title = latest.Title
		} else if known {
			title = caller.Title
		}
		trail = append([]SessionRef{{SessionID: latestID, Title: title}}, trail...)
		callerID = caller.CallingSessionID
	}
	return trail
}

// Siblings are the subagents a subagent can switch to: everything its
// caller called, across the caller's compactions, as the caller's header
// lists them.
func Siblings(list []sessions.Summary, callingSessionID string) []sessions.Summary {
	if callingSessionID == "" {
		return nil
	}
	return Of(list, compactionChain(list, callingSessionID))
}

// ListedSessionOf is the listed row a Session belongs to: its main Session,
// at the latest link of that Session's compaction chain (see
// sessions.FoldContinued).
func ListedSessionOf(list []sessions.Summary, sessionID string) string {
	byID := map[string]sessions.Summary{}
	continuedBy := map[string]string{}
	for _, s := range list {
		byID[s.SessionID] = s
		if s.ParentID != "" {
			continuedBy[s.ParentID] = s.SessionID
		}
	}
	seen := map[string]bool{}
	id := sessionID
	for s, ok := byID[id]; ok && s.CallingSessionID != "" && !seen[id]; s, ok = byID[id] {
		seen[id] = true
		id = s.CallingSessionID
	}
	for next, ok := continuedBy[id]; ok && !seen[id]; next, ok = continuedBy[id] {
		seen[id] = true
		id = next
	}
	return id
}

// Status is a subagent's run as the Daemon reports it.
type Status string

const (
	Pending   Status = "pending"
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

// Run is the Daemon's account of one subagent's run.
type Run struct {
	Status       Status
	ToolUseCount *int
	DurationMs   *float64
}

func IsRunning(status Status) bool { return status == Running || status == Pending }

// FromSummary is the Run the Daemon's subagent invocation summary reports.
func FromSummary(sum protocol.SubagentInvocationSummary) *Run {
	r := Run{Status: Status(sum.Status), DurationMs: sum.DurationMs}
	if sum.ToolUseCount != nil {
		n := int(*sum.ToolUseCount)
		r.ToolUseCount = &n
	}
	return &r
}

// RunFrom is the Run for a subagent this Client has heard about: the
// Daemon's own summary when it has one, else running while the subagent's
// Session is doing anything (working is its working state, "" when unknown).
func RunFrom(summary *Run, working string) (Run, bool) {
	if summary != nil && summary.Status != "" {
		return *summary, true
	}
	if working != "" && working != "idle" {
		return Run{Status: Running}, true
	}
	return Run{}, false
}

// RunningCounts is how many subagents are running under each listed row.
func RunningCounts(list []sessions.Summary, runs map[string]Run) map[string]int {
	counts := map[string]int{}
	for _, s := range list {
		if s.CallingSessionID == "" || !IsRunning(runs[s.SessionID].Status) {
			continue
		}
		counts[ListedSessionOf(list, s.SessionID)]++
	}
	return counts
}

// Request is what a Task call asked for.
type Request struct {
	// SubagentType is the custom droid's name as the Daemon knows it, such as `explorer`.
	SubagentType string
	Description  string
	Prompt       string
}

func TaskRequest(call *transcript.ToolCall) Request {
	text := func(key string) string {
		s, _ := call.Input[key].(string)
		return s
	}
	return Request{SubagentType: text("subagent_type"), Description: text("description"), Prompt: text("prompt")}
}

// SubagentName turns `explorer` into `Explorer`, the way the Daemon titles the
// subagent's Session.
func SubagentName(subagentType string) string {
	if subagentType == "" {
		return "Subagent"
	}
	r, n := utf8.DecodeRuneInString(subagentType)
	return string(unicode.ToUpper(r)) + subagentType[n:]
}

const backgroundLaunch = "Task launched in background"

var launchedSession = regexp.MustCompile(`(?m)^session_id:\s*(\S+)\s*$`)

// LaunchedSessionID: a background Task answers at once with the subagent's
// Session id; "" when this is not such an answer.
func LaunchedSessionID(call *transcript.ToolCall) string {
	if m := launchedSession.FindStringSubmatch(resultText(call)); m != nil {
		return m[1]
	}
	return ""
}

func resultText(call *transcript.ToolCall) string {
	if call.Result == nil {
		return ""
	}
	return transcript.ResultText(call.Result)
}

// TaskState is what the Task call is doing: the Daemon's own account when
// this Client has one, otherwise what the tool result says. Launched is a
// background run nobody here has heard back from.
type TaskState string

const Launched TaskState = "launched"

func StateOf(call *transcript.ToolCall, run *Run) TaskState {
	switch {
	case call.Result != nil && call.Result.IsError != nil && *call.Result.IsError:
		return TaskState(Failed)
	case run != nil:
		return TaskState(run.Status)
	case call.Result == nil:
		return TaskState(Running)
	case strings.HasPrefix(resultText(call), backgroundLaunch):
		return Launched
	}
	return TaskState(Completed)
}

// Report is the subagent's report, once a waited-for Task has one; a launch
// notice is not a report.
func Report(call *transcript.ToolCall) string {
	text := resultText(call)
	if strings.HasPrefix(text, backgroundLaunch) {
		return ""
	}
	return text
}

// Links is what a transcript needs to link a Task call to its subagent.
type Links struct {
	// ByToolUse holds the listed subagents by the Task call that started them.
	ByToolUse map[string]sessions.Summary
	Runs      map[string]Run
}

func ByToolUse(list []sessions.Summary) map[string]sessions.Summary {
	m := map[string]sessions.Summary{}
	for _, s := range list {
		if s.CallingToolUseID != "" {
			m[s.CallingToolUseID] = s
		}
	}
	return m
}

// Link is a Task call as its row shows it.
type Link struct {
	Request Request
	State   TaskState
	Run     *Run
	Report  string
	// SessionID is the subagent's Session, "" until it is known.
	SessionID string
}

// LinkFor reads a Task call against what the Client knows; links may be nil.
func LinkFor(call *transcript.ToolCall, links *Links) Link {
	var sessionID string
	var run *Run
	if links != nil {
		sessionID = links.ByToolUse[call.Use.ID].SessionID
	}
	if sessionID == "" {
		sessionID = LaunchedSessionID(call)
	}
	if links != nil && sessionID != "" {
		if r, ok := links.Runs[sessionID]; ok {
			run = &r
		}
	}
	l := Link{Request: TaskRequest(call), State: StateOf(call, run), Run: run, Report: Report(call)}
	if links != nil {
		l.SessionID = sessionID
	}
	return l
}

// UnlistedTaskCalls names the Task calls whose subagent the list does not
// show, each with whether it has answered yet; empty when every one is listed.
func UnlistedTaskCalls(entries []*transcript.Entry, byToolUse map[string]sessions.Summary) string {
	var missing []string
	for _, entry := range entries {
		for _, block := range entry.Blocks {
			if block.Kind != transcript.Subagent {
				continue
			}
			if _, ok := byToolUse[block.Call.Use.ID]; ok {
				continue
			}
			name := block.Call.Use.ID
			if block.Call.Result != nil {
				name += ":answered"
			}
			missing = append(missing, name)
		}
	}
	return strings.Join(missing, ",")
}

func FormatRunDuration(ms float64) string {
	seconds := int(math.Round(ms / 1000))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds%60)
	}
	return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
}
