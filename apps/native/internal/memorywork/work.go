package memorywork

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

// What the Host asks of Memory Sessions (ADR 0011): consolidating a Memory
// that grew large, a Project Memory one category slice at a time and the
// Global Memory its core and then each topic (ADR 0030); sorting the Global
// Memory's core into topics once; and extracting entries from a finished
// Session that saved none. The model's answer is validated here; a wrong
// answer is a rejected slice or a skipped entry, never lost data.

const (
	// SliceChars is the most entry text the model sees at once; a larger
	// category is split.
	SliceChars = 40_000
	// GlobalSliceChars and SortSliceChars are smaller: an answer about
	// every category at once, or a topic for every entry, is harder, and a
	// cheap model given 40k characters of them thought for minutes and
	// answered nothing.
	GlobalSliceChars = 15_000
	SortSliceChars   = 12_000
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
	// Sessions is the Daemon's sessions folder, where a Global Memory
	// consolidation finds the Workspace that wrote each entry; "" skips that.
	Sessions string
}

// CoreGroup is the Group of a slice of the Global Memory's core.
const CoreGroup = "core"

// SliceOutcome is how one slice of a consolidation went.
type SliceOutcome struct {
	// Group is what the slice held: a Project Memory's category, or the
	// Global Memory's CoreGroup or one of its topics.
	Group   string
	Entries int
	OK      bool
	// Reason says why the slice was left unchanged; "" when OK.
	Reason string
}

var stringArray = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}

func pairArray(key string) map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"id": map[string]any{"type": "string"}, key: map[string]any{"type": "string"}},
			"required":             []string{"id", key},
			"additionalProperties": false,
		},
	}
}

// ConsolidationSchema is the reply a consolidation asks for.
var ConsolidationSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"keep":    stringArray,
		"rewrite": pairArray("text"),
		"remove":  stringArray,
		"merge": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ids":  stringArray,
					"text": map[string]any{"type": "string"},
					// Gemini refuses an empty string in an enum, so the
					// category is checked when the answer is applied.
					"category": map[string]any{"type": "string", "description": `One of the categories, or "" to keep the first entry's.`},
				},
				"required":             []string{"ids", "text", "category"},
				"additionalProperties": false,
			},
		},
		"move":  pairArray("workspace"),
		"topic": pairArray("topic"),
	},
	"required":             []string{"keep", "rewrite", "remove", "merge", "move", "topic"},
	"additionalProperties": false,
}

// SortingSchema is the reply the sort of the Global Memory's core into
// topics asks for.
var SortingSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"topics": pairArray("topic")},
	"required":             []string{"topics"},
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
					// "" for a global fact about the user, and for every project one.
					"topic": map[string]any{"type": "string"},
				},
				"required":             []string{"scope", "category", "text", "topic"},
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

// pairs reads an array of {id, key} objects; a reply read from the final
// text, without the schema, may leave an optional one out.
func pairs(v any, key string, optional bool) ([][2]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, optional && v == nil
	}
	out := [][2]string{}
	for _, r := range arr {
		m, _ := r.(map[string]any)
		id, okID := m["id"].(string)
		value, okValue := m[key].(string)
		if !okID || !okValue {
			return nil, false
		}
		out = append(out, [2]string{id, value})
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
	rewrite, okRewrite := pairs(v["rewrite"], "text", false)
	merge, okMerge := v["merge"].([]any)
	move, okMove := pairs(v["move"], "workspace", true)
	topic, okTopic := pairs(v["topic"], "topic", true)
	if !okKeep || !okRemove || !okRewrite || !okMerge || !okMove || !okTopic {
		return memory.SliceChanges{}, false
	}
	changes := memory.SliceChanges{Keep: keep, Remove: remove, Rewrite: []memory.Rewrite{}, Merge: []memory.Merge{}, Move: []memory.Move{}, Topic: []memory.TopicChange{}}
	for _, r := range rewrite {
		changes.Rewrite = append(changes.Rewrite, memory.Rewrite{ID: r[0], Text: r[1]})
	}
	for _, r := range merge {
		m, _ := r.(map[string]any)
		ids, okIDs := stringList(m["ids"])
		text, okText := m["text"].(string)
		category, okCategory := m["category"].(string)
		if !okIDs || !okText || (!okCategory && m["category"] != nil) {
			return memory.SliceChanges{}, false
		}
		changes.Merge = append(changes.Merge, memory.Merge{IDs: ids, Text: text, Category: memory.Category(category)})
	}
	for _, m := range move {
		changes.Move = append(changes.Move, memory.Move{ID: m[0], Workspace: m[1]})
	}
	for _, t := range topic {
		changes.Topic = append(changes.Topic, memory.TopicChange{ID: t[0], Topic: t[1]})
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
	return slicesOf(entries, SliceChars)
}

func slicesOf(entries []memory.Entry, limit int) [][]memory.Entry {
	var slices [][]memory.Entry
	var current []memory.Entry
	chars := 0
	for _, e := range entries {
		n := jsLength(e.Text)
		if len(current) > 0 && chars+n > limit {
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
	ID  string `json:"id"`
	Day string `json:"day"`
	// Category is sent for a Global Memory slice, which holds every category.
	Category string `json:"category,omitempty"`
	Text     string `json:"text"`
	// Found is how many searches returned a Global Memory entry.
	Found *int `json:"found,omitempty"`
	// WrittenIn is the Workspace of the Session that saved a Global Memory
	// entry, or writtenInChat; "" when unknown.
	WrittenIn string `json:"written_in,omitempty"`
}

// writtenInChat is where a Global Memory entry saved in a chat without a
// project was written.
const writtenInChat = "chat"

type consolidationInput struct {
	Memory string `json:"memory"`
	// Category is a Project Memory slice's.
	Category string `json:"category,omitempty"`
	// Group is CoreGroup or "topic" for a Global Memory slice, and Topic the
	// topic's name.
	Group   string      `json:"group,omitempty"`
	Topic   string      `json:"topic,omitempty"`
	Entries []sentEntry `json:"entries"`
	// Projects are the Workspaces a Global Memory entry may move to.
	Projects []string `json:"projects,omitempty"`
	// Topics are the Global Memory's topics, which its entries may be filed
	// under.
	Topics []string `json:"topics,omitempty"`
}

// origins says where the Session that saved each Global Memory entry ran,
// and offers every project among them, and every Project Memory, as a place
// to move entries to.
func (w Work) origins() (writtenIn map[string]string, projects []string, err error) {
	writtenIn = map[string]string{}
	offered := map[string]bool{}
	summaries, err := w.Store.Summaries()
	if err != nil {
		return nil, nil, err
	}
	for _, s := range summaries {
		if s.Scope == memory.ScopeProject {
			offered[s.Workspace] = true
		}
	}
	entries, err := w.Store.List(memory.GlobalSlot, "")
	if err != nil {
		return nil, nil, err
	}
	sessions := map[string]string{}
	for _, e := range entries {
		session, err := w.Store.WriterSession(e.ID)
		if err != nil {
			return nil, nil, err
		}
		if session == "" || w.Sessions == "" {
			continue
		}
		where, seen := sessions[session]
		if !seen {
			if file := memory.FindSessionTranscript(w.Sessions, session); file != "" {
				if memory.IsScratchSessionTranscript(file) {
					where = writtenInChat
				} else if cwd := memory.ReadSessionWorkspace(file); cwd != "" {
					where = memory.ProjectSlot(cwd).Workspace
					offered[where] = true
				}
			}
			sessions[session] = where
		}
		if where != "" {
			writtenIn[e.ID] = where
		}
	}
	for workspace := range offered {
		projects = append(projects, workspace)
	}
	sort.Strings(projects)
	return writtenIn, projects, nil
}

// globalContext is what every slice of a Global Memory consolidation or sort
// sends beside its entries.
type globalContext struct {
	writtenIn map[string]string
	found     map[string]int
	projects  []string
	offered   map[string]bool
}

func (w Work) globalContext() (globalContext, error) {
	writtenIn, projects, err := w.origins()
	if err != nil {
		return globalContext{}, err
	}
	found, err := w.Store.FoundCounts(memory.GlobalSlot)
	if err != nil {
		return globalContext{}, err
	}
	offered := map[string]bool{}
	for _, p := range projects {
		offered[p] = true
	}
	return globalContext{writtenIn: writtenIn, found: found, projects: projects, offered: offered}, nil
}

func (g globalContext) sent(slice []memory.Entry) []sentEntry {
	sent := make([]sentEntry, len(slice))
	for i, e := range slice {
		n := g.found[e.ID]
		sent[i] = sentEntry{ID: e.ID, Day: e.Day, Category: string(e.Category), Text: e.Text, Found: &n, WrittenIn: g.writtenIn[e.ID]}
	}
	return sent
}

func topicNames(store *memory.Store) ([]string, error) {
	layers, err := store.GlobalLayers()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(layers.Topics))
	for i, t := range layers.Topics {
		names[i] = t.Topic
	}
	sort.Strings(names)
	return names, nil
}

// consolidateSlice asks for one slice's changes and applies them; offered are
// the Workspaces its entries may move to.
func (w Work) consolidateSlice(ctx context.Context, slot memory.Slot, group string, slice []memory.Entry, input consolidationInput, offered map[string]bool) (SliceOutcome, memory.SliceChanges) {
	outcome := SliceOutcome{Group: group, Entries: len(slice)}
	title := "Memory: consolidate " + memoryName(slot) + " / " + group
	if input.Group == "topic" {
		title = "Memory: consolidate " + memoryName(slot) + " / topic: " + group
	}
	reply, err := w.run(ctx, Request{
		Title:   title,
		Cwd:     w.sessionWorkspace(slot),
		ModelID: w.ModelID,
		Prompt:  w.Prompt,
		Input:   jsonText(input),
		Schema:  ConsolidationSchema,
	})
	changes, ok := ParseSliceChanges(reply)
	switch {
	case err != nil:
		outcome.Reason = err.Error()
	case !ok:
		outcome.Reason = "The reply did not match the schema."
	default:
		reason := ""
		for _, m := range changes.Move {
			if !offered[m.Workspace] {
				reason = "The answer moves " + m.ID + " to " + m.Workspace + ", which was not offered."
				break
			}
		}
		if reason == "" {
			changes = keepTheUsersOwn(slice, changes)
			if reason, err = w.Store.ApplySlice(slice, changes); err != nil {
				reason = err.Error()
			}
		}
		outcome.OK = reason == ""
		outcome.Reason = reason
	}
	return outcome, changes
}

// keepTheUsersOwn turns into a keep a move of a correction or a preference,
// which are about the user, and of an entry that does not name the project
// it would move to. A cheap model moved entries to whichever project they
// were saved in: a rule against kubectl on production, a git quirk.
func keepTheUsersOwn(slice []memory.Entry, changes memory.SliceChanges) memory.SliceChanges {
	byID := map[string]memory.Entry{}
	for _, e := range slice {
		byID[e.ID] = e
	}
	moves := []memory.Move{}
	for _, m := range changes.Move {
		e := byID[m.ID]
		if e.Category == memory.CategoryCorrection || e.Category == memory.CategoryPreference || !names(e.Text, filepath.Base(m.Workspace)) {
			changes.Keep = append(changes.Keep, m.ID)
			continue
		}
		moves = append(moves, m)
	}
	changes.Move = moves
	return changes
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// names says whether text names a folder: its words in a row, so that
// "anlan CLI" names anlan-cli but "Droid" does not name droi.
func names(text, folder string) bool {
	want, have := words(folder), words(text)
	if len(want) == 0 {
		return false
	}
	for i := 0; i+len(want) <= len(have); i++ {
		if slices.Equal(have[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// Consolidate consolidates one Memory; each slice stands or falls alone.
func Consolidate(ctx context.Context, w Work, slot memory.Slot) ([]SliceOutcome, error) {
	if slot.Scope == memory.ScopeGlobal {
		return consolidateGlobal(ctx, w)
	}
	outcomes := []SliceOutcome{}
	for _, category := range memory.Categories {
		entries, err := w.Store.List(slot, category)
		if err != nil {
			return outcomes, err
		}
		for _, slice := range SlicesOf(entries) {
			// A lone entry has nothing to merge with.
			if len(slice) < 2 {
				continue
			}
			sent := make([]sentEntry, len(slice))
			for i, e := range slice {
				sent[i] = sentEntry{ID: e.ID, Day: e.Day, Text: e.Text}
			}
			outcome, _ := w.consolidateSlice(ctx, slot, string(category), slice, consolidationInput{Memory: slot.Workspace, Category: string(category), Entries: sent}, nil)
			outcomes = append(outcomes, outcome)
		}
	}
	return outcomes, w.finishConsolidation(slot, outcomes, nil)
}

// finishConsolidation marks a Memory consolidated unless every answer was
// rejected, and exports it and the projects entries moved to.
func (w Work) finishConsolidation(slot memory.Slot, outcomes []SliceOutcome, movedTo map[string]bool) error {
	// A run whose every answer was rejected changed nothing, so it does not count.
	counts := len(outcomes) == 0
	for _, o := range outcomes {
		counts = counts || o.OK
	}
	if counts {
		if err := w.Store.MarkConsolidated(slot); err != nil {
			return err
		}
	}
	if _, err := memory.ExportMarkdown(w.Store, slot); err != nil {
		return err
	}
	moved := make([]string, 0, len(movedTo))
	for p := range movedTo {
		moved = append(moved, p)
	}
	sort.Strings(moved)
	for _, p := range moved {
		if _, err := memory.ExportMarkdown(w.Store, memory.ProjectSlot(p)); err != nil {
			return err
		}
	}
	return nil
}

// consolidateGlobal consolidates the Global Memory's core, which may file
// entries under topics or move them to a project, then each topic of two or
// more entries, so that what the core filed is consolidated with its topic.
func consolidateGlobal(ctx context.Context, w Work) ([]SliceOutcome, error) {
	outcomes := []SliceOutcome{}
	slot := memory.GlobalSlot
	if _, err := w.Store.Backup(); err != nil {
		return outcomes, err
	}
	g, err := w.globalContext()
	if err != nil {
		return outcomes, err
	}
	movedTo := map[string]bool{}
	applied := func(o SliceOutcome, c memory.SliceChanges) {
		outcomes = append(outcomes, o)
		if o.OK {
			for _, m := range c.Move {
				movedTo[m.Workspace] = true
			}
		}
	}
	core, err := w.Store.ListTopic(slot, "", "")
	if err != nil {
		return outcomes, err
	}
	var inCore []memory.Entry
	for _, e := range core {
		if e.Topic == "" {
			inCore = append(inCore, e)
		}
	}
	coreDone := len(inCore) == 0
	for _, slice := range slicesOf(inCore, GlobalSliceChars) {
		topics, err := topicNames(w.Store)
		if err != nil {
			return outcomes, err
		}
		o, c := w.consolidateSlice(ctx, slot, CoreGroup, slice, consolidationInput{Memory: "global", Group: CoreGroup, Entries: g.sent(slice), Projects: g.projects, Topics: topics}, g.offered)
		applied(o, c)
		coreDone = coreDone || o.OK
	}
	if coreDone {
		if err := w.Store.MarkSorted(0); err != nil {
			return outcomes, err
		}
	}
	entries, err := w.Store.List(slot, "")
	if err != nil {
		return outcomes, err
	}
	byTopic := map[string][]memory.Entry{}
	for _, e := range entries {
		if e.Topic != "" {
			byTopic[e.Topic] = append(byTopic[e.Topic], e)
		}
	}
	names := make([]string, 0, len(byTopic))
	for name := range byTopic {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, slice := range slicesOf(byTopic[name], GlobalSliceChars) {
			// A lone entry in a topic has nothing to merge with.
			if len(slice) < 2 {
				continue
			}
			topics, err := topicNames(w.Store)
			if err != nil {
				return outcomes, err
			}
			o, c := w.consolidateSlice(ctx, slot, name, slice, consolidationInput{Memory: "global", Group: "topic", Topic: name, Entries: g.sent(slice), Projects: g.projects, Topics: topics}, g.offered)
			applied(o, c)
		}
	}
	return outcomes, w.finishConsolidation(slot, outcomes, movedTo)
}

type sortingInput struct {
	Memory  string      `json:"memory"`
	Entries []sentEntry `json:"entries"`
	Topics  []string    `json:"topics,omitempty"`
}

// ErrSortFailed is SortTopics' answer when no slice of the core was sorted.
var ErrSortFailed = errors.New("no part of the Global Memory's core was sorted into topics")

// SortTopics sorts the Global Memory's core into topics the first time it
// runs, giving each entry a topic or leaving it in the core and changing no
// text, and answers how many entries it filed. Once sorted, or with nothing
// to sort, it does nothing; when every answer is rejected it marks nothing,
// so the next run tries again.
func SortTopics(ctx context.Context, w Work) (int, error) {
	layers, err := w.Store.GlobalLayers()
	if err != nil || layers.SortedAt != "" {
		return 0, err
	}
	core, err := w.Store.List(memory.GlobalSlot, "")
	if err != nil {
		return 0, err
	}
	var inCore []memory.Entry
	for _, e := range core {
		if e.Topic == "" {
			inCore = append(inCore, e)
		}
	}
	if len(inCore) == 0 {
		return 0, w.Store.MarkSorted(0)
	}
	if _, err := w.Store.Backup(); err != nil {
		return 0, err
	}
	g, err := w.globalContext()
	if err != nil {
		return 0, err
	}
	filed, sorted := 0, false
	var reasons []string
	for _, slice := range slicesOf(inCore, SortSliceChars) {
		topics, err := topicNames(w.Store)
		if err != nil {
			return filed, err
		}
		reply, err := w.run(ctx, Request{
			Title:   "Memory: sort Global Memory into topics",
			Cwd:     w.sessionWorkspace(memory.GlobalSlot),
			ModelID: w.ModelID,
			Prompt:  w.Prompt,
			Input:   jsonText(sortingInput{Memory: "global", Entries: g.sent(slice), Topics: topics}),
			Schema:  SortingSchema,
		})
		if err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		v, _ := decode(reply).(map[string]any)
		answer, ok := pairs(v["topics"], "topic", false)
		if !ok {
			reasons = append(reasons, "The reply did not match the schema.")
			continue
		}
		// A sort only files entries, so an id the model mistyped or left out
		// stays in the core instead of costing the whole slice.
		topicOf := map[string]string{}
		for _, a := range answer {
			if _, seen := topicOf[a[0]]; !seen {
				topicOf[a[0]] = a[1]
			}
		}
		changes := memory.SliceChanges{Keep: []string{}, Topic: []memory.TopicChange{}}
		answered := 0
		for _, e := range slice {
			topic, ok := topicOf[e.ID]
			if ok {
				answered++
			}
			if strings.Trim(strings.TrimSpace(topic), `"'“”`) == "" {
				changes.Keep = append(changes.Keep, e.ID)
			} else {
				changes.Topic = append(changes.Topic, memory.TopicChange{ID: e.ID, Topic: topic})
			}
		}
		if answered == 0 {
			reasons = append(reasons, "The answer named none of the entries it was sent.")
			continue
		}
		reason, err := w.Store.ApplySlice(slice, changes)
		if err != nil {
			return filed, err
		}
		if reason != "" {
			reasons = append(reasons, reason)
			continue
		}
		sorted = true
		filed += len(changes.Topic)
	}
	if !sorted {
		return 0, fmt.Errorf("%w: %s", ErrSortFailed, strings.Join(reasons, "; "))
	}
	if err := w.Store.MarkSorted(filed); err != nil {
		return filed, err
	}
	_, err = memory.ExportMarkdown(w.Store, memory.GlobalSlot)
	return filed, err
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
	Topic    string          `json:"topic,omitempty"`
}

type extractionInput struct {
	Workspace *string         `json:"workspace"`
	Existing  []existingEntry `json:"existing"`
	// Topics are the Global Memory's topics, for a global entry to reuse.
	Topics     []string `json:"topics,omitempty"`
	Transcript string   `json:"transcript"`
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
		existing[i] = existingEntry{Category: e.Category, Text: e.Text, Topic: e.Topic}
	}
	topics, err := topicNames(w.Store)
	if err != nil {
		return 0, 0, err
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
		Input:   jsonText(extractionInput{Workspace: workspace, Existing: existing, Topics: topics, Transcript: TranscriptText(turns)}),
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
		slot, topic := project, ""
		if global {
			slot = memory.GlobalSlot
			topic, _ = entry["topic"].(string)
		}
		result, err := w.Store.AddWithTopic(slot, memory.Category(entry["category"].(string)), text, topic)
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
