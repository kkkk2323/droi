// Package sessions is the Session list as the Client shows it: summaries
// from daemon.list_available_sessions, grouped by Workspace, sorted, pinned,
// folded into compaction chains, and the subagents kept out (a port of
// packages/daemon-layer/src/sessions.ts and its neighbours).
package sessions

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
)

// Tag is a Session tag.
type Tag struct {
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Summary is one listed Session.
type Summary struct {
	SessionID string
	Title     string
	Cwd       string
	// RepoRoot is the grouping key the Daemon gives checkouts; Cwd else.
	RepoRoot string
	// UpdatedAt is in Unix seconds.
	UpdatedAt     int64
	MessagesCount *int
	ArchivedAt    string
	Tags          []Tag
	// ParentID is the Session this one continues after a compaction.
	ParentID string
	// CallingSessionID and CallingToolUseID: for a subagent, its caller.
	CallingSessionID string
	CallingToolUseID string
	// Worktree is the Daemon-managed worktree the Session runs in, nil for
	// none or once it was removed.
	Worktree *Worktree
}

// Worktree is a Session's worktree: a checkout of RepoRoot at Path, on Branch.
type Worktree struct {
	Path, Branch, RepoRoot string
	// Lifecycle is "ephemeral" (the Daemon cleans it up, archiving the
	// Session deletes it), "persistent" (deleted by hand only) or "".
	Lifecycle string
}

// Ephemeral reports whether archiving the Session deletes its worktree. A
// worktree listed without a lifecycle is one the Daemon adopted, which it
// never cleans up.
func (w *Worktree) Ephemeral() bool { return w != nil && w.Lifecycle == "ephemeral" }

// WorkspaceOf is the Workspace a Session belongs to: a worktree's main
// checkout, else the repository root the Daemon gives, else its folder.
func WorkspaceOf(s Summary) string {
	if s.Worktree != nil && s.Worktree.RepoRoot != "" {
		return s.Worktree.RepoRoot
	}
	return cmp.Or(s.RepoRoot, s.Cwd)
}

// The tags Droi writes (see CONTEXT.md).
const (
	ContinuesTag = "droi.continues"
	DraftTag     = "droi.draft"
	ScratchTag   = "droi.scratch"
	MemoryTag    = "droi.memory"
)

func hasTag(tags []Tag, name string) bool {
	return slices.ContainsFunc(tags, func(t Tag) bool { return t.Name == name })
}

// ContinuationParent is the parent a child Session's tag names.
func ContinuationParent(tags []Tag) string {
	for _, t := range tags {
		if t.Name == ContinuesTag {
			return t.Metadata["parent"]
		}
	}
	return ""
}

// ContinuationTags are a child's tags: the parent's, minus an older link,
// plus the new one.
func ContinuationTags(parentID string, inherited []Tag) []Tag {
	var out []Tag
	for _, t := range inherited {
		if t.Name != ContinuesTag {
			out = append(out, t)
		}
	}
	return append(out, Tag{Name: ContinuesTag, Metadata: map[string]string{"parent": parentID}})
}

// AutomationTag is the tag the Daemon gives the Sessions an Automation
// starts: metadata names the Automation and says "run" for a run.
const AutomationTag = "automation"

// AutomationRun is the Automation whose run a Session is, "" for none.
func AutomationRun(tags []Tag) (id, name string) {
	for _, t := range tags {
		if t.Name == AutomationTag && t.Metadata["type"] == "run" {
			return t.Metadata["automationId"], t.Metadata["automationName"]
		}
	}
	return "", ""
}

// WithoutAutomationRuns leaves out the runs, which the Automations page lists.
func WithoutAutomationRuns(list []Summary) []Summary {
	var out []Summary
	for _, s := range list {
		if id, _ := AutomationRun(s.Tags); id == "" {
			out = append(out, s)
		}
	}
	return out
}

func IsDraft(tags []Tag) bool   { return hasTag(tags, DraftTag) }
func IsScratch(tags []Tag) bool { return hasTag(tags, ScratchTag) }
func IsMemory(tags []Tag) bool  { return hasTag(tags, MemoryTag) }

var scratchFolder = regexp.MustCompile(`/\d{4}-\d{2}-\d{2}-[0-9a-f]{6}/?$`)

// IsScratchSession: the tag, or a Scratch folder's name, since some
// Sessions there come without the tag.
func IsScratchSession(s Summary) bool {
	return IsScratch(s.Tags) || (s.Cwd != "" && scratchFolder.MatchString(s.Cwd))
}

// Group keys that are no Workspace path.
const (
	RecentsGroupKey        = "droi:recents"
	PinnedSessionsGroupKey = "droi:pinned-sessions"
)

// FoldContinued drops Sessions another listed Session continues.
func FoldContinued(list []Summary) []Summary {
	parents := map[string]bool{}
	for _, s := range list {
		if s.ParentID != "" && s.ParentID != s.SessionID {
			parents[s.ParentID] = true
		}
	}
	var out []Summary
	for _, s := range list {
		if !parents[s.SessionID] {
			out = append(out, s)
		}
	}
	return out
}

// ContinuationChain is the listed Sessions s continues, nearest first.
func ContinuationChain(list []Summary, s Summary) []Summary {
	byID := map[string]Summary{}
	for _, x := range list {
		byID[x.SessionID] = x
	}
	seen := map[string]bool{s.SessionID: true}
	var chain []Summary
	p, ok := byID[s.ParentID]
	for ok && !seen[p.SessionID] {
		chain = append(chain, p)
		seen[p.SessionID] = true
		p, ok = byID[p.ParentID]
	}
	return chain
}

// MainSessions leaves the subagents out.
func MainSessions(list []Summary) []Summary {
	var out []Summary
	for _, s := range list {
		if s.CallingSessionID == "" {
			out = append(out, s)
		}
	}
	return out
}

// Group is a Workspace's Sessions, or Recents, or the pinned ones.
type Group struct {
	Key      string
	Label    string
	Path     string
	Scratch  bool
	Sessions []Summary
}

// Pins are the pinned Workspaces and Sessions.
type Pins struct {
	Workspaces map[string]bool
	Sessions   map[string]bool
}

// WorkspaceSort orders the groups; SessionSort the Sessions in them.
type (
	WorkspaceSort string
	SessionSort   string
)

const (
	SortMostSessions WorkspaceSort = "sessions"
	SortRecent       WorkspaceSort = "recent"
	SortName         WorkspaceSort = "name"
	SortManual       WorkspaceSort = "manual"

	SessionsRecent  SessionSort = "recent"
	SessionsCreated SessionSort = "created"
)

var WorkspaceSorts = []WorkspaceSort{SortMostSessions, SortRecent, SortName, SortManual}
var SessionSorts = []SessionSort{SessionsRecent, SessionsCreated}

var WorkspaceSortLabels = map[WorkspaceSort]string{
	SortMostSessions: l10n.N("Most sessions"), SortRecent: l10n.N("Recently active"), SortName: l10n.N("Name"), SortManual: l10n.N("Manual"),
}
var SessionSortLabels = map[SessionSort]string{SessionsRecent: l10n.N("Recently active"), SessionsCreated: l10n.N("Created")}

// Order is how the list is sorted.
type Order struct {
	Workspaces WorkspaceSort
	Sessions   SessionSort
	Manual     []string
	// FirstSeen is when each Session was first seen, in ms; the Daemon
	// lists no creation time.
	FirstSeen map[string]float64
}

var DefaultOrder = Order{Workspaces: SortMostSessions, Sessions: SessionsRecent}

// NoteFirstSeen adds the Sessions not seen before, at the earlier of now
// and their last change; it returns nil when none was new.
func NoteFirstSeen(firstSeen map[string]float64, list []Summary, nowMs float64) map[string]float64 {
	var next map[string]float64
	for _, s := range list {
		if _, ok := firstSeen[s.SessionID]; ok {
			continue
		}
		if next == nil {
			next = make(map[string]float64, len(firstSeen)+1)
			for k, v := range firstSeen {
				next[k] = v
			}
		}
		next[s.SessionID] = min(nowMs, float64(s.UpdatedAt)*1000)
	}
	return next
}

// GroupByWorkspace groups and sorts the list: by default the Workspace with
// the most conversations first, ties to the newest, and inside each the
// Sessions newest first. Pinned Workspaces and Sessions come first; with
// pinnedApart pinned Sessions leave their groups for one of their own.
// Scratch Sessions share one Recents group, always last.
func GroupByWorkspace(list []Summary, pins Pins, order Order, pinnedApart bool) []Group {
	groups := map[string]*Group{}
	var keys []string
	var recents, pinned *Group
	for _, s := range list {
		if pinnedApart && pins.Sessions[s.SessionID] {
			if pinned == nil {
				pinned = &Group{Key: PinnedSessionsGroupKey, Label: l10n.L("Pinned sessions")}
			}
			pinned.Sessions = append(pinned.Sessions, s)
			continue
		}
		if IsScratchSession(s) {
			if recents == nil {
				recents = &Group{Key: RecentsGroupKey, Label: l10n.L("Recents"), Scratch: true}
			}
			recents.Sessions = append(recents.Sessions, s)
			continue
		}
		path := WorkspaceOf(s)
		key := cmp.Or(path, "(unknown)")
		g := groups[key]
		if g == nil {
			g = &Group{Key: key, Label: WorkspaceLabel(path), Path: path}
			groups[key] = g
			keys = append(keys, key)
		}
		g.Sessions = append(g.Sessions, s)
	}
	result := make([]*Group, 0, len(keys))
	paths := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, groups[k])
		paths = append(paths, groups[k].Path)
	}
	named := WorkspaceLabels(paths)
	for _, g := range result {
		if l, ok := named[g.Path]; ok {
			g.Label = l
		}
	}
	created := func(s Summary) float64 {
		if v, ok := order.FirstSeen[s.SessionID]; ok {
			return v
		}
		return float64(s.UpdatedAt) * 1000
	}
	sortSessions := func(g *Group) {
		slices.SortStableFunc(g.Sessions, func(a, b Summary) int {
			if c := cmp.Compare(b2i(pins.Sessions[b.SessionID]), b2i(pins.Sessions[a.SessionID])); c != 0 {
				return c
			}
			if order.Sessions == SessionsCreated {
				if c := cmp.Compare(created(b), created(a)); c != 0 {
					return c
				}
			}
			return cmp.Compare(b.UpdatedAt, a.UpdatedAt)
		})
	}
	for _, g := range result {
		sortSessions(g)
	}
	if recents != nil {
		sortSessions(recents)
	}
	if pinned != nil {
		sortSessions(pinned)
	}
	newest := func(g *Group) int64 {
		var n int64
		for _, s := range g.Sessions {
			n = max(n, s.UpdatedAt)
		}
		return n
	}
	conversations := func(g *Group) int {
		n := 0
		for _, s := range g.Sessions {
			if s.MessagesCount == nil || *s.MessagesCount > 0 {
				n++
			}
		}
		return n
	}
	byDefault := func(a, b *Group) int {
		if c := cmp.Compare(conversations(b), conversations(a)); c != 0 {
			return c
		}
		return cmp.Compare(newest(b), newest(a))
	}
	place := func(g *Group) int {
		if i := slices.Index(order.Manual, g.Key); i >= 0 {
			return i
		}
		return len(order.Manual)
	}
	chosen := func(a, b *Group) int {
		switch order.Workspaces {
		case SortRecent:
			return cmp.Or(cmp.Compare(newest(b), newest(a)), byDefault(a, b))
		case SortName:
			return cmp.Or(localeCompare(a.Label, b.Label), byDefault(a, b))
		case SortManual:
			return cmp.Or(cmp.Compare(place(a), place(b)), byDefault(a, b))
		}
		return byDefault(a, b)
	}
	slices.SortStableFunc(result, func(a, b *Group) int {
		return cmp.Or(cmp.Compare(b2i(pins.Workspaces[b.Key]), b2i(pins.Workspaces[a.Key])), chosen(a, b))
	})
	var out []Group
	if pinned != nil {
		out = append(out, *pinned)
	}
	for _, g := range result {
		out = append(out, *g)
	}
	if recents != nil {
		out = append(out, *recents)
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// localeCompare approximates String.localeCompare for Workspace names:
// case folded first, then as is.
func localeCompare(a, b string) int {
	return cmp.Or(strings.Compare(strings.ToLower(a), strings.ToLower(b)), strings.Compare(a, b))
}

// MoveWorkspace is the manual order after moving key before or after
// target, from the order shown.
func MoveWorkspace(shown []string, key, target string, after bool) []string {
	if key == target {
		return slices.Clone(shown)
	}
	rest := slices.DeleteFunc(slices.Clone(shown), func(k string) bool { return k == key })
	at := slices.Index(rest, target)
	if at < 0 {
		return slices.Clone(shown)
	}
	if after {
		at++
	}
	return slices.Insert(rest, at, key)
}

// Sessions touched within RecentWindowMs always show; older ones wait
// behind "Show N older", OlderBatch at a time.
const (
	RecentWindowMs = 3 * 24 * 60 * 60 * 1000
	OlderBatch     = 30
)

// VisibleSessions are the rows a section shows: every pinned or recent
// Session plus the first revealed older ones, and how many stay hidden.
func VisibleSessions(list []Summary, revealed int, nowMs int64, pinned map[string]bool) ([]Summary, int) {
	cutoff := nowMs - RecentWindowMs
	var visible []Summary
	older := 0
	for _, s := range list {
		if pinned[s.SessionID] {
			visible = append(visible, s)
			continue
		}
		recent := s.UpdatedAt*1000 >= cutoff
		if recent || older < revealed {
			visible = append(visible, s)
		}
		if !recent {
			older++
		}
	}
	return visible, max(0, older-revealed)
}

// A Workspace none of whose Sessions changed in StaleWorkspaceMs waits
// behind "Show N older workspaces".
const StaleWorkspaceMs = 30 * 24 * 60 * 60 * 1000

// SplitStale parts groups, in order, into the Workspaces used within
// StaleWorkspaceMs or holding the Session keep, and the rest.
func SplitStale(groups []Group, nowMs int64, keep string) (active, stale []Group) {
	cutoff := nowMs - StaleWorkspaceMs
	for _, g := range groups {
		used := slices.ContainsFunc(g.Sessions, func(s Summary) bool { return s.UpdatedAt*1000 >= cutoff || (keep != "" && s.SessionID == keep) })
		if used {
			active = append(active, g)
		} else {
			stale = append(stale, g)
		}
	}
	return active, stale
}

var trailingSep = regexp.MustCompile(`[\\/]+$`)

func pathSegments(path string) []string {
	return strings.FieldsFunc(trailingSep.ReplaceAllString(path, ""), func(r rune) bool { return r == '/' || r == '\\' })
}

// WorkspaceLabel is a Workspace's last path segment.
func WorkspaceLabel(path string) string {
	if path == "" {
		return l10n.L("Unknown workspace")
	}
	parts := pathSegments(path)
	if len(parts) == 0 {
		return trailingSep.ReplaceAllString(path, "")
	}
	return parts[len(parts)-1]
}

// WorkspaceLabels is each path's WorkspaceLabel; where two would read the
// same, the folders above tell them apart, in brackets: /Users/me/dev/tmp
// and /private/tmp read "tmp (dev)" and "tmp (private)".
func WorkspaceLabels(paths []string) map[string]string {
	segs := make(map[string][]string, len(paths))
	for _, p := range paths {
		if p != "" {
			segs[p] = pathSegments(p)
		}
	}
	depth := make(map[string]int, len(segs))
	label := func(p string) string {
		s := segs[p]
		if len(s) == 0 {
			return WorkspaceLabel(p)
		}
		name := s[len(s)-1]
		if d := depth[p]; d > 0 {
			return name + " (" + strings.Join(s[len(s)-1-d:len(s)-1], "/") + ")"
		}
		return name
	}
	for {
		byLabel := make(map[string][]string, len(segs))
		for p := range segs {
			l := label(p)
			byLabel[l] = append(byLabel[l], p)
		}
		grew := false
		for _, same := range byLabel {
			if len(same) < 2 {
				continue
			}
			for _, p := range same {
				if depth[p] < len(segs[p])-1 {
					depth[p]++
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}
	out := make(map[string]string, len(segs))
	for p := range segs {
		out[p] = label(p)
	}
	return out
}

// RecentWorkspace is a Workspace the New session page offers.
type RecentWorkspace struct {
	Path, Label string
	LastUsedAt  int64
}

// RecentWorkspaces: most recent first, one per path, Scratch and
// Automation runs left out.
func RecentWorkspaces(list []Summary) []RecentWorkspace {
	byPath := map[string]*RecentWorkspace{}
	var order []string
	for _, s := range list {
		if id, _ := AutomationRun(s.Tags); IsScratchSession(s) || id != "" {
			continue
		}
		path := WorkspaceOf(s)
		if path == "" {
			continue
		}
		if w, ok := byPath[path]; !ok {
			byPath[path] = &RecentWorkspace{Path: path, LastUsedAt: s.UpdatedAt}
			order = append(order, path)
		} else if w.LastUsedAt < s.UpdatedAt {
			w.LastUsedAt = s.UpdatedAt
		}
	}
	named := WorkspaceLabels(order)
	out := make([]RecentWorkspace, 0, len(order))
	for _, p := range order {
		w := *byPath[p]
		w.Label = named[p]
		out = append(out, w)
	}
	slices.SortStableFunc(out, func(a, b RecentWorkspace) int { return cmp.Compare(b.LastUsedAt, a.LastUsedAt) })
	return out
}
