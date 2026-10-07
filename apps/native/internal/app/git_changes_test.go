package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

func TestGitChangesButton(t *testing.T) {
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Deploy", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra: map[string]any{"git": map[string]any{"branch": "fix/login", "files": []map[string]any{
				{"path": "src/auth/login.ts", "status": "modified", "additions": 3, "deletions": 1},
				{"path": "README.md", "status": "deleted", "additions": 0, "deletions": 9},
			}}}},
		{Title: "Scratch", Cwd: "/Users/dev/notes", Messages: []fakedaemon.Message{{Role: "user", Text: "hello"}}},
	}}
	h := newHarness(t, sc, "")
	h.openSession("Deploy")
	const label = "Branch fix/login, 2 changed files"
	h.until("the git button", func() bool { _, ok := h.tt.Find(label); return ok })
	for _, want := range []string{"fix/login", "+3", "−10"} {
		if !h.hasText(want) {
			t.Errorf("no %q in the header: %q", want, h.tt.Texts())
		}
	}
	h.click(label)
	h.frame()
	for _, want := range []string{"2 uncommitted files", "login.ts", "src/auth/", "README.md"} {
		if !h.hasText(want) {
			t.Errorf("no %q in the changed files: %q", want, h.tt.Texts())
		}
	}
	// Listed is not enough: the rows must take room in the popover.
	for _, row := range []string{"src/auth/login.ts", "README.md"} {
		if r, ok := h.tt.Find(row); !ok || r.H < 20 {
			t.Errorf("row %q not shown: %+v", row, r)
		}
	}

	// Outside a Git repository there is no button.
	h.click("Scratch")
	h.settle()
	if h.hasText("fix/login") || h.hasText("uncommitted") {
		t.Errorf("git button outside a repository: %q", h.tt.Texts())
	}
}
