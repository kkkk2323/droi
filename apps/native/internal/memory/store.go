// Memory's source of truth: one SQLite database in the Memory folder (ADR
// 0010), shared with the Electron Desktop Shell, so its schema and the SQL
// text that creates it are those of apps/desktop/src/memory/store.ts. Every
// Session runs its own Memory Server process and the hooks open it too, so it
// runs in WAL mode with a busy timeout and every write is one short
// transaction.

// Package memory is Droi's Memory core: the store, the Memory Server (a stdio
// MCP Server), the hooks, the Markdown export and what they read from the
// Daemon's Session files. It is a port of apps/desktop/src/memory.
package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// Category is what kind of fact an entry records.
type Category string

const (
	CategoryFailure    Category = "failure"
	CategoryCorrection Category = "correction"
	CategoryInsight    Category = "insight"
	CategoryPreference Category = "preference"
	CategoryConvention Category = "convention"
	CategoryToolQuirk  Category = "tool-quirk"
)

// Categories in the order the tools' schemas list them.
var Categories = []Category{
	CategoryFailure, CategoryCorrection, CategoryInsight, CategoryPreference, CategoryConvention, CategoryToolQuirk,
}

// IsCategory says whether value names a Category.
func IsCategory(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, c := range Categories {
		if string(c) == s {
			return true
		}
	}
	return false
}

// Scope is Project Memory or Global Memory.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Entry is one fact in Memory.
type Entry struct {
	// ID is 8 lowercase hex characters.
	ID    string
	Scope Scope
	// Workspace is the Workspace path for Project Memory; "" for Global Memory.
	Workspace string
	Category  Category
	// Day is the UTC day the entry was recorded or last rewritten, YYYY-MM-DD.
	Day  string
	Text string
}

// Limit is a Memory's size limits in characters of entry text: past Soft,
// Settings suggests consolidating; past Hard, writes are refused.
type Limit struct{ Soft, Hard int }

// Limits per Scope.
var Limits = map[Scope]Limit{
	ScopeProject: {Soft: 200_000, Hard: 300_000},
	ScopeGlobal:  {Soft: 40_000, Hard: 60_000},
}

// DatabaseFile is the database's name in the Memory folder.
const DatabaseFile = "memory.sqlite"

// Slot is one Project Memory or the Global Memory.
type Slot struct {
	Scope Scope
	// Workspace is the Workspace path of a Project Memory.
	Workspace string
}

// GlobalSlot is the Global Memory.
var GlobalSlot = Slot{Scope: ScopeGlobal}

// ProjectSlot is a Workspace's Project Memory. The Daemon may hand over the
// path a Session was opened with while a process's cwd is resolved (/tmp
// against /private/tmp), so both are keyed by the real path. A linked git
// worktree is keyed by the same place in its main checkout, so every
// worktree of a repository shares the repository's Project Memory.
func ProjectSlot(workspace string) Slot {
	path := workspace
	if abs, err := filepath.Abs(workspace); err == nil {
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			path = mainCheckout(real)
		}
	}
	// A Workspace that is gone keeps the path it was recorded under.
	return Slot{Scope: ScopeProject, Workspace: path}
}

// SlotOf is the Memory an entry belongs to.
func SlotOf(e Entry) Slot {
	if e.Scope == ScopeGlobal {
		return GlobalSlot
	}
	return Slot{Scope: ScopeProject, Workspace: e.Workspace}
}

// WorkspaceKey is a Project Memory's key in the database.
func WorkspaceKey(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:16]
}

const globalKey = "global"

func slotKey(slot Slot) string {
	if slot.Scope == ScopeGlobal {
		return globalKey
	}
	return WorkspaceKey(slot.Workspace)
}

// SlotSummary is one Memory's size as Settings → Memory shows it.
type SlotSummary struct {
	Scope     Scope
	Workspace string
	Entries   int
	Chars     int
	// LastConsolidated is an ISO timestamp; "" before the first consolidation.
	LastConsolidated string
	OverSoftLimit    bool
}

// WriteResult is a write's outcome: the entry written, or why nothing was.
type WriteResult struct {
	OK            bool
	Entry         Entry
	OverSoftLimit bool
	// Reason says why the write was refused.
	Reason string
}

// SearchOptions are a search's query and where it looks.
type SearchOptions struct {
	Query string
	Slot  Slot
	// Category limits the search to one category; "" searches every one.
	Category Category
	// Limit defaults to 10.
	Limit int
}

// SliceChanges is a consolidation's answer for one slice: what becomes of
// each entry it was sent.
type SliceChanges struct {
	Keep    []string  `json:"keep"`
	Rewrite []Rewrite `json:"rewrite"`
	Remove  []string  `json:"remove"`
	// Merge makes several entries one new entry with the text.
	Merge []Merge `json:"merge"`
}

// Rewrite gives an entry new text.
type Rewrite struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Merge replaces the entries IDs with one entry of Text.
type Merge struct {
	IDs  []string `json:"ids"`
	Text string   `json:"text"`
}

// Call is one Memory Server call as the call log keeps it.
type Call struct {
	// SessionID is the calling Session; "" when unknown.
	SessionID string
	Tool      string
	// Slot is the Memory the call reached; nil when it reached none.
	Slot *Slot
	// Query is a search's query; nil for other calls.
	Query *string
	OK    bool
	// Found are the entries a search returned.
	Found []string
	// Written is the entry an add, replace or remove changed.
	Written string
	// Similar are the entries an add was shown as possibly saying the same thing.
	Similar []string
}

// Usage is how the searches of one Memory went since calls were first logged.
type Usage struct {
	Searches int
	// EmptySearches returned nothing.
	EmptySearches int
	// NeverFound counts entries no search has returned; corrections are left
	// out, the hook injects them.
	NeverFound int
}

// CorrectionCaps bound the corrections put in front of a Session.
type CorrectionCaps struct{ Entries, Chars int }

// Store is an open Memory database. It holds one connection, as the TS store
// does, so BEGIN IMMEDIATE transactions are its own; it is safe for use from
// several goroutines.
type Store struct {
	dir  string
	db   *sql.DB
	conn *sql.Conn
	mu   sync.Mutex
}

// The SQL text is store.ts's to the byte: SQLite keeps it in sqlite_master,
// and the schema test compares that with the Electron app's.
const pragmas = `
    PRAGMA busy_timeout = 5000;
    PRAGMA journal_mode = WAL;
    PRAGMA synchronous = NORMAL;
  `

const schema = `
    CREATE TABLE IF NOT EXISTS slots (
      key TEXT PRIMARY KEY,
      workspace TEXT,
      last_consolidated TEXT
    );
    CREATE TABLE IF NOT EXISTS entries (
      rowid INTEGER PRIMARY KEY,
      id TEXT NOT NULL UNIQUE,
      slot TEXT NOT NULL REFERENCES slots(key),
      category TEXT NOT NULL,
      day TEXT NOT NULL,
      text TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS entries_slot ON entries(slot, category);
    CREATE TABLE IF NOT EXISTS session_writes (
      session_id TEXT PRIMARY KEY,
      at TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS calls (
      rowid INTEGER PRIMARY KEY,
      at TEXT NOT NULL,
      session_id TEXT,
      tool TEXT NOT NULL,
      slot TEXT,
      query TEXT,
      ok INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS calls_slot ON calls(slot, tool);
    CREATE TABLE IF NOT EXISTS call_entries (
      call INTEGER NOT NULL REFERENCES calls(rowid),
      entry TEXT NOT NULL,
      role TEXT NOT NULL CHECK (role IN ('found', 'written', 'similar'))
    );
    CREATE INDEX IF NOT EXISTS call_entries_entry ON call_entries(entry, role);
    CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts
      USING fts5(text, content='entries', content_rowid='rowid', tokenize='trigram');
    CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
      INSERT INTO entries_fts(rowid, text) VALUES (new.rowid, new.text);
    END;
    CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
      INSERT INTO entries_fts(entries_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
    END;
    CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE OF text ON entries BEGIN
      INSERT INTO entries_fts(entries_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
      INSERT INTO entries_fts(rowid, text) VALUES (new.rowid, new.text);
    END;
  `

const selectEntries = `SELECT e.id, e.slot, e.category, e.day, e.text, s.workspace
    FROM entries e JOIN slots s ON s.key = e.slot`

// OpenStore opens the Memory database in dir, creating both if need be.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, DatabaseFile))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{dir: dir, db: db, conn: conn}
	// The Daemon starts a Session's Memory Server and its hook at once, and
	// on a new database one of them can find the other switching it to WAL
	// or creating the schema: SQLite answers that busy at once, without
	// waiting out busy_timeout. Both steps are idempotent, so try again.
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := s.prepare()
		if err == nil {
			return s, nil
		}
		if !isBusy(err) || time.Now().After(deadline) {
			s.Close()
			return nil, err
		}
		time.Sleep(time.Duration(20+mrand.IntN(60)) * time.Millisecond)
	}
}

func (s *Store) prepare() error {
	// node:sqlite turns foreign key enforcement on by default; the Electron
	// app's store therefore enforces them, and so does this one.
	for _, q := range []string{pragmas, "PRAGMA foreign_keys = ON", schema} {
		if _, err := s.exec(q); err != nil {
			return err
		}
	}
	return nil
}

func isBusy(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// Dir is the Memory folder.
func (s *Store) Dir() string { return s.dir }

// Close closes the database.
func (s *Store) Close() error {
	err := s.conn.Close()
	return errors.Join(err, s.db.Close())
}

func (s *Store) exec(q string, args ...any) (sql.Result, error) {
	return s.conn.ExecContext(context.Background(), q, args...)
}

func (s *Store) queryRow(q string, args ...any) *sql.Row {
	return s.conn.QueryRowContext(context.Background(), q, args...)
}

func (s *Store) rows(q string, args ...any) ([]Entry, error) {
	r, err := s.conn.QueryContext(context.Background(), q, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	entries := []Entry{}
	for r.Next() {
		var e Entry
		var key, category string
		var workspace sql.NullString
		if err := r.Scan(&e.ID, &key, &category, &e.Day, &e.Text, &workspace); err != nil {
			return nil, err
		}
		e.Category = Category(category)
		if key == globalKey {
			e.Scope = ScopeGlobal
		} else {
			e.Scope = ScopeProject
			e.Workspace = workspace.String
		}
		entries = append(entries, e)
	}
	return entries, r.Err()
}

func (s *Store) transaction(run func() error) error {
	if _, err := s.exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	err := run()
	if err == nil {
		_, err = s.exec("COMMIT")
	}
	if err != nil {
		_, _ = s.exec("ROLLBACK")
	}
	return err
}

func (s *Store) ensureSlot(slot Slot) error {
	var workspace any
	if slot.Scope != ScopeGlobal {
		workspace = slot.Workspace
	}
	_, err := s.exec("INSERT OR IGNORE INTO slots (key, workspace) VALUES (?, ?)", slotKey(slot), workspace)
	return err
}

// sizeOf counts in SQLite's LENGTH, characters, where the TS store adds a new
// text's JavaScript length, UTF-16 units; both are kept as they are.
func (s *Store) sizeOf(key string) (int, error) {
	var n int64
	err := s.queryRow("SELECT COALESCE(SUM(LENGTH(text)), 0) AS n FROM entries WHERE slot = ?", key).Scan(&n)
	return int(n), err
}

func (s *Store) newID() (string, error) {
	for {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id := hex.EncodeToString(b)
		var one int
		err := s.queryRow("SELECT 1 FROM entries WHERE id = ?", id).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
	}
}

func (s *Store) get(id string) (Entry, bool, error) {
	found, err := s.rows(selectEntries+" WHERE e.id = ?", id)
	if err != nil || len(found) == 0 {
		return Entry{}, false, err
	}
	return found[0], true, nil
}

func checkText(text string) string {
	if jsTrim(text) == "" {
		return "The text is empty."
	}
	return FindSecret(text)
}

func (s *Store) insert(key string, category Category, text string) (string, error) {
	id, err := s.newID()
	if err != nil {
		return "", err
	}
	_, err = s.exec("INSERT INTO entries (id, slot, category, day, text) VALUES (?, ?, ?, ?, ?)", id, key, string(category), today(), text)
	return id, err
}

// Add records one entry in a Memory, refusing empty text, a secret and a
// write past the Memory's hard limit.
func (s *Store) Add(slot Slot, category Category, rawText string) (WriteResult, error) {
	text := jsTrim(rawText)
	if refused := checkText(text); refused != "" {
		return WriteResult{Reason: refused}, nil
	}
	limits := Limits[slot.Scope]
	s.mu.Lock()
	defer s.mu.Unlock()
	var result WriteResult
	err := s.transaction(func() error {
		if err := s.ensureSlot(slot); err != nil {
			return err
		}
		key := slotKey(slot)
		size, err := s.sizeOf(key)
		if err != nil {
			return err
		}
		if size+jsLength(text) > limits.Hard {
			where := "This Workspace’s Memory"
			if slot.Scope == ScopeGlobal {
				where = "Global Memory"
			}
			result = WriteResult{Reason: fmt.Sprintf("%s is full (%d of %d characters). Nothing was saved. Ask the user to consolidate it in Droi under Settings → Memory, or replace or remove entries that are no longer true.", where, size, limits.Hard)}
			return nil
		}
		id, err := s.insert(key, category, text)
		if err != nil {
			return err
		}
		entry, _, err := s.get(id)
		result = WriteResult{OK: true, Entry: entry, OverSoftLimit: size+jsLength(text) > limits.Soft}
		return err
	})
	return result, err
}

// Replace rewrites an entry's text, keeping its id and category.
func (s *Store) Replace(id, rawText string) (WriteResult, error) {
	text := jsTrim(rawText)
	if refused := checkText(text); refused != "" {
		return WriteResult{Reason: refused}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result WriteResult
	err := s.transaction(func() error {
		found, ok, err := s.get(id)
		if err != nil {
			return err
		}
		if !ok {
			result = WriteResult{Reason: "No Memory entry has the id " + id + "."}
			return nil
		}
		if _, err := s.exec("UPDATE entries SET text = ?, day = ? WHERE id = ?", text, today(), id); err != nil {
			return err
		}
		slot := SlotOf(found)
		entry, _, err := s.get(id)
		if err != nil {
			return err
		}
		size, err := s.sizeOf(slotKey(slot))
		result = WriteResult{OK: true, Entry: entry, OverSoftLimit: size > Limits[slot.Scope].Soft}
		return err
	})
	return result, err
}

// Remove deletes an entry and says whether there was one.
func (s *Store) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.exec("DELETE FROM entries WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// Get finds an entry by id.
func (s *Store) Get(id string) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

// List is every entry of a Memory, by category, oldest first; or those of one
// category when category is not "".
func (s *Store) List(slot Slot, category Category) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := slotKey(slot)
	if category != "" {
		return s.rows(selectEntries+" WHERE e.slot = ? AND e.category = ? ORDER BY e.rowid", key, string(category))
	}
	return s.rows(selectEntries+" WHERE e.slot = ? ORDER BY e.category, e.rowid", key)
}

var termPattern = jsRegexp(`"([^"]*)"|(\S+)`)

// SearchTerms are the terms of a search: words, or quoted phrases kept whole.
// A one-character term beside others is dropped: "用" or "a" would match
// almost every entry.
func SearchTerms(query string) []string {
	terms := []string{}
	for _, m := range termPattern.FindAllStringSubmatchIndex(query, -1) {
		var term string
		if m[2] >= 0 {
			term = query[m[2]:m[3]]
		} else {
			term = query[m[4]:m[5]]
		}
		if term = jsTrim(term); term != "" {
			terms = append(terms, term)
		}
	}
	longer := []string{}
	for _, t := range terms {
		if utf8.RuneCountInString(t) > 1 {
			longer = append(longer, t)
		}
	}
	if len(longer) > 0 {
		return longer
	}
	return terms
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Search finds a Memory's entries by keyword: entries holding every term
// first, ranked by bm25; any term only when none holds them all.
func (s *Store) Search(o SearchOptions) ([]Entry, error) {
	limit := o.Limit
	if limit == 0 {
		limit = 10
	}
	key := slotKey(o.Slot)
	byCategory := ""
	var extra []any
	if o.Category != "" {
		byCategory = " AND e.category = ?"
		extra = []any{string(o.Category)}
	}
	terms := SearchTerms(o.Query)
	if len(terms) == 0 {
		return []Entry{}, nil
	}
	// The trigram tokenizer matches nothing shorter than three characters, so a
	// short term (a two-character Chinese word, say) is matched as a substring.
	var long, short []string
	for _, t := range terms {
		if utf8.RuneCountInString(t) >= 3 {
			long = append(long, t)
		} else {
			short = append(short, t)
		}
	}
	likeClause := func(count int, operator string) string {
		if count == 0 {
			return ""
		}
		parts := make([]string, count)
		for i := range parts {
			parts[i] = "e.text LIKE ? ESCAPE '\\'"
		}
		return " AND (" + strings.Join(parts, " "+operator+" ") + ")"
	}
	var likeParams []any
	for _, t := range short {
		likeParams = append(likeParams, "%"+likeEscaper.Replace(t)+"%")
	}
	matched := func(operator string, withShort bool) ([]Entry, error) {
		phrases := make([]string, len(long))
		for i, t := range long {
			phrases[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
		}
		clause := ""
		args := append([]any{strings.Join(phrases, " "+operator+" "), key}, extra...)
		if withShort {
			clause = likeClause(len(short), operator)
			args = append(args, likeParams...)
		}
		args = append(args, limit)
		return s.rows(selectEntries+` JOIN entries_fts f ON f.rowid = e.rowid
            WHERE entries_fts MATCH ? AND e.slot = ?`+byCategory+clause+`
            ORDER BY bm25(entries_fts) LIMIT ?`, args...)
	}
	substrings := func(operator string) ([]Entry, error) {
		args := append(append(append([]any{key}, extra...), likeParams...), limit)
		return s.rows(selectEntries+" WHERE e.slot = ?"+byCategory+likeClause(len(short), operator)+" ORDER BY e.rowid DESC LIMIT ?", args...)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(long) == 0 {
		return substrings("OR")
	}
	// Every term first, which is the precise answer; any term only when nothing
	// holds them all, with the best-matched entries first.
	all, err := matched("AND", true)
	if err != nil || len(all) > 0 {
		return all, err
	}
	anyTerm, err := matched("OR", false)
	if err != nil || len(short) == 0 || len(anyTerm) >= limit {
		return anyTerm, err
	}
	more, err := substrings("OR")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range anyTerm {
		seen[e.ID] = true
	}
	for _, e := range more {
		if !seen[e.ID] {
			anyTerm = append(anyTerm, e)
		}
	}
	if len(anyTerm) > limit {
		anyTerm = anyTerm[:limit]
	}
	return anyTerm, nil
}

// Corrections are the correction entries of these Memories together, newest
// first, within both caps.
func (s *Store) Corrections(slots []Slot, caps CorrectionCaps) ([]Entry, error) {
	if len(slots) == 0 {
		return []Entry{}, nil
	}
	marks := make([]string, len(slots))
	args := make([]any, 0, len(slots)+1)
	for i, slot := range slots {
		marks[i] = "?"
		args = append(args, slotKey(slot))
	}
	args = append(args, caps.Entries)
	s.mu.Lock()
	found, err := s.rows(selectEntries+` WHERE e.slot IN (`+strings.Join(marks, ", ")+`) AND e.category = 'correction'
          ORDER BY e.day DESC, e.rowid DESC LIMIT ?`, args...)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	kept := []Entry{}
	chars := 0
	for _, e := range found {
		if chars+jsLength(e.Text) > caps.Chars {
			break
		}
		chars += jsLength(e.Text)
		kept = append(kept, e)
	}
	return kept, nil
}

// Size is a Memory's size in characters of entry text.
func (s *Store) Size(slot Slot) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sizeOf(slotKey(slot))
}

// Summaries are every Project Memory with entries, then the Global Memory.
func (s *Store) Summaries() ([]SlotSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.conn.QueryContext(context.Background(), `SELECT s.key, s.workspace, s.last_consolidated AS lastConsolidated,
             COUNT(e.id) AS entries, COALESCE(SUM(LENGTH(e.text)), 0) AS chars
           FROM slots s LEFT JOIN entries e ON e.slot = s.key
           GROUP BY s.key ORDER BY s.workspace`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	summaries := []SlotSummary{}
	global := SlotSummary{Scope: ScopeGlobal}
	for r.Next() {
		var key string
		var workspace, last sql.NullString
		var entries, chars int64
		if err := r.Scan(&key, &workspace, &last, &entries, &chars); err != nil {
			return nil, err
		}
		if key == globalKey {
			global = SlotSummary{Scope: ScopeGlobal, Entries: int(entries), Chars: int(chars), LastConsolidated: last.String, OverSoftLimit: int(chars) > Limits[ScopeGlobal].Soft}
			continue
		}
		if entries > 0 {
			summaries = append(summaries, SlotSummary{
				Scope: ScopeProject, Workspace: workspace.String, Entries: int(entries), Chars: int(chars),
				LastConsolidated: last.String, OverSoftLimit: int(chars) > Limits[ScopeProject].Soft,
			})
		}
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	return append(summaries, global), nil
}

// ApplySlice applies a consolidation to exactly the entries that were sent.
// Anything naming an id outside sent, or leaving one of them unaccounted for,
// changes nothing, and so does a slice another Session changed meanwhile; the
// answer is then the reason, and "" once applied.
func (s *Store) ApplySlice(sent []Entry, changes SliceChanges) (string, error) {
	sentIDs := map[string]bool{}
	for _, e := range sent {
		sentIDs[e.ID] = true
	}
	named := append([]string{}, changes.Keep...)
	for _, r := range changes.Rewrite {
		named = append(named, r.ID)
	}
	named = append(named, changes.Remove...)
	for _, m := range changes.Merge {
		named = append(named, m.IDs...)
	}
	namedSet := map[string]bool{}
	for _, id := range named {
		if !sentIDs[id] {
			return "The answer names " + id + ", which was not sent.", nil
		}
		namedSet[id] = true
	}
	if len(namedSet) != len(named) {
		return "The answer names an entry more than once.", nil
	}
	for _, e := range sent {
		if !namedSet[e.ID] {
			return "The answer leaves " + e.ID + " unaccounted for.", nil
		}
	}
	for _, r := range changes.Rewrite {
		if refused := checkText(r.Text); refused != "" {
			return refused, nil
		}
	}
	for _, m := range changes.Merge {
		if refused := checkText(m.Text); refused != "" {
			return refused, nil
		}
	}
	if len(sent) == 0 {
		return "", nil
	}
	first := sent[0]
	key := slotKey(SlotOf(first))
	s.mu.Lock()
	defer s.mu.Unlock()
	reason := ""
	err := s.transaction(func() error {
		// Another Session may have changed the slice while the model worked on it.
		for _, e := range sent {
			current, ok, err := s.get(e.ID)
			if err != nil {
				return err
			}
			if !ok || current.Text != e.Text {
				reason = e.ID + " changed while it was being consolidated."
				return nil
			}
		}
		for _, r := range changes.Rewrite {
			if _, err := s.exec("UPDATE entries SET text = ?, day = ? WHERE id = ?", jsTrim(r.Text), today(), r.ID); err != nil {
				return err
			}
		}
		for _, id := range changes.Remove {
			if _, err := s.exec("DELETE FROM entries WHERE id = ?", id); err != nil {
				return err
			}
		}
		for _, m := range changes.Merge {
			for _, id := range m.IDs {
				if _, err := s.exec("DELETE FROM entries WHERE id = ?", id); err != nil {
					return err
				}
			}
			if _, err := s.insert(key, first.Category, jsTrim(m.Text)); err != nil {
				return err
			}
		}
		return nil
	})
	return reason, err
}

// MarkConsolidated records that a Memory was consolidated now.
func (s *Store) MarkConsolidated(slot Slot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureSlot(slot); err != nil {
		return err
	}
	_, err := s.exec("UPDATE slots SET last_consolidated = ? WHERE key = ?", isoNow(), slotKey(slot))
	return err
}

// LogCall records one Memory Server call, so Settings can say how Memory is
// used.
func (s *Store) LogCall(call Call) error {
	var sessionID, slot, query any
	if call.SessionID != "" {
		sessionID = call.SessionID
	}
	if call.Slot != nil {
		slot = slotKey(*call.Slot)
	}
	if call.Query != nil {
		query = *call.Query
	}
	ok := 0
	if call.OK {
		ok = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transaction(func() error {
		r, err := s.exec("INSERT INTO calls (at, session_id, tool, slot, query, ok) VALUES (?, ?, ?, ?, ?, ?)", isoNow(), sessionID, call.Tool, slot, query, ok)
		if err != nil {
			return err
		}
		rowid, err := r.LastInsertId()
		if err != nil {
			return err
		}
		link := func(id, role string) error {
			_, err := s.exec("INSERT INTO call_entries (call, entry, role) VALUES (?, ?, ?)", rowid, id, role)
			return err
		}
		for _, id := range call.Found {
			if err := link(id, "found"); err != nil {
				return err
			}
		}
		if call.Written != "" {
			if err := link(call.Written, "written"); err != nil {
				return err
			}
		}
		for _, id := range call.Similar {
			if err := link(id, "similar"); err != nil {
				return err
			}
		}
		return nil
	})
}

// Usage says how the searches of one Memory went since calls were first
// logged.
func (s *Store) Usage(slot Slot) (Usage, error) {
	key := slotKey(slot)
	s.mu.Lock()
	defer s.mu.Unlock()
	var searches, empty, never int64
	err := s.queryRow(`SELECT COUNT(*) AS searches,
             COALESCE(SUM(NOT EXISTS (
               SELECT 1 FROM call_entries h WHERE h.call = c.rowid AND h.role = 'found'
             )), 0) AS empty
           FROM calls c WHERE c.slot = ? AND c.tool = 'memory_search' AND c.ok = 1`, key).Scan(&searches, &empty)
	if err != nil {
		return Usage{}, err
	}
	err = s.queryRow(`SELECT COUNT(*) AS n FROM entries e
           WHERE e.slot = ? AND e.category != 'correction' AND NOT EXISTS (
             SELECT 1 FROM call_entries h WHERE h.entry = e.id AND h.role = 'found'
           )`, key).Scan(&never)
	return Usage{Searches: int(searches), EmptySearches: int(empty), NeverFound: int(never)}, err
}

// LoggedSince is the ISO timestamp of the first logged call; "" before any.
func (s *Store) LoggedSince() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var at sql.NullString
	err := s.queryRow("SELECT MIN(at) AS at FROM calls").Scan(&at)
	return at.String, err
}

// RecordWrite notes that a Session wrote to Memory, so the fallback
// extraction leaves it alone.
func (s *Store) RecordWrite(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.exec("INSERT OR REPLACE INTO session_writes (session_id, at) VALUES (?, ?)", sessionID, isoNow())
	return err
}

// HasWrite says whether a Session wrote to Memory.
func (s *Store) HasWrite(sessionID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var one int
	err := s.queryRow("SELECT 1 FROM session_writes WHERE session_id = ?", sessionID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
