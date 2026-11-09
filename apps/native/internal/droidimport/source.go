package droidimport

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/internal/memory"
)

// firstLine bounds the session_start line.
const firstLine = 1 << 20

// Source is one droid session on disk that has a conversation the user may import.
type Source struct {
	ID      string
	Path    string
	Title   string
	Cwd     string
	Size    int64
	ModTime time.Time
}

// List is the sessions in dir, droid's sessions folder, newest first. A subagent's session and a Memory
// Session are left out: the first belongs to the session that started it, the second is Droi's own bookkeeping.
// So is a session with nothing said in it, which droid leaves behind for every window opened and closed. A folder
// that is not there lists nothing.
func List(dir string) ([]Source, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []Source
	for _, file := range files {
		src, ok := source(file)
		if ok {
			out = append(out, src)
		}
	}
	slices.SortFunc(out, func(a, b Source) int { return b.ModTime.Compare(a.ModTime) })
	return out, nil
}

func source(file string) (Source, bool) {
	f, err := os.Open(file)
	if err != nil {
		return Source{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Source{}, false
	}
	r := bufio.NewReaderSize(f, firstLine)
	line, err := r.ReadSlice('\n')
	if err != nil && err != io.EOF {
		return Source{}, false
	}
	var start struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Title   string `json:"title"`
		Cwd     string `json:"cwd"`
		Calling string `json:"callingSessionId"`
	}
	if json.Unmarshal(line, &start) != nil || start.Type != "session_start" || start.ID == "" || start.Calling != "" {
		return Source{}, false
	}
	if slices.Contains(memory.SessionTags(file), memory.MemorySessionTag) || !saysAnything(r) {
		return Source{}, false
	}
	return Source{ID: start.ID, Path: file, Title: start.Title, Cwd: start.Cwd, Size: info.Size(), ModTime: info.ModTime()}, true
}

// saysAnything reads the transcript up to its first message that would come over.
func saysAnything(r io.Reader) bool {
	c := newConverter()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		c.line(sc.Bytes())
		if c.out.Messages > 0 {
			return true
		}
	}
	return false
}
