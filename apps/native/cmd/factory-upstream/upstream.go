package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The upstream files under packages/factory that Droi follows: index.mjs function by function, the rest whole.
const (
	pkgDir    = "packages/factory"
	indexFile = "index.mjs"
)

var wholeFiles = []string{"anthropic-tools.mjs", "client-phrases.json"}

// Report is what changed upstream between the commit Droi ported and To.
type Report struct {
	From, To string
	Commits  []string
	// Changed is each ported name of index.mjs whose declaration changed, with its diff.
	Changed []Change
	// Missing names a ported name index.mjs declared at From and no longer declares at To.
	Missing []string
	// Undeclared names a ported name that is no top-level declaration at either commit: a method such as send, or
	// a field. A change to it shows only in Elsewhere.
	Undeclared []string
	// Unchanged counts the ported names whose declaration is the same.
	Unchanged int
	// Whole is the diff of each file Droi follows whole, when it changed.
	Whole []Change
	// Changelog is the lines upstream's CHANGELOG.md gained.
	Changelog []string
	// Elsewhere counts the changed lines of index.mjs outside every ported declaration.
	Elsewhere int
}

// Change is one ported part's diff.
type Change struct {
	Name, Diff string
}

// Action is "none" when nothing Droi ports changed, else "review".
func (r Report) Action() string {
	if len(r.Changed) == 0 && len(r.Missing) == 0 && len(r.Whole) == 0 {
		return "none"
	}
	return "review"
}

// Compare reads the commit and the ported names from ported, and compares that commit of repo with to.
func Compare(ported, repo, to string) (Report, error) {
	from, names, err := readPorted(ported)
	if err != nil {
		return Report{}, err
	}
	to, err = git(repo, "rev-parse", "--verify", to+"^{commit}")
	if err != nil {
		return Report{}, err
	}
	r := Report{From: from, To: to}
	if strings.HasPrefix(to, from) {
		return r, nil
	}
	if _, err := git(repo, "rev-parse", "--verify", from+"^{commit}"); err != nil {
		return Report{}, fmt.Errorf("the ported commit %s is not in %s: %w", short(from), repo, err)
	}
	if log, err := git(repo, "log", "--format=%h %s", from+".."+to, "--", pkgDir); err == nil && log != "" {
		r.Commits = strings.Split(log, "\n")
	}
	oldIndex, _ := show(repo, from, indexFile)
	newIndex, _ := show(repo, to, indexFile)
	oldLines, newLines := lines(oldIndex), lines(newIndex)
	var oldCovered, newCovered []span
	for _, name := range names {
		o, okOld := declaration(oldLines, name)
		n, okNew := declaration(newLines, name)
		if okOld {
			oldCovered = append(oldCovered, o)
		}
		if !okNew {
			if okOld {
				r.Missing = append(r.Missing, name)
			} else {
				r.Undeclared = append(r.Undeclared, name)
			}
			continue
		}
		newCovered = append(newCovered, n)
		before := ""
		if okOld {
			before = o.text(oldLines)
		}
		after := n.text(newLines)
		if before == after {
			r.Unchanged++
			continue
		}
		d, err := diffText(name, before, after)
		if err != nil {
			return Report{}, err
		}
		r.Changed = append(r.Changed, Change{Name: name, Diff: d})
	}
	r.Elsewhere = changedOutside(oldLines, newLines, oldCovered, newCovered)
	for _, f := range wholeFiles {
		d, err := git(repo, "diff", "--no-color", from, to, "--", pkgDir+"/"+f)
		if err != nil {
			return Report{}, err
		}
		if d != "" {
			r.Whole = append(r.Whole, Change{Name: f, Diff: d})
		}
	}
	if d, err := git(repo, "diff", "--no-color", "-U0", from, to, "--", pkgDir+"/CHANGELOG.md"); err == nil {
		for _, l := range strings.Split(d, "\n") {
			if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
				r.Changelog = append(r.Changelog, l[1:])
			}
		}
	}
	return r, nil
}

// Markdown is the report as the Automation leaves it.
func (r Report) Markdown(now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Factory plugin upstream: %s → %s (%s)\n\n", short(r.From), short(r.To), now.Format("2006-01-02"))
	switch {
	case strings.HasPrefix(r.To, r.From):
		b.WriteString("Action: none. Droi is ported from upstream's latest commit.\n")
		return b.String()
	case r.Action() == "none":
		fmt.Fprintf(&b, "Action: none. Nothing Droi ports changed (%d ported names unchanged). Move PORTED-FROM.md's commit to %s.\n", r.Unchanged, r.To)
	default:
		fmt.Fprintf(&b, "Action: review. %d ported names changed, %d are gone, %d files Droi follows whole changed; %d unchanged.\n",
			len(r.Changed), len(r.Missing), len(r.Whole), r.Unchanged)
	}
	if len(r.Commits) > 0 {
		b.WriteString("\n## Commits to packages/factory\n\n")
		for _, c := range r.Commits {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	if len(r.Changed) > 0 {
		b.WriteString("\n## Ported code that changed (index.mjs)\n")
		for _, c := range r.Changed {
			fmt.Fprintf(&b, "\n### `%s`\n\n```diff\n%s\n```\n", c.Name, strings.TrimRight(c.Diff, "\n"))
		}
	}
	if len(r.Missing) > 0 {
		b.WriteString("\n## Ported names index.mjs no longer declares\n\n")
		for _, m := range r.Missing {
			fmt.Fprintf(&b, "- `%s`\n", m)
		}
	}
	if len(r.Whole) > 0 {
		b.WriteString("\n## Files Droi follows whole\n")
		for _, c := range r.Whole {
			fmt.Fprintf(&b, "\n### %s\n\n```diff\n%s\n```\n", c.Name, strings.TrimRight(c.Diff, "\n"))
		}
	}
	if len(r.Changelog) > 0 {
		b.WriteString("\n## CHANGELOG.md\n\n")
		for _, l := range r.Changelog {
			b.WriteString(l + "\n")
		}
	}
	fmt.Fprintf(&b, "\n## Left out\n\n%d changed lines of index.mjs are outside every ported declaration: comments, Claude Code's and OpenCode's parts, and the parts of a declaration Droi ports only some of.\n", r.Elsewhere)
	if len(r.Undeclared) > 0 {
		fmt.Fprintf(&b, "\nNot top-level declarations, so not compared one by one: %s.\n", "`"+strings.Join(r.Undeclared, "`, `")+"`")
	}
	return b.String()
}

var (
	commitLine = regexp.MustCompile(`(?m)^commit:\s+([0-9a-f]{7,40})\s*$`)
	ident      = regexp.MustCompile("`([A-Za-z_$][A-Za-z0-9_$]*)`")
)

// readPorted answers PORTED-FROM.md's commit and the upstream names its tables name: each identifier in backticks in
// a row's second column.
func readPorted(path string) (string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	m := commitLine.FindSubmatch(data)
	if m == nil {
		return "", nil, fmt.Errorf("%s names no commit", path)
	}
	var names []string
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		cells := strings.Split(sc.Text(), "|")
		if len(cells) < 4 || !strings.HasPrefix(strings.TrimSpace(sc.Text()), "|") {
			continue
		}
		for _, id := range ident.FindAllStringSubmatch(cells[2], -1) {
			if !slices.Contains(names, id[1]) {
				names = append(names, id[1])
			}
		}
	}
	if len(names) == 0 {
		return "", nil, fmt.Errorf("%s names nothing it ports", path)
	}
	return string(m[1]), names, nil
}

// span is a declaration's lines, [start, end).
type span struct{ start, end int }

func (s span) text(ls []string) string { return strings.Join(ls[s.start:s.end], "\n") }

var topLevel = regexp.MustCompile(`^(export\s+)?(default\s+)?(async\s+)?(function\*?|const|let|var|class)\s`)

// declaration finds name's top-level declaration in an ES module: from its line to the line before the next one,
// leaving out the comments and blank lines that lead into the next.
func declaration(ls []string, name string) (span, bool) {
	start := regexp.MustCompile(`^(export\s+)?(async\s+)?(function\*?\s+|const\s+|let\s+|var\s+|class\s+)` + regexp.QuoteMeta(name) + `\b`)
	for i, l := range ls {
		if !start.MatchString(l) {
			continue
		}
		end := len(ls)
		for j := i + 1; j < len(ls); j++ {
			if topLevel.MatchString(ls[j]) {
				end = j
				break
			}
		}
		for end > i+1 {
			t := strings.TrimSpace(ls[end-1])
			if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") {
				end--
				continue
			}
			break
		}
		return span{i, end}, true
	}
	return span{}, false
}

// changedOutside counts the lines that differ between old and new once every ported declaration is taken out.
func changedOutside(oldLines, newLines []string, oldCovered, newCovered []span) int {
	rest := func(ls []string, covered []span) []string {
		var out []string
		for i, l := range ls {
			in := false
			for _, s := range covered {
				if i >= s.start && i < s.end {
					in = true
					break
				}
			}
			if !in {
				out = append(out, l)
			}
		}
		return out
	}
	a, b := rest(oldLines, oldCovered), rest(newLines, newCovered)
	common := lcs(a, b)
	return len(a) - common + len(b) - common
}

// lcs is the length of the longest common subsequence of a and b's lines.
func lcs(a, b []string) int {
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for i := range a {
		for j := range b {
			if a[i] == b[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = max(cur[j], prev[j+1])
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// diffText is git's unified diff of two texts, under name.
func diffText(name, before, after string) (string, error) {
	dir, err := os.MkdirTemp("", "factory-upstream-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte(before+"\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(b, []byte(after+"\n"), 0o644); err != nil {
		return "", err
	}
	out, err := exec.Command("git", "diff", "--no-index", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", a, b).Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return "", err
	}
	// The temporary paths say nothing; the header names the declaration instead.
	text := string(out)
	if i := strings.Index(text, "\n@@"); i >= 0 {
		text = text[i+1:]
	}
	return "--- a/" + name + "\n+++ b/" + name + "\n" + text, nil
}

func show(repo, commit, file string) (string, error) {
	return git(repo, "show", commit+":"+pkgDir+"/"+file)
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// fetchHead clones url into repo when it is not there, fetches upstream's HEAD, and answers its commit.
func fetchHead(repo, url string) (string, error) {
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
			return "", err
		}
		if out, err := exec.Command("git", "clone", "--quiet", "--filter=blob:none", "--no-checkout", url, repo).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone %s: %v: %s", url, err, out)
		}
	}
	if _, err := git(repo, "fetch", "--quiet", "origin", "HEAD"); err != nil {
		return "", err
	}
	return git(repo, "rev-parse", "FETCH_HEAD")
}

func git(repo string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
