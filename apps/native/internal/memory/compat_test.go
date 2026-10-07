package memory

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type masterRow struct {
	Type    string  `json:"type"`
	Name    string  `json:"name"`
	TblName string  `json:"tbl_name"`
	SQL     *string `json:"sql"`
}

// testdata/schema.json is sqlite_master of a database the Electron app's
// store created (node:sqlite, SQLite 3.53).
func TestTheSchemaIsTheElectronApps(t *testing.T) {
	var want []masterRow
	if err := json.Unmarshal(must[[]byte](t)(os.ReadFile("testdata/schema.json")), &want); err != nil {
		t.Fatal(err)
	}
	_, dir := openTestStore(t)
	db := must[*sql.DB](t)(sql.Open("sqlite", filepath.Join(dir, DatabaseFile)))
	defer db.Close()
	rows := must[*sql.Rows](t)(db.Query("SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name"))
	defer rows.Close()
	got := []masterRow{}
	for rows.Next() {
		var r masterRow
		var text sql.NullString
		if err := rows.Scan(&r.Type, &r.Name, &r.TblName, &text); err != nil {
			t.Fatal(err)
		}
		if text.Valid {
			r.SQL = &text.String
		}
		got = append(got, r)
	}
	if len(got) != len(want) {
		t.Fatalf("%d objects, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Type != want[i].Type || got[i].Name != want[i].Name || got[i].TblName != want[i].TblName || (got[i].SQL == nil) != (want[i].SQL == nil) ||
			got[i].SQL != nil && *got[i].SQL != *want[i].SQL {
			t.Errorf("object %d:\ngot  %+v\nwant %+v", i, got[i], want[i])
		}
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q %v", mode, err)
	}
}

func TestAStoreOpensAgainOverItsOwnDatabase(t *testing.T) {
	s, dir := openTestStore(t)
	entry := add(t, s, project, "survives a reopen", CategoryConvention)
	again := must[*Store](t)(OpenStore(dir))
	defer again.Close()
	got, ok := getEntry(t, again, entry.ID)
	if !ok || got != entry {
		t.Fatalf("%+v", got)
	}
}

// nodeWithSQLite is node when it has node:sqlite (22.5 or later).
func nodeWithSQLite(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH")
	}
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		t.Skip("node --version failed")
	}
	m := regexp.MustCompile(`^v(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		t.Skipf("node version %q", out)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 22 || major == 22 && minor < 5 {
		t.Skipf("node %s has no node:sqlite", m[0])
	}
	return node
}

// The Electron app reads and writes the same file through node:sqlite. The
// script reads what Go wrote, searches it through the FTS index, and writes an
// entry Go then has to find; the triggers keep the index in step either way.
const nodeScript = `
import { DatabaseSync } from 'node:sqlite'
const [file, id] = process.argv.slice(1)
const db = new DatabaseSync(file)
db.exec('PRAGMA busy_timeout = 5000; PRAGMA journal_mode = WAL;')
const found = db.prepare("SELECT e.id, e.text, s.workspace FROM entries e JOIN slots s ON s.key = e.slot JOIN entries_fts f ON f.rowid = e.rowid WHERE entries_fts MATCH ? ORDER BY bm25(entries_fts)").all('"vitest"')
const calls = db.prepare('SELECT tool, ok FROM calls').all()
db.prepare("INSERT INTO entries (id, slot, category, day, text) VALUES (?, 'global', 'preference', '2026-10-07', ?)").run(id, '回答用中文 written by node')
console.log(JSON.stringify({ found: found.map((r) => ({ ...r })), calls: calls.map((r) => ({ ...r })) }))
db.close()
`

func TestTheElectronAppsSQLiteReadsAndWritesAGoDatabase(t *testing.T) {
	node := nodeWithSQLite(t)
	s, dir := openTestStore(t)
	entry := add(t, s, project, "Unit tests run with vitest", CategoryConvention)
	add(t, s, GlobalSlot, "terse answers", CategoryPreference)
	q := "vitest"
	if err := s.LogCall(Call{SessionID: "s", Tool: "memory_search", Slot: &project, Query: &q, OK: true, Found: []string{entry.ID}}); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(node, "--input-type=module", "--no-warnings", "-e", nodeScript, filepath.Join(dir, DatabaseFile), "0dde0dde")
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if e, ok := err.(*exec.ExitError); ok {
			stderr = e.Stderr
		}
		t.Fatalf("%v: %s", err, stderr)
	}
	var got struct {
		Found []struct{ ID, Text, Workspace string }
		Calls []struct {
			Tool string
			OK   int
		}
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(got.Found) != 1 || got.Found[0].ID != entry.ID || got.Found[0].Workspace != project.Workspace {
		t.Fatalf("%s", out)
	}
	if len(got.Calls) != 1 || got.Calls[0].Tool != "memory_search" || got.Calls[0].OK != 1 {
		t.Fatalf("%s", out)
	}

	fromNode, ok := getEntry(t, s, "0dde0dde")
	if !ok || fromNode.Scope != ScopeGlobal || fromNode.Category != CategoryPreference {
		t.Fatalf("%+v", fromNode)
	}
	eq(t, texts(search(t, s, SearchOptions{Query: "written", Slot: GlobalSlot})), []string{"回答用中文 written by node"})
	eq(t, texts(search(t, s, SearchOptions{Query: "中文", Slot: GlobalSlot})), []string{"回答用中文 written by node"})
}

// When the Electron app's sources sit beside this package, its own store opens
// a database the Go store created, and the Go store one the TS store created.
func TestTheElectronAppsStoreAndThisOneOpenEachOthersDatabase(t *testing.T) {
	node := nodeWithSQLite(t)
	src := must[string](t)(filepath.Abs("../../../desktop/src/memory"))
	if _, err := os.Stat(filepath.Join(src, "store.ts")); err != nil {
		t.Skip("the Electron app's Memory sources are not here")
	}
	script := `
import { openMemoryStore } from ` + strconvQuote(filepath.Join(src, "store.ts")) + `
const [goDir, tsDir] = process.argv.slice(1)
const fromGo = openMemoryStore(goDir)
const listed = fromGo.list({ scope: 'project', workspace: '/Users/dev/app' })
const found = fromGo.search({ query: 'vitest', slot: { scope: 'project', workspace: '/Users/dev/app' } })
const usage = fromGo.usage({ scope: 'project', workspace: '/Users/dev/app' })
const summaries = fromGo.summaries()
fromGo.close()
const ts = openMemoryStore(tsDir)
const added = ts.add({ scope: 'project', workspace: '/Users/dev/app' }, 'correction', 'Use pnpm, not npm')
ts.logCall({ sessionId: 's', tool: 'memory_add', slot: { scope: 'project', workspace: '/Users/dev/app' }, query: null, ok: true, written: added.entry.id })
ts.markConsolidated({ scope: 'global' })
ts.close()
console.log(JSON.stringify({ listed, found, usage, summaries, added: added.entry }))
`
	goStore, goDir := openTestStore(t)
	entry := add(t, goStore, project, "Unit tests run with vitest", CategoryConvention)
	if err := goStore.MarkConsolidated(project); err != nil {
		t.Fatal(err)
	}
	tsDir := t.TempDir()
	cmd := exec.Command(node, "--import", filepath.Join(src, "test-support", "run-ts.mjs"), "--input-type=module", "--no-warnings", "-e", script, goDir, tsDir)
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if e, ok := err.(*exec.ExitError); ok {
			stderr = e.Stderr
		}
		t.Skipf("node could not run the TS store (%v): %s", err, stderr)
	}
	var got struct {
		Listed, Found []struct{ ID, Scope, Workspace, Category, Day, Text string }
		Usage         struct{ Searches, EmptySearches, NeverFound int }
		Summaries     []struct {
			Scope, Workspace string
			Entries, Chars   int
			LastConsolidated string
		}
		Added struct{ ID, Text string }
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(got.Listed) != 1 || got.Listed[0].ID != entry.ID || got.Listed[0].Day != entry.Day || len(got.Found) != 1 || got.Usage.NeverFound != 1 {
		t.Fatalf("%s", out)
	}
	if len(got.Summaries) != 2 || got.Summaries[0].Workspace != project.Workspace || got.Summaries[0].Chars != len(entry.Text) || !isoTime.MatchString(got.Summaries[0].LastConsolidated) {
		t.Fatalf("%s", out)
	}

	ts := must[*Store](t)(OpenStore(tsDir))
	defer ts.Close()
	corrections := must[[]Entry](t)(ts.Corrections([]Slot{project, GlobalSlot}, CorrectionSliceCaps))
	if len(corrections) != 1 || corrections[0].ID != got.Added.ID || corrections[0].Text != "Use pnpm, not npm" {
		t.Fatalf("%+v", corrections)
	}
	eq(t, texts(search(t, ts, SearchOptions{Query: "pnpm", Slot: project})), []string{"Use pnpm, not npm"})
	summaries := must[[]SlotSummary](t)(ts.Summaries())
	if len(summaries) != 2 || !isoTime.MatchString(summaries[1].LastConsolidated) {
		t.Fatalf("%+v", summaries)
	}
	eq(t, must[string](t)(ts.LoggedSince()) != "", true)
}

func strconvQuote(s string) string { return strconv.Quote(s) }
