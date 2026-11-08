package gitutil

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// emptyTree is git's id of the tree with nothing in it: what a repository without a commit is compared with.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// untrackedLimit is the largest untracked file whose lines are counted; a bigger one counts as one change.
const untrackedLimit = 1 << 20

// Change is one file a working tree changed against HEAD, staged or not.
type Change struct {
	Path string
	// Status is "added", "modified", "deleted", "renamed", "copied", "untracked" or "conflicted".
	Status string
	// From is the path a renamed or copied file had.
	From                 string
	Additions, Deletions int
	// Binary files have no line counts.
	Binary bool
}

// Changes is a working tree against HEAD: its branch, every changed file, and the lines in all of them.
type Changes struct {
	Branch   string
	Detached bool
	Files    []Change
	// Additions and Deletions are the totals of Files.
	Additions, Deletions int
}

// Clean reports that nothing changed.
func (c Changes) Clean() bool { return len(c.Files) == 0 }

// Status reads dir's working tree. ok is false when dir is not in a git repository.
func Status(ctx context.Context, dir string) (c Changes, ok bool, err error) {
	if !IsRepo(ctx, dir) {
		return Changes{}, false, nil
	}
	out, err := run(ctx, Timeout, dir, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--renames")
	if err != nil {
		return Changes{}, true, err
	}
	head := ""
	records := bytes.Split(out, []byte{0})
	for i := 0; i < len(records); i++ {
		rec := string(records[i])
		switch {
		case strings.HasPrefix(rec, "# branch.oid "):
			head = strings.TrimPrefix(rec, "# branch.oid ")
		case strings.HasPrefix(rec, "# branch.head "):
			name := strings.TrimPrefix(rec, "# branch.head ")
			if name == "(detached)" {
				c.Detached = true
			} else {
				c.Branch = name
			}
		case strings.HasPrefix(rec, "1 "):
			// 1 XY sub mH mI mW hH hI path
			f := strings.SplitN(rec, " ", 9)
			if len(f) == 9 {
				c.Files = append(c.Files, Change{Path: f[8], Status: statusOf(f[1])})
			}
		case strings.HasPrefix(rec, "2 "):
			// 2 XY sub mH mI mW hH hI Xscore path, then the original path as the next record
			f := strings.SplitN(rec, " ", 10)
			if len(f) == 10 && i+1 < len(records) {
				i++
				c.Files = append(c.Files, Change{Path: f[9], Status: statusOf(f[1]), From: string(records[i])})
			}
		case strings.HasPrefix(rec, "u "):
			// u XY sub m1 m2 m3 mW h1 h2 h3 path
			f := strings.SplitN(rec, " ", 11)
			if len(f) == 11 {
				c.Files = append(c.Files, Change{Path: f[10], Status: "conflicted"})
			}
		case strings.HasPrefix(rec, "? "):
			c.Files = append(c.Files, Change{Path: strings.TrimPrefix(rec, "? "), Status: "untracked"})
		}
	}
	if len(c.Files) == 0 {
		return c, true, nil
	}
	base := "HEAD"
	if head == "(initial)" {
		base = emptyTree
	}
	counts, err := numstat(ctx, dir, base)
	if err != nil {
		return c, true, err
	}
	top, _ := TopLevel(ctx, dir)
	for i := range c.Files {
		f := &c.Files[i]
		if f.Status == "untracked" {
			f.Additions, f.Binary = countLines(filepath.Join(top, filepath.FromSlash(f.Path)))
		} else if n, ok := counts[f.Path]; ok {
			f.Additions, f.Deletions, f.Binary = n.add, n.del, n.binary
		}
		c.Additions += f.Additions
		c.Deletions += f.Deletions
	}
	sort.Slice(c.Files, func(a, b int) bool { return c.Files[a].Path < c.Files[b].Path })
	return c, true, nil
}

// statusOf names a porcelain XY pair: the index's change, else the working tree's.
func statusOf(xy string) string {
	for _, code := range xy {
		switch code {
		case 'A':
			return "added"
		case 'D':
			return "deleted"
		case 'R':
			return "renamed"
		case 'C':
			return "copied"
		case 'M', 'T':
			return "modified"
		}
	}
	return "modified"
}

type lineCount struct {
	add, del int
	binary   bool
}

// numstat counts the lines each tracked file changed against base, staged and not, by the path it has now.
func numstat(ctx context.Context, dir, base string) (map[string]lineCount, error) {
	out, err := run(ctx, Timeout, dir, "diff", "--numstat", "-z", "-M", base, "--")
	if err != nil {
		return nil, err
	}
	counts := map[string]lineCount{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(string(fields[i]), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) {
			// A rename: the counts, then the old path and the new one as records of their own.
			path = string(fields[i+2])
			i += 2
		}
		n := lineCount{binary: parts[0] == "-"}
		n.add, _ = strconv.Atoi(parts[0])
		n.del, _ = strconv.Atoi(parts[1])
		counts[path] = n
	}
	return counts, nil
}

// countLines is an untracked file's lines, as git would count them if it were added; a large or binary file is
// one change with no lines.
func countLines(path string) (int, bool) {
	// Not through a link: one to a pipe or to /dev/zero would never end. git counts a link as one changed line of
	// its target's name, which is no line of the user's.
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	if info.Size() > untrackedLimit {
		return 0, true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return 0, true
	}
	n := 0
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 64*1024), untrackedLimit+1)
	for s.Scan() {
		n++
	}
	return n, false
}
