// Memory's hook (ADR 0031). At SessionStart it records the Session, so the
// Host summarizes it once it has been idle, and puts the Memory Summary in
// front of the model with a short policy on how to use it.

package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// HookInput is what the Daemon passes a hook on stdin; a field that is absent,
// empty or not a string is "".
type HookInput struct {
	HookEventName  string
	SessionID      string
	TranscriptPath string
	Cwd            string
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
		Source:         str("source"),
	}, nil
}

// PendingNoteChars bounds the Memory Notes the Memory Summary has not taken
// in yet, which SessionStart shows beside it.
const PendingNoteChars = 3_000

// The read policy follows Codex's read path (codex-rs/ext/memories,
// read_path_v2.md, Apache-2.0), with Droi's tools.
var policy = strings.Join([]string{
	"## Memory",
	"",
	"Droi Memory is on. The Memory Summary below was written from earlier Sessions on this computer. Use it as historical context: apply the user's actual preferences, corrections, decisions and supported task scope.",
	"- Its pointers name Session Summaries (" + SessionSummariesDir + "/...) and Memory Notes (" + NotesDir + "/...). Read a Memory Note it points to before work in that project. Read a Session Summary with memory_read when its evidence, wording, chronology or uncertainty could change your answer; otherwise do not look up history speculatively. Use memory_search when a route you need is missing.",
	"- Memory is not proof of current behavior. For a claim that may have changed or that matters, check the actual source, and say so when you are unsure. When Memory disagrees with the repository or with the user, they win.",
	"- Droi keeps Memory from finished Sessions on its own; do not try to save what you learn. Only when the user explicitly asks you to remember, forget or correct something, call memory_note with what they asked.",
}, "\n")

const noSummary = "There is no Memory Summary yet: Droi writes it from Sessions once they have been idle for a while."

// pendingNotes are the Memory Notes the Memory Summary has not taken in,
// newest first, within PendingNoteChars.
func pendingNotes(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(dir, NotesDir))
	if err != nil {
		return nil
	}
	taken := LoadState(dir).Phase2.Inputs
	var out []string
	chars := 0
	for _, e := range slices.Backward(entries) {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, NotesDir, e.Name()))
		if err != nil {
			continue
		}
		if taken[NotesDir+"/"+e.Name()] == Hash(b) {
			continue
		}
		text := string(b)
		if _, body, ok := strings.Cut(text, "\n\n"); ok && len(Header(text)) > 0 {
			text = body
		}
		text = "- " + oneLine(text)
		if n := utf8.RuneCountInString(text); chars+n > PendingNoteChars {
			break
		} else {
			chars += n
		}
		out = append(out, text)
	}
	return out
}

// SessionStartContext is what the SessionStart hook prints.
func SessionStartContext(dir string) string {
	var b strings.Builder
	b.WriteString("<memory-context>\n" + policy + "\n\n")
	if summary := strings.TrimSpace(ReadSummary(dir)); summary != "" {
		b.WriteString("========= MEMORY_SUMMARY BEGINS =========\n" + summary + "\n========= MEMORY_SUMMARY ENDS =========\n")
	} else {
		b.WriteString(noSummary + "\n")
	}
	if notes := pendingNotes(dir); len(notes) > 0 {
		b.WriteString("\nMemory Notes the user asked for that the summary does not include yet, newest first; they win over it:\n" + strings.Join(notes, "\n") + "\n")
	}
	b.WriteString("</memory-context>\n")
	return b.String()
}

// RunHook answers one hook event with what goes to stdout; "" prints nothing.
func RunHook(dir string, input HookInput) (string, error) {
	if input.HookEventName != "SessionStart" {
		return "", nil
	}
	// A Memory Session runs on the same Daemon and so fires the hook too; it
	// works from the material it is sent and is never summarized.
	if input.TranscriptPath != "" && IsMemorySessionTranscript(input.TranscriptPath) {
		return "", nil
	}
	var err error
	if input.SessionID != "" && input.TranscriptPath != "" {
		err = RecordStart(dir, Started{SessionID: input.SessionID, Transcript: input.TranscriptPath, Cwd: input.Cwd, At: time.Now().UTC().Format(time.RFC3339)})
	}
	return SessionStartContext(dir), err
}
