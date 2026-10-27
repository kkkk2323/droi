package memorywork

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

// The Memory before ADR 0031 kept entries in memory.sqlite. They are brought
// in once: the corrections and preferences, which the user gave, as Memory
// Notes, kept until the user removes them; every other entry as Session
// Summaries of their own, one set per Memory, which age like any other. The
// database is only read, so an older Droi still finds it as it was.

// LegacyDatabase is the earlier Memory's database in the Memory folder.
const LegacyDatabase = "memory.sqlite"

type legacyEntry struct {
	workspace, category, day, text, topic string
}

// legacyChunkBytes bounds one imported file, as a Session Summary is bounded.
const legacyChunkBytes = 8_500

var categoryTitles = map[string]string{
	"correction": "Corrections",
	"preference": "Preferences",
	"convention": "Conventions",
	"tool-quirk": "Tool quirks",
	"failure":    "Failures",
	"insight":    "Insights",
}

var categoryOrder = []string{"correction", "preference", "convention", "tool-quirk", "failure", "insight"}

func readLegacy(file string) ([]legacyEntry, error) {
	db, err := sql.Open("sqlite", "file:"+(&url.URL{Path: file}).EscapedPath()+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var topics int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'entry_topics'").Scan(&topics); err != nil {
		return nil, err
	}
	query := "SELECT COALESCE(s.workspace, ''), e.category, e.day, e.text, '' FROM entries e JOIN slots s ON s.key = e.slot"
	if topics > 0 {
		query = "SELECT COALESCE(s.workspace, ''), e.category, e.day, e.text, COALESCE(t.topic, '') FROM entries e JOIN slots s ON s.key = e.slot LEFT JOIN entry_topics t ON t.entry = e.id"
	}
	rows, err := db.Query(query + " ORDER BY e.day, e.rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []legacyEntry
	for rows.Next() {
		var e legacyEntry
		if err := rows.Scan(&e.workspace, &e.category, &e.day, &e.text, &e.topic); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// legacyGroup is one Memory of the earlier Memory: a Project Memory, the
// Global Memory's core, or one of its topics.
type legacyGroup struct {
	workspace, topic string
}

func (g legacyGroup) name() string {
	switch {
	case g.workspace != "":
		return g.workspace
	case g.topic != "":
		return "Global Memory, topic " + g.topic
	}
	return "Global Memory"
}

func (g legacyGroup) slug() string {
	base := "global"
	if g.workspace != "" {
		base = filepath.Base(g.workspace)
	} else if g.topic != "" {
		base = "topic-" + g.topic
	}
	s := cmp.Or(memory.Slug(base, 30), "memory")
	return s + "-" + memory.Hash([]byte(g.workspace + "\x00" + g.topic))[:6]
}

// chunks splits lines into runs of at most legacyChunkBytes.
func chunks(lines []string) [][]string {
	var out [][]string
	var cur []string
	size := 0
	for _, l := range lines {
		if len(cur) > 0 && size+len(l) > legacyChunkBytes {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, l)
		size += len(l)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// importLegacy brings the earlier Memory in, once, and answers how many files
// it wrote. Without a database it only marks the import done.
func importLegacy(dir string, st *memory.State, now time.Time) (int, error) {
	file := filepath.Join(dir, LegacyDatabase)
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		st.ImportedAt = iso(now)
		return 0, nil
	}
	entries, err := readLegacy(file)
	if err != nil {
		return 0, err
	}
	byGroup := map[legacyGroup]map[string][]legacyEntry{}
	var groups []legacyGroup
	for _, e := range entries {
		g := legacyGroup{workspace: e.workspace}
		if e.workspace == "" {
			g.topic = e.topic
		}
		if byGroup[g] == nil {
			byGroup[g] = map[string][]legacyEntry{}
			groups = append(groups, g)
		}
		byGroup[g][e.category] = append(byGroup[g][e.category], e)
	}
	slices.SortFunc(groups, func(a, b legacyGroup) int { return strings.Compare(a.name(), b.name()) })
	written := 0
	stamp := memory.TimeStamp(now)
	for _, g := range groups {
		cats := byGroup[g]
		// The user's own words: Memory Notes.
		for _, cat := range []string{"correction", "preference"} {
			var lines []string
			for _, e := range cats[cat] {
				lines = append(lines, "- "+oneLine(e.text)+" ("+e.day+")\n")
			}
			parts := chunks(lines)
			for i, part := range parts {
				name := fmt.Sprintf("%s/%s-imported-%s-%ss", memory.NotesDir, stamp, g.slug(), cat)
				if len(parts) > 1 {
					name += fmt.Sprintf("-%d", i+1)
				}
				body := "written_at: " + iso(now) + "\nimported_from: " + g.name() + "\n\n" +
					"The user's " + strings.ToLower(categoryTitles[cat]) + " for " + g.name() + ", kept by Droi's earlier Memory with the day each was recorded. Each still applies unless a later Memory Note says otherwise.\n\n" +
					strings.Join(part, "")
				if err := memory.WriteFileAtomic(filepath.Join(dir, filepath.FromSlash(name+".md")), []byte(memory.RedactSecrets(body))); err != nil {
					return written, err
				}
				written++
			}
		}
		// Everything else Droid recorded: Session Summaries of their own.
		var lines []string
		newest := map[int]string{}
		for _, cat := range categoryOrder[2:] {
			if len(cats[cat]) == 0 {
				continue
			}
			lines = append(lines, "\n## "+categoryTitles[cat]+"\n")
			for _, e := range cats[cat] {
				lines = append(lines, "- "+oneLine(e.text)+" ("+e.day+")\n")
			}
		}
		parts := chunks(lines)
		for i, part := range parts {
			for _, l := range part {
				if open := strings.LastIndex(l, "("); open >= 0 && strings.HasSuffix(l, ")\n") {
					if day := l[open+1 : len(l)-2]; day > newest[i] {
						newest[i] = day
					}
				}
			}
			updated := parseTime(newest[i] + "T00:00:00Z")
			if updated.IsZero() {
				updated = now
			}
			title := "Earlier Memory of " + g.name()
			if len(parts) > 1 {
				title += fmt.Sprintf(", part %d of %d", i+1, len(parts))
			}
			name := fmt.Sprintf("%s/%s-imported-%s-%d.md", memory.SessionSummariesDir, memory.TimeStamp(updated), g.slug(), i+1)
			body := "session_id: imported\nupdated_at: " + iso(updated) + "\nworkspace: " + g.name() + "\ntitle: " + title + "\n\n" +
				"# " + title + "\n\nEntries Droid recorded in earlier Sessions, before Droi kept Session Summaries, with the day each was recorded. Droid wrote them, not the user.\n" +
				strings.Join(part, "")
			if err := memory.WriteFileAtomic(filepath.Join(dir, filepath.FromSlash(name)), []byte(memory.RedactSecrets(body))); err != nil {
				return written, err
			}
			st.Summaries[name] = &memory.SummaryRecord{UpdatedAt: iso(updated), GeneratedAt: iso(now), Imported: true}
			written++
		}
	}
	st.ImportedAt = iso(now)
	return written, nil
}
