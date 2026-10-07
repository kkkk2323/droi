package memorywork

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

// What the Host asks of Memory Sessions (ADR 0011): consolidating a Memory
// that grew large, one category slice at a time, and extracting entries from
// a finished Session that saved none. The model's answer is validated here; a
// wrong answer is a rejected slice or a skipped entry, never lost data.

const (
	// SliceChars is the most entry text the model sees at once; a larger
	// category is split.
	SliceChars = 40_000
	// TranscriptChars is the transcript an extraction sends, from the end: the
	// latest turns matter most.
	TranscriptChars = 60_000
	// SessionTimeout bounds one Memory Session.
	SessionTimeout = 10 * time.Minute
)

// Work is what a consolidation or an extraction runs with.
type Work struct {
	Store   *memory.Store
	Run     Run
	ModelID string
	Prompt  string
	// Home is where a Memory Session runs when its Memory has no Workspace on
	// disk; the user's home folder when "".
	Home string
}

// SliceOutcome is how one category slice of a consolidation went.
type SliceOutcome struct {
	Category memory.Category
	Entries  int
	OK       bool
	// Reason says why the slice was left unchanged; "" when OK.
	Reason string
}

var stringArray = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}

// ConsolidationSchema is the reply a consolidation asks for.
var ConsolidationSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"keep": stringArray,
		"rewrite": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"id": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}},
				"required":             []string{"id", "text"},
				"additionalProperties": false,
			},
		},
		"remove": stringArray,
		"merge": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"ids": stringArray, "text": map[string]any{"type": "string"}},
				"required":             []string{"ids", "text"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"keep", "rewrite", "remove", "merge"},
	"additionalProperties": false,
}

// ExtractionSchema is the reply an extraction asks for.
var ExtractionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entries": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope":    map[string]any{"type": "string", "enum": []string{"project", "global"}},
					"category": map[string]any{"type": "string", "enum": categoryNames()},
					"text":     map[string]any{"type": "string"},
				},
				"required":             []string{"scope", "category", "text"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"entries"},
	"additionalProperties": false,
}

func categoryNames() []string {
	names := make([]string, len(memory.Categories))
	for i, c := range memory.Categories {
		names[i] = string(c)
	}
	return names
}

func decode(reply json.RawMessage) any {
	var v any
	if json.Unmarshal(reply, &v) != nil {
		return nil
	}
	return v
}

func stringList(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// ParseSliceChanges reads a consolidation's reply; false when it does not
// match ConsolidationSchema.
func ParseSliceChanges(reply json.RawMessage) (memory.SliceChanges, bool) {
	v, ok := decode(reply).(map[string]any)
	if !ok {
		return memory.SliceChanges{}, false
	}
	keep, okKeep := stringList(v["keep"])
	remove, okRemove := stringList(v["remove"])
	rewrite, okRewrite := v["rewrite"].([]any)
	merge, okMerge := v["merge"].([]any)
	if !okKeep || !okRemove || !okRewrite || !okMerge {
		return memory.SliceChanges{}, false
	}
	changes := memory.SliceChanges{Keep: keep, Remove: remove, Rewrite: []memory.Rewrite{}, Merge: []memory.Merge{}}
	for _, r := range rewrite {
		m, _ := r.(map[string]any)
		id, okID := m["id"].(string)
		text, okText := m["text"].(string)
		if !okID || !okText {
			return memory.SliceChanges{}, false
		}
		changes.Rewrite = append(changes.Rewrite, memory.Rewrite{ID: id, Text: text})
	}
	for _, r := range merge {
		m, _ := r.(map[string]any)
		ids, okIDs := stringList(m["ids"])
		text, okText := m["text"].(string)
		if !okIDs || !okText {
			return memory.SliceChanges{}, false
		}
		changes.Merge = append(changes.Merge, memory.Merge{IDs: ids, Text: text})
	}
	return changes, true
}

// sessionWorkspace is where a Memory Session for this Memory runs: its
// Workspace while it exists.
func (w Work) sessionWorkspace(slot memory.Slot) string {
	if slot.Scope == memory.ScopeProject {
		if _, err := os.Stat(slot.Workspace); err == nil {
			return slot.Workspace
		}
	}
	if w.Home != "" {
		return w.Home
	}
	home, _ := os.UserHomeDir()
	return home
}

func memoryName(slot memory.Slot) string {
	if slot.Scope == memory.ScopeGlobal {
		return "Global Memory"
	}
	return filepath.Base(slot.Workspace)
}

func (w Work) run(ctx context.Context, r Request) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, SessionTimeout)
	defer cancel()
	return w.Run(ctx, r)
}

// jsLength is a string's length in UTF-16 code units, as JavaScript counts it.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// jsonText is JSON.stringify, which leaves <, > and & alone.
func jsonText(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

// SlicesOf splits a category's entries into slices of at most SliceChars,
// keeping an entry larger than that alone in its own slice.
func SlicesOf(entries []memory.Entry) [][]memory.Entry {
	var slices [][]memory.Entry
	var current []memory.Entry
	chars := 0
	for _, e := range entries {
		n := jsLength(e.Text)
		if len(current) > 0 && chars+n > SliceChars {
			slices = append(slices, current)
			current = nil
			chars = 0
		}
		current = append(current, e)
		chars += n
	}
	if len(current) > 0 {
		slices = append(slices, current)
	}
	return slices
}

type sentEntry struct {
	ID   string `json:"id"`
	Day  string `json:"day"`
	Text string `json:"text"`
}

type consolidationInput struct {
	Memory   string      `json:"memory"`
	Category string      `json:"category"`
	Entries  []sentEntry `json:"entries"`
}

// Consolidate consolidates every category of one Memory; each slice stands or
// falls alone.
func Consolidate(ctx context.Context, w Work, slot memory.Slot) ([]SliceOutcome, error) {
	outcomes := []SliceOutcome{}
	memoryKey := "global"
	if slot.Scope != memory.ScopeGlobal {
		memoryKey = slot.Workspace
	}
	for _, category := range memory.Categories {
		entries, err := w.Store.List(slot, category)
		if err != nil {
			return outcomes, err
		}
		for _, slice := range SlicesOf(entries) {
			if len(slice) < 2 {
				continue
			}
			outcome := SliceOutcome{Category: category, Entries: len(slice)}
			sent := make([]sentEntry, len(slice))
			for i, e := range slice {
				sent[i] = sentEntry{ID: e.ID, Day: e.Day, Text: e.Text}
			}
			reply, err := w.run(ctx, Request{
				Title:   "Memory: consolidate " + memoryName(slot) + " / " + string(category),
				Cwd:     w.sessionWorkspace(slot),
				ModelID: w.ModelID,
				Prompt:  w.Prompt,
				Input:   jsonText(consolidationInput{Memory: memoryKey, Category: string(category), Entries: sent}),
				Schema:  ConsolidationSchema,
			})
			switch changes, ok := ParseSliceChanges(reply); {
			case err != nil:
				outcome.Reason = err.Error()
			case !ok:
				outcome.Reason = "The reply did not match the schema."
			default:
				reason, err := w.Store.ApplySlice(slice, changes)
				if err != nil {
					reason = err.Error()
				}
				outcome.OK = reason == ""
				outcome.Reason = reason
			}
			outcomes = append(outcomes, outcome)
		}
	}
	// A run whose every answer was rejected changed nothing, so it does not count.
	counts := len(outcomes) == 0
	for _, o := range outcomes {
		counts = counts || o.OK
	}
	if counts {
		if err := w.Store.MarkConsolidated(slot); err != nil {
			return outcomes, err
		}
	}
	_, err := memory.ExportMarkdown(w.Store, slot)
	return outcomes, err
}

// TranscriptText is a transcript as an extraction sends it: its last
// TranscriptChars.
func TranscriptText(turns []memory.Turn) string {
	parts := make([]string, len(turns))
	for i, t := range turns {
		role := "Assistant"
		if t.Role == "user" {
			role = "User"
		}
		parts[i] = role + ": " + t.Text
	}
	text := strings.Join(parts, "\n\n")
	if jsLength(text) <= TranscriptChars {
		return text
	}
	units := utf16.Encode([]rune(text))
	return string(utf16.Decode(units[len(units)-TranscriptChars:]))
}

type existingEntry struct {
	Category memory.Category `json:"category"`
	Text     string          `json:"text"`
}

type extractionInput struct {
	Workspace  *string         `json:"workspace"`
	Existing   []existingEntry `json:"existing"`
	Transcript string          `json:"transcript"`
}

// Extract extracts entries from a finished Session and adds the ones the
// store accepts.
func Extract(ctx context.Context, w Work, request memory.ExtractionRequest) (added, refused int, err error) {
	turns := memory.ReadTranscript(request.TranscriptPath)
	if len(turns) == 0 {
		return 0, 0, nil
	}
	// A Scratch Session's folder is made for it and trashed with it, so it has
	// no Project Memory (ADR 0011): only Global entries come out of it.
	project := memory.GlobalSlot
	var workspace *string
	if !request.Scratch {
		project = memory.ProjectSlot(request.Cwd)
		workspace = &request.Cwd
	}
	known, err := w.Store.List(project, "")
	if err != nil {
		return 0, 0, err
	}
	existing := make([]existingEntry, len(known))
	for i, e := range known {
		existing[i] = existingEntry{Category: e.Category, Text: e.Text}
	}
	id := []rune(request.SessionID)
	if len(id) > 8 {
		id = id[:8]
	}
	reply, err := w.run(ctx, Request{
		Title:   "Memory: extract from " + string(id),
		Cwd:     w.sessionWorkspace(project),
		ModelID: w.ModelID,
		Prompt:  w.Prompt,
		Input:   jsonText(extractionInput{Workspace: workspace, Existing: existing, Transcript: TranscriptText(turns)}),
		Schema:  ExtractionSchema,
	})
	if err != nil {
		return 0, 0, err
	}
	// PreCompact and SessionEnd may both ask about the same Session; the hook
	// skips one that wrote.
	if err := w.Store.RecordWrite(request.SessionID); err != nil {
		return 0, 0, err
	}
	obj, _ := decode(reply).(map[string]any)
	entries, _ := obj["entries"].([]any)
	touchedProject, touchedGlobal := false, false
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		global := entry["scope"] == "global"
		text, isText := entry["text"].(string)
		if !memory.IsCategory(entry["category"]) || !isText || (!global && project.Scope == memory.ScopeGlobal) {
			refused++
			continue
		}
		slot := project
		if global {
			slot = memory.GlobalSlot
		}
		result, err := w.Store.Add(slot, memory.Category(entry["category"].(string)), text)
		if err != nil {
			return added, refused, err
		}
		if !result.OK {
			refused++
			continue
		}
		added++
		if global {
			touchedGlobal = true
		} else {
			touchedProject = true
		}
	}
	if touchedProject {
		if _, err := memory.ExportMarkdown(w.Store, project); err != nil {
			return added, refused, err
		}
	}
	if touchedGlobal {
		if _, err := memory.ExportMarkdown(w.Store, memory.GlobalSlot); err != nil {
			return added, refused, err
		}
	}
	return added, refused, nil
}
