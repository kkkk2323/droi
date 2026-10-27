// The Memory folder (ADR 0031). Droid reads it in a Session through the
// Memory Server; the Host writes it between Sessions. Only the Host writes
// the Memory Summary, the Session Summaries and state.json; the hook and the
// Memory Server each add files of their own (a started Session, a read, a
// Memory Note), so no file has two writers.

package memory

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// SummaryFile is the Memory Summary, which every Session starts with.
	SummaryFile = "memory_summary.md"
	// SessionSummariesDir holds one Session Summary per summarized Session.
	SessionSummariesDir = "session_summaries"
	// NotesDir holds the Memory Notes Droid writes when the user asks.
	NotesDir = "notes"
	// StartedDir holds a file per Session the SessionStart hook saw.
	StartedDir = "started"
	// ReadsFile is where the Memory Server logs reads of Session Summaries.
	ReadsFile = "reads.jsonl"
	// StateFile is the Host's bookkeeping.
	StateFile = "state.json"

	// SummaryMaxBytes bounds the Memory Summary, as Codex bounds its own.
	SummaryMaxBytes = 10_000
	// SessionSummaryMaxBytes bounds one Session Summary.
	SessionSummaryMaxBytes = 9_000
	// NoteMaxChars bounds one Memory Note.
	NoteMaxChars = 4_000
)

// SummaryHeadings are the sections every Memory Summary has, in order,
// after its first line SummaryVersion.
var SummaryHeadings = []string{"## User Profile", "## User preferences", "## General Tips", "## What's in Memory"}

// SummaryVersion is the Memory Summary's first line.
const SummaryVersion = "v1"

// CheckSummary says why text is no valid Memory Summary, "" when it is.
func CheckSummary(text string) string {
	if reason := CheckSummaryShape(text); reason != "" {
		return reason
	}
	if len(text) >= SummaryMaxBytes {
		return fmt.Sprintf("The Memory Summary is %d bytes; it must stay under %d.", len(text), SummaryMaxBytes)
	}
	return ""
}

// CheckSummaryShape is CheckSummary without the size: the version line and
// the headings.
func CheckSummaryShape(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != SummaryVersion {
		return "The Memory Summary must start with the line " + SummaryVersion + "."
	}
	for _, h := range SummaryHeadings {
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.TrimSpace(l) == h }) {
			return "The Memory Summary has no " + h + " heading."
		}
	}
	return ""
}

// ReadSummary is the Memory Summary, "" before the first.
func ReadSummary(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, SummaryFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// WriteFileAtomic writes a file whole, so a reader never sees half of it.
func WriteFileAtomic(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return err
	}
	tmp := file + ".tmp" + randomSuffix()
	if err := os.WriteFile(tmp, data, 0o666); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Hash is the SHA-256 Phase2Record keeps of an input.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// File is one file of the Memory folder that Droid may read.
type File struct {
	// Path is relative to the Memory folder, with forward slashes.
	Path    string
	Size    int64
	ModTime time.Time
}

// Files are the Memory Summary, the Session Summaries and the Memory Notes,
// in that order, each folder by name.
func Files(dir string) ([]File, error) {
	var out []File
	if info, err := os.Lstat(filepath.Join(dir, SummaryFile)); err == nil && info.Mode().IsRegular() {
		out = append(out, File{SummaryFile, info.Size(), info.ModTime()})
	}
	for _, sub := range []string{SessionSummariesDir, NotesDir} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, File{sub + "/" + e.Name(), info.Size(), info.ModTime()})
		}
	}
	return out, nil
}

// Resolve is the file a path Droid gave names, and its path in the folder,
// refusing anything outside the Memory Summary, the Session Summaries and
// the Memory Notes.
func Resolve(dir, rel string) (file, clean string, err error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, `\`, "/"))
	clean = path.Clean(rel)
	refused := errors.New("Memory has no file " + rel + ". Name the Memory Summary (" + SummaryFile + "), a Session Summary (" + SessionSummariesDir + "/...) or a Memory Note (" + NotesDir + "/...), as memory_list shows them.")
	if rel == "" || path.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", "", refused
	}
	dirName, name := path.Split(clean)
	if clean != SummaryFile && (dirName != SessionSummariesDir+"/" && dirName != NotesDir+"/" || !strings.HasSuffix(name, ".md")) {
		return "", "", refused
	}
	file = filepath.Join(dir, filepath.FromSlash(clean))
	info, statErr := os.Lstat(file)
	if statErr != nil || !info.Mode().IsRegular() {
		return "", "", refused
	}
	return file, clean, nil
}

// Header is the "key: value" lines that open a Session Summary or a Memory
// Note, up to the first blank line.
func Header(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.ContainsAny(k, " \t") {
			break
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}

// Started is what the SessionStart hook records of a Session: the Sessions
// Droi summarizes are the ones its own Daemon ran with Memory on.
type Started struct {
	SessionID  string `json:"sessionId"`
	Transcript string `json:"transcriptPath"`
	Cwd        string `json:"cwd"`
	At         string `json:"at"`
}

// RecordStart keeps a Session the hook saw; a later SessionStart of the same
// Session (after a compaction) leaves the first record.
func RecordStart(dir string, s Started) error {
	file := filepath.Join(dir, StartedDir, sessionFileName(s.SessionID))
	if _, err := os.Stat(file); err == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return WriteFileAtomic(file, b)
}

// ReadStarted are the Sessions the hook recorded.
func ReadStarted(dir string) ([]Started, error) {
	entries, err := os.ReadDir(filepath.Join(dir, StartedDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Started
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, StartedDir, e.Name()))
		if err != nil {
			continue
		}
		var s Started
		if json.Unmarshal(b, &s) == nil && s.SessionID != "" && s.Transcript != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// ForgetStarted drops a Session's record.
func ForgetStarted(dir, sessionID string) {
	_ = os.Remove(filepath.Join(dir, StartedDir, sessionFileName(sessionID)))
}

// sessionFileName is a Session's file name: a Daemon session id is a UUID,
// but nothing relies on that.
func sessionFileName(sessionID string) string {
	var b strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String() + ".json"
}

// Read is one read of a Session Summary.
type Read struct {
	File string `json:"file"`
	At   string `json:"at"`
}

// RecordRead logs a read; each Memory Server appends its own short lines.
func RecordRead(dir, rel string) error {
	b, err := json.Marshal(Read{File: rel, At: isoNow()})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, ReadsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// TakeReads moves the read log aside and answers the reads of every log
// moved aside at least settle ago, removing those. A Memory Server that
// opened the log just before the move writes into the moved file, which is
// read only once it has settled.
func TakeReads(dir string, now time.Time, settle time.Duration) ([]Read, error) {
	log := filepath.Join(dir, ReadsFile)
	if _, err := os.Stat(log); err == nil {
		if err := os.Rename(log, filepath.Join(dir, fmt.Sprintf("reads-%d.jsonl", now.UnixNano()))); err != nil {
			return nil, err
		}
	}
	moved, err := filepath.Glob(filepath.Join(dir, "reads-*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []Read
	for _, file := range moved {
		info, err := os.Stat(file)
		if err != nil || now.Sub(info.ModTime()) < settle {
			continue
		}
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var r Read
			if json.Unmarshal(scanner.Bytes(), &r) == nil && r.File != "" {
				out = append(out, r)
			}
		}
		f.Close()
		_ = os.Remove(file)
	}
	return out, nil
}

// Slug is a file-name-safe form of s: its lowercase ASCII words and
// numbers joined by hyphens, whole words within max bytes; "" when s has
// none.
func Slug(s string, max int) string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') })
	out := ""
	for _, w := range words {
		next := w
		if out != "" {
			next = out + "-" + w
		}
		if len(next) > max {
			if out == "" {
				out = w[:max]
			}
			break
		}
		out = next
	}
	return out
}

// TimeStamp is a time as Memory's file names begin: 2026-10-27T09-21-03.
func TimeStamp(t time.Time) string { return t.UTC().Format("2006-01-02T15-04-05") }

// WriteNote saves a Memory Note, never replacing one, and answers its path in
// the Memory folder. The Host folds every note into the Memory Summary.
func WriteNote(dir, text, sessionID string, now time.Time) (string, error) {
	text = jsTrim(text)
	if text == "" {
		return "", errors.New("The note is empty.")
	}
	if utf8.RuneCountInString(text) > NoteMaxChars {
		return "", fmt.Errorf("A note is at most %d characters; keep to what the user asked to remember, forget or correct.", NoteMaxChars)
	}
	if reason := FindSecret(text); reason != "" {
		return "", errors.New(reason)
	}
	slug := Slug(text, 40)
	if slug == "" {
		slug = "note"
	}
	body := "written_at: " + now.UTC().Format(time.RFC3339) + "\n"
	if sessionID != "" {
		body += "session_id: " + sessionID + "\n"
	}
	body += "\n" + text + "\n"
	if err := os.MkdirAll(filepath.Join(dir, NotesDir), 0o777); err != nil {
		return "", err
	}
	base := TimeStamp(now) + "-" + slug
	for i := 1; ; i++ {
		name := base + ".md"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.md", base, i)
		}
		f, err := os.OpenFile(filepath.Join(dir, NotesDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.WriteString(body)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return NotesDir + "/" + name, err
	}
}
