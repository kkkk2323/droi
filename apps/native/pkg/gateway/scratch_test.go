package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
	"time"
)

type scratchFixture struct {
	home    string
	root    string
	trashed []string
	folders *ScratchFolders
}

func newScratchFixture(t *testing.T) *scratchFixture {
	f := &scratchFixture{home: t.TempDir()}
	f.root = filepath.Join(f.home, ".droi", "chats")
	f.folders = &ScratchFolders{
		Root: func() string { return f.root },
		MoveToTrash: func(path string) error {
			f.trashed = append(f.trashed, path)
			return os.RemoveAll(path)
		},
		Now: func() time.Time { return time.Date(2026, 9, 26, 23, 30, 0, 0, time.Local) },
	}
	return f
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestScratchCreateMakesADatedFolderUnderTheScratchFolderCreatingTheRoot(t *testing.T) {
	f := newScratchFixture(t)
	first, err1 := f.folders.Create()
	second, err2 := f.folders.Create()
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if filepath.Dir(first) != f.root {
		t.Fatal(first)
	}
	if !regexp.MustCompile(`^2026-09-26-[0-9a-f]{6}$`).MatchString(filepath.Base(first)) {
		t.Fatal(first)
	}
	if second == first || !exists(first) || !exists(second) {
		t.Fatal(first, second)
	}
}

func TestScratchCreateFollowsAChangedScratchFolder(t *testing.T) {
	f := newScratchFixture(t)
	f.root = filepath.Join(f.home, "elsewhere")
	path, err := f.folders.Create()
	if err != nil || filepath.Dir(path) != f.root {
		t.Fatal(path, err)
	}
}

func TestScratchTrashMovesAFolderWithAnythingInItToTheTrash(t *testing.T) {
	f := newScratchFixture(t)
	path, _ := f.folders.Create()
	_ = os.WriteFile(filepath.Join(path, ".DS_Store"), nil, 0o644)
	if err := f.folders.Trash(path); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.trashed, []string{path}) {
		t.Fatal(f.trashed)
	}
	// A folder already gone is fine.
	if err := f.folders.Trash(path); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.trashed, []string{path}) {
		t.Fatal(f.trashed)
	}
}

func TestScratchTrashSimplyRemovesAnEmptyFolder(t *testing.T) {
	f := newScratchFixture(t)
	path, _ := f.folders.Create()
	if err := f.folders.Trash(path); err != nil {
		t.Fatal(err)
	}
	if exists(path) || len(f.trashed) != 0 {
		t.Fatal(f.trashed)
	}
}

func TestScratchRestoreRecreatesAMissingFolderAndLeavesAnExistingOneAlone(t *testing.T) {
	f := newScratchFixture(t)
	path, _ := f.folders.Create()
	_ = os.WriteFile(filepath.Join(path, "notes.md"), []byte("x"), 0o644)
	if err := f.folders.Restore(path); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(path, "notes.md")) {
		t.Fatal("restore touched an existing folder")
	}
	_ = f.folders.Trash(path)
	if err := f.folders.Restore(path); err != nil || !exists(path) {
		t.Fatal(err)
	}
}

func TestScratchRefusesAnyPathThatIsNotAScratchWorkspace(t *testing.T) {
	f := newScratchFixture(t)
	project := filepath.Join(f.home, "projects", "app")
	_ = os.MkdirAll(project, 0o755)
	made, _ := f.folders.Create()
	refused := []string{
		project,
		f.root,
		filepath.Join(f.root, "notes"),
		filepath.Join(made, "nested"),
		f.root + "/../" + filepath.Base(made),
		made + "/../../../projects/app",
		"relative/2026-09-26-abcdef",
	}
	for _, path := range refused {
		for name, call := range map[string]func(string) error{"trash": f.folders.Trash, "restore": f.folders.Restore} {
			var refusal *NotAScratchWorkspace
			err := call(path)
			if !errors.As(err, &refusal) || err.Error() != path+" is not a Scratch Workspace" {
				t.Errorf("%s %s: %v", name, path, err)
			}
		}
	}
	if len(f.trashed) != 0 || !exists(project) {
		t.Fatal(f.trashed)
	}
}
