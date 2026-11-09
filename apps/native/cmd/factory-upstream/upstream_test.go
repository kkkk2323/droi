package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const portedFixture = "# Ported\n\n```text\ncommit:     %s\n```\n\n| Go | upstream (`index.mjs`, first line) | notes |\n| --- | --- | --- |\n| `Body` | `droidBody` 10, `send`'s path 20 | x |\n| `phrases.go` | `client-phrases.json`, `PHRASES` 5 | y |\n"

const indexV1 = `import x from 'x'

const PHRASES = ['a', 'b']

// The opening.
function droidBody(body) {
  return body
}

// Not Droi's.
function claudeCode(body) {
  return 1
}

export default {
  async send(req) {
    return droidBody(req)
  },
}
`

// fakeUpstream is a repository shaped as magpie-community/plugins: its first commit, and a second made by edit.
func fakeUpstream(t *testing.T, edit func(index string) string, phrases string) (repo, first, second string) {
	t.Helper()
	repo = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	put := func(name, body string) {
		t.Helper()
		path := filepath.Join(repo, "packages", "factory", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	put("index.mjs", indexV1)
	put("client-phrases.json", `["a","b"]`)
	put("CHANGELOG.md", "# 0.1.42\n")
	run("add", "-A")
	run("commit", "-q", "-m", "0.1.42")
	first = run("rev-parse", "HEAD")
	put("index.mjs", edit(indexV1))
	if phrases != "" {
		put("client-phrases.json", phrases)
	}
	put("CHANGELOG.md", "# 0.1.43\n- droidBody trims the body\n# 0.1.42\n")
	run("add", "-A")
	run("commit", "-q", "-m", "0.1.43: trim")
	return repo, first, run("rev-parse", "HEAD")
}

func portedFile(t *testing.T, commit string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "PORTED-FROM.md")
	if err := os.WriteFile(path, []byte(strings.Replace(portedFixture, "%s", commit, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A hand-made upstream commit that changes a ported function and a part Droi does not port: the report shows the
// ported function's diff and the commit, counts the rest, and asks for a review.
func TestTheReportShowsWhatChangedInThePortedCode(t *testing.T) {
	repo, first, second := fakeUpstream(t, func(s string) string {
		s = strings.Replace(s, "  return body\n}", "  return body.trim()\n}", 1)
		return strings.Replace(s, "  return 1\n", "  return 2\n", 1)
	}, `["a","b","c"]`)
	r, err := Compare(portedFile(t, first), repo, second)
	if err != nil {
		t.Fatal(err)
	}
	if r.Action() != "review" || len(r.Changed) != 1 || r.Changed[0].Name != "droidBody" {
		t.Fatalf("report %+v", r)
	}
	md := r.Markdown(time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"# Factory plugin upstream: " + first[:7] + " → " + second[:7] + " (2026-11-08)",
		"Action: review. 1 ported names changed, 0 are gone, 1 files Droi follows whole changed; 1 unchanged.",
		"- " + second[:7] + " 0.1.43: trim",
		"### `droidBody`",
		"-  return body\n+  return body.trim()",
		"### client-phrases.json",
		"- droidBody trims the body",
		"2 changed lines of index.mjs are outside every ported declaration",
		"Not top-level declarations, so not compared one by one: `send`.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "return 2") {
		t.Errorf("the report shows a part Droi does not port:\n%s", md)
	}
}

// A commit that changes only what Droi does not port needs nothing but moving PORTED-FROM.md's commit.
func TestAChangeElsewhereNeedsNoReview(t *testing.T) {
	repo, first, second := fakeUpstream(t, func(s string) string {
		return strings.Replace(s, "  return 1\n", "  return 2\n", 1)
	}, "")
	r, err := Compare(portedFile(t, first), repo, second)
	if err != nil {
		t.Fatal(err)
	}
	md := r.Markdown(time.Now())
	if r.Action() != "none" || !strings.Contains(md, "Action: none. Nothing Droi ports changed (2 ported names unchanged). Move PORTED-FROM.md's commit to "+second+".") {
		t.Fatalf("report:\n%s", md)
	}
}

// A ported function that upstream removed is named, and needs a review.
func TestARemovedPortedFunctionIsNamed(t *testing.T) {
	repo, first, second := fakeUpstream(t, func(s string) string {
		return strings.Replace(s, "const PHRASES = ['a', 'b']\n", "", 1)
	}, "")
	r, err := Compare(portedFile(t, first), repo, second)
	if err != nil {
		t.Fatal(err)
	}
	if r.Action() != "review" || len(r.Missing) != 1 || r.Missing[0] != "PHRASES" {
		t.Fatalf("report %+v", r)
	}
}

// Upstream at the ported commit is nothing to do.
func TestUpToDateIsNothingToDo(t *testing.T) {
	repo, first, _ := fakeUpstream(t, func(s string) string { return s + "\n" }, "")
	r, err := Compare(portedFile(t, first), repo, first)
	if err != nil {
		t.Fatal(err)
	}
	if md := r.Markdown(time.Now()); r.Action() != "none" || !strings.Contains(md, "Droi is ported from upstream's latest commit") {
		t.Fatalf("report:\n%s", md)
	}
}

// The repository's own PORTED-FROM.md parses: a commit, and the names its tables give.
func TestTheRepositorysPortedFromParses(t *testing.T) {
	commit, names, err := readPorted(filepath.Join("..", "..", "pkg", "llmproxy", "factory", "PORTED-FROM.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(commit) != 40 {
		t.Fatalf("commit %q", commit)
	}
	for _, want := range []string{"droidBody", "anthropicBody", "sanitizeBody", "fastBody"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("%s is not among the ported names %v", want, names)
		}
	}
}
