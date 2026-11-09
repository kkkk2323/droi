package host

import (
	"path/filepath"
	"testing"
)

// A Kernel conversation's worktrees go in Droi's own folder, apart from Factory's ~/.factory/worktrees, unless
// Settings names a full path; a folder that is no full path would put them wherever Droi runs from.
func TestWorktreesGoInDroisOwnFolder(t *testing.T) {
	home := t.TempDir()
	h := New(Config{UserData: t.TempDir(), Home: home, Env: func(string) string { return "" }})
	want := filepath.Join(home, ".droi", "worktrees")
	if got := h.WorktreeRoot(); got != want || h.DefaultWorktreeRoot() != want {
		t.Fatalf("the worktree root is %s, want %s", got, want)
	}
	for dir, want := range map[string]string{"/tmp/trees": "/tmp/trees", "trees": want, "": want} {
		if err := h.Settings.Update(func(s *Settings) { s.WorktreeDirectory = OptString(dir) }); err != nil {
			t.Fatal(err)
		}
		if got := h.WorktreeRoot(); got != want {
			t.Errorf("Settings' %q: %s, want %s", dir, got, want)
		}
	}
}
