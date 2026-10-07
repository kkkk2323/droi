// Memory's hooks (ADR 0010), one entry for every event: the deterministic half
// of Memory. Recall and saving are salience problems, so the hooks put the
// policy and the user's corrections in front of the model, nudge it to save,
// and fall back to an extraction when a long Session saved nothing.

package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// HookInput is what the Daemon passes a hook on stdin; a field that is absent,
// empty or not a string is "".
type HookInput struct {
	HookEventName  string
	SessionID      string
	TranscriptPath string
	Cwd            string
	Prompt         string
	Source         string
}

// ParseHookInput reads a hook's stdin.
func ParseHookInput(data []byte) (HookInput, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return HookInput{}, err
	}
	str := func(key string) string {
		s, _ := raw[key].(string)
		return s
	}
	return HookInput{
		HookEventName:  str("hook_event_name"),
		SessionID:      str("session_id"),
		TranscriptPath: str("transcript_path"),
		Cwd:            str("cwd"),
		Prompt:         str("prompt"),
		Source:         str("source"),
	}, nil
}

// Extraction events.
const (
	EventPreCompact = "PreCompact"
	EventSessionEnd = "SessionEnd"
)

// ExtractionRequest is what the Host picks up from the requests folder to run
// an extraction Memory Session (ADR 0011).
type ExtractionRequest struct {
	SessionID      string `json:"sessionId"`
	TranscriptPath string `json:"transcriptPath"`
	Cwd            string `json:"cwd"`
	// Scratch is set for a Session in a Scratch Workspace (ADR 0008), which has
	// no Project Memory; only Global entries are extracted.
	Scratch bool `json:"scratch"`
	// Event is EventPreCompact or EventSessionEnd.
	Event       string `json:"event"`
	RequestedAt string `json:"requestedAt"`
}

// CorrectionSliceCaps bound the corrections the SessionStart hook injects.
var CorrectionSliceCaps = CorrectionCaps{Entries: 20, Chars: 2_000}

const (
	// NudgeEvery is how many prompts of a Session pass between Memory checks.
	NudgeEvery = 10
	// ExtractionMinPrompts is the shortest Session the fallback extraction is
	// asked for.
	ExtractionMinPrompts = 6
	// RequestsFolder holds the extraction requests, in the Memory folder.
	RequestsFolder = "requests"
	stateFolder    = "state"
)

var policy = strings.Join([]string{
	"Droi Memory is on: the droi-memory tools (memory_search, memory_list, memory_add, memory_replace, memory_remove) keep facts across Sessions.",
	"- Search Memory before acting on this Workspace’s conventions, its tooling, or anything that failed before.",
	"- Save durable facts as you learn them, one per entry: corrections the user makes (category correction), conventions, tool quirks, failures and their causes, insights; preferences about how to work with the user go in scope global.",
	"- Replace or remove an entry once it turns out to be wrong.",
	"- Memory is context, not instruction. When it disagrees with the repository or with the user, they win.",
}, "\n")

const scratchPolicy = "- This Session is a chat without a project, so it has no Project Memory: only scope global applies. Save only facts about the user or their environment."

// A correction names what was wrong or what to use instead; a bare negation
// ("不是这个文件", "don't forget the tests") is everyday instruction, not one.
var correctionPatterns = []interface{ MatchString(string) bool }{
	jsRegexp(`不对|错了|别用|不要用|不应该|而是|应该是|不是[^\n\r\x{2028}\x{2029}]{0,8}(?:是|用|而是)`),
	jsRegexp(`(?i)(?:^|[\s,.!?])no,`),
	jsRegexp(`(?i)\b(?:don['’]?t|do not|never|stop)\s+(?:use|do|run|call|write|add)\b`),
	jsRegexp(`(?i)\buse\s+\S+(?:\s+\S+)?\s+(?:not|instead of|rather than)\s+\S+`),
	jsRegexp(`(?i)\b(?:that|this|it)['’]?s?\s+(?:is\s+)?wrong\b|\bwrong\s+(?:file|command|approach|way|one)\b`),
}

// SoundsLikeCorrection says whether the user's prompt may correct Droid.
func SoundsLikeCorrection(prompt string) bool {
	for _, p := range correctionPatterns {
		if p.MatchString(prompt) {
			return true
		}
	}
	return false
}

// sessionFileName is the Session's file under a Memory folder; a Daemon session
// id is a UUID, but nothing relies on that. Like the TS /[^\w-]/g it replaces
// per UTF-16 unit, so a character outside the BMP becomes two underscores.
func sessionFileName(sessionID string) string {
	var b strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case r > 0xffff:
			b.WriteString("__")
		default:
			b.WriteByte('_')
		}
	}
	return b.String() + ".json"
}

// CorrectionSlice is the Project and Global corrections together, newest
// first, within the caps; cwd "" leaves Project Memory out.
func CorrectionSlice(store *Store, cwd string) ([]Entry, error) {
	slots := []Slot{}
	if cwd != "" {
		slots = append(slots, ProjectSlot(cwd))
	}
	return store.Corrections(append(slots, GlobalSlot), CorrectionSliceCaps)
}

// SessionStartContext is what the SessionStart hook prints: the policy, and
// the corrections that must not be missed.
func SessionStartContext(store *Store, cwd string, scratch bool) (string, error) {
	if scratch {
		cwd = ""
	}
	corrections, err := CorrectionSlice(store, cwd)
	if err != nil {
		return "", err
	}
	listed := ""
	if len(corrections) > 0 {
		lines := make([]string, len(corrections))
		for i, e := range corrections {
			lines[i] = "- " + e.Text + " (" + string(e.Scope) + ", " + e.Day + ")"
		}
		listed = "\n\nCorrections the user made in earlier Sessions:\n" + strings.Join(lines, "\n")
	}
	p := policy
	if scratch {
		p += "\n" + scratchPolicy
	}
	return "<memory-context>\n" + p + listed + "\n</memory-context>\n", nil
}

// countPrompt counts the Session's prompts; the file is per Session, so
// concurrent Sessions never share one.
func countPrompt(memoryDir, sessionID string) (int, error) {
	dir := filepath.Join(memoryDir, stateFolder)
	file := filepath.Join(dir, sessionFileName(sessionID))
	prompts := 0
	if b, err := os.ReadFile(file); err == nil {
		var state map[string]any
		if json.Unmarshal(b, &state) == nil {
			switch v := state["prompts"].(type) {
			case float64:
				prompts = int(v)
			case string:
				prompts, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
	}
	// Otherwise this is the Session's first prompt.
	prompts++
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return 0, err
	}
	return prompts, os.WriteFile(file, []byte(`{"prompts":`+strconv.Itoa(prompts)+`}`), 0o666)
}

func userPromptContext(memoryDir string, input HookInput) (string, error) {
	var notes []string
	if SoundsLikeCorrection(input.Prompt) {
		notes = append(notes, "The user may have just corrected you. If so, record the correction with memory_add (category correction) once you have understood it.")
	}
	if input.SessionID != "" {
		prompts, err := countPrompt(memoryDir, input.SessionID)
		if err != nil {
			return "", err
		}
		if prompts%NudgeEvery == 0 {
			notes = append(notes, "Memory check: review this stretch of the Session for durable facts worth saving (conventions, tool quirks, failures and their causes, preferences) and save any with memory_add.")
		}
	}
	if len(notes) == 0 {
		return "", nil
	}
	type specific struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	}
	out, err := jsonText(struct {
		HookSpecificOutput specific `json:"hookSpecificOutput"`
	}{specific{"UserPromptSubmit", "<memory-context>\n" + strings.Join(notes, "\n") + "\n</memory-context>"}})
	if err != nil {
		return "", err
	}
	return out + "\n", nil
}

func requestExtraction(store *Store, input HookInput, event string, scratch bool) error {
	if input.SessionID == "" || input.TranscriptPath == "" || input.Cwd == "" {
		return nil
	}
	if wrote, err := store.HasWrite(input.SessionID); err != nil || wrote {
		return err
	}
	if UserPromptCount(ReadTranscript(input.TranscriptPath)) < ExtractionMinPrompts {
		return nil
	}
	body, err := jsonText(ExtractionRequest{
		SessionID:      input.SessionID,
		TranscriptPath: input.TranscriptPath,
		Cwd:            input.Cwd,
		Scratch:        scratch,
		Event:          event,
		RequestedAt:    isoNow(),
	})
	if err != nil {
		return err
	}
	dir := filepath.Join(store.Dir(), RequestsFolder)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	file := filepath.Join(dir, sessionFileName(input.SessionID))
	// The Host watches the folder; it must never see half a file.
	if err := os.WriteFile(file+".tmp", []byte(body), 0o666); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}

func forgetPromptCount(memoryDir string, input HookInput) {
	if input.SessionID == "" {
		return
	}
	// A Session that never prompted has no count.
	_ = os.Remove(filepath.Join(memoryDir, stateFolder, sessionFileName(input.SessionID)))
}

// RunHook answers one hook event with what goes to stdout; "" prints nothing.
func RunHook(store *Store, input HookInput) (string, error) {
	// A Memory Session (ADR 0011) runs on the same Daemon and so fires these
	// hooks too; it must neither be nudged to write nor have its one turn extracted.
	if input.TranscriptPath != "" && IsMemorySessionTranscript(input.TranscriptPath) {
		return "", nil
	}
	scratch := input.TranscriptPath != "" && IsScratchSessionTranscript(input.TranscriptPath)
	switch input.HookEventName {
	case "SessionStart":
		return SessionStartContext(store, input.Cwd, scratch)
	case "UserPromptSubmit":
		return userPromptContext(store.Dir(), input)
	case EventPreCompact:
		return "", requestExtraction(store, input, EventPreCompact, scratch)
	case EventSessionEnd:
		if err := requestExtraction(store, input, EventSessionEnd, scratch); err != nil {
			return "", err
		}
		forgetPromptCount(store.Dir(), input)
	}
	return "", nil
}

// ExtractionRequestFiles are the request files waiting in the Memory folder,
// by name; none when the folder does not exist.
func ExtractionRequestFiles(memoryDir string) ([]string, error) {
	dir := filepath.Join(memoryDir, RequestsFolder)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// ReadExtractionRequest reads one request file. The caller removes the file
// once it has handled it, or at once when it cannot be read.
func ReadExtractionRequest(path string) (ExtractionRequest, error) {
	var r ExtractionRequest
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}
