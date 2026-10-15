package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// scratchTrash is a Scratch folder on disk whose Trash is a list.
type scratchTrash struct {
	root string

	mu      sync.Mutex
	trashed []string
}

func newScratchTrash(t *testing.T) *scratchTrash {
	t.Helper()
	s := &scratchTrash{root: filepath.Join(t.TempDir(), "chats")}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		t.Fatal(err)
	}
	return s
}

// folder makes a Scratch Workspace folder holding a file, so the Gateway
// moves it to the Trash instead of removing it.
func (s *scratchTrash) folder(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(s.root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (s *scratchTrash) options(o *gateway.Options) {
	o.Scratch = &gateway.ScratchFolders{Root: func() string { return s.root }, MoveToTrash: func(path string) error {
		s.mu.Lock()
		s.trashed = append(s.trashed, path)
		s.mu.Unlock()
		return os.RemoveAll(path)
	}}
}

func folderExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *scratchTrash) moved() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.trashed...)
}

func scratchSession(title, id, cwd string, extra map[string]any) fakedaemon.SessionSpec {
	tags := []map[string]any{{"name": "droi.scratch"}}
	if parent, ok := extra["parent"].(string); ok {
		tags = append(tags, map[string]any{"name": "droi.continues", "metadata": map[string]string{"parent": parent}})
	}
	fields := map[string]any{"sessionId": id, "tags": tags}
	if at, ok := extra["archivedAt"]; ok {
		fields["archivedAt"] = at
	}
	return fakedaemon.SessionSpec{Title: title, Cwd: cwd, Messages: []fakedaemon.Message{{Role: "user", Text: title}}, Extra: fields}
}

func (h *harness) chooseFromMenu(title, item string) {
	h.t.Helper()
	h.until("the row", func() bool { return h.hasText(title) })
	if err := h.tt.RightClick(title); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
	if err := h.tt.ChooseMenuItem(item); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
}

func TestArchivingAScratchSessionMovesItsFolderToTheTrashAndUnarchivingRestoresIt(t *testing.T) {
	scratch := newScratchTrash(t)
	cwd := scratch.folder(t, "2026-10-14-0a1b2c")
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{scratchSession("Name the dog", "chat-1", cwd, nil)}}
	h := startHarnessGateway(t, sc, "", "", scratch.options, func(cfg *Config) { prefs.ShowArchived.Set(cfg.Prefs, true) })

	h.chooseFromMenu("Name the dog", "Archive")
	h.until("the folder in the Trash", func() bool { return len(scratch.moved()) == 1 })
	if got := scratch.moved()[0]; got != cwd {
		t.Fatalf("trashed %s, want %s", got, cwd)
	}
	if folderExists(cwd) {
		t.Fatal("the folder is still there")
	}

	h.chooseFromMenu("Name the dog", "Unarchive")
	h.until("the folder back", func() bool { return folderExists(cwd) })
	if _, err := h.d.WaitForRequest("daemon.unarchive_session", 1); err != nil {
		t.Fatal(err)
	}
}

func TestArchivingAScratchSessionKeepsTheFolderAnotherUnarchivedSessionStillUses(t *testing.T) {
	scratch := newScratchTrash(t)
	cwd := scratch.folder(t, "2026-10-14-0a1b2c")
	// Both continue "root" after a compaction, so each is listed on its own.
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		scratchSession("Name the dog", "root", cwd, nil),
		scratchSession("Name the cat", "cat", cwd, map[string]any{"parent": "root"}),
		scratchSession("Name the fish", "fish", cwd, map[string]any{"parent": "root"}),
	}}
	h := startHarnessGateway(t, sc, "", "", scratch.options, nil)

	h.chooseFromMenu("Name the cat", "Archive")
	h.until("the archive", func() bool { return h.count("daemon.archive_session") == 2 })
	h.until("the row gone", func() bool { return !h.hasText("Name the cat") })
	if moved := scratch.moved(); len(moved) != 0 || !folderExists(cwd) {
		t.Fatalf("the folder %s moved while \"Name the fish\" still uses it: %v", cwd, moved)
	}

	h.chooseFromMenu("Name the fish", "Archive")
	h.until("the folder in the Trash", func() bool { return len(scratch.moved()) == 1 })
}

func TestArchivingASessionInAWorkspaceTouchesNoFolder(t *testing.T) {
	scratch := newScratchTrash(t)
	h := startHarnessGateway(t, sessionsScenario(), "", "", scratch.options, nil)
	h.chooseFromMenu("Fix the login race", "Archive")
	h.until("the archive", func() bool { return h.count("daemon.archive_session") == 1 })
	h.settle()
	if moved := scratch.moved(); len(moved) != 0 {
		t.Fatalf("trashed %v", moved)
	}
}
