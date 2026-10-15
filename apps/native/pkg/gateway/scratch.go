package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// ScratchFolders makes Scratch Workspaces (ADR 0008): folders under the
// Scratch folder for Sessions started without a Workspace. Only a folder it
// could have made, directly inside the Scratch folder, is ever touched.
type ScratchFolders struct {
	// Root is read on every call: the Scratch folder is a setting.
	Root        func() string
	MoveToTrash func(path string) error
	// Now defaults to time.Now; the folder name carries its local date.
	Now func() time.Time
}

var scratchName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-[0-9a-f]{6}$`)

// NotAScratchWorkspace is a path the Gateway will not touch: anything but
// a Scratch Workspace.
type NotAScratchWorkspace struct{ Path string }

func (e *NotAScratchWorkspace) Error() string { return e.Path + " is not a Scratch Workspace" }

// Create makes a new, empty folder; its path is where the Session starts.
func (s *ScratchFolders) Create() (string, error) {
	root := s.Root()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	for {
		b := make([]byte, 3)
		_, _ = rand.Read(b)
		path := filepath.Join(root, now().Format("2006-01-02")+"-"+hex.EncodeToString(b))
		err := os.Mkdir(path, 0o755)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
}

// Trash moves the folder to the Trash, or removes it when empty (an
// abandoned Draft Session's, or a conversation that never wrote a file). A
// folder already gone is left at that.
func (s *ScratchFolders) Trash(path string) error {
	target, err := s.checked(path)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil
	}
	if len(entries) == 0 {
		return os.Remove(target)
	}
	return s.MoveToTrash(target)
}

// Restore recreates the folder when it is gone, so an unarchived Session
// opens again.
func (s *ScratchFolders) Restore(path string) error {
	target, err := s.checked(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(target, 0o755)
}

// checked resolves symlinks on both sides, so `..` or a link cannot lead out
// of the Scratch folder.
func (s *ScratchFolders) checked(path string) (string, error) {
	root := s.Root()
	name := filepath.Base(path)
	if !filepath.IsAbs(path) || !scratchName.MatchString(name) {
		return "", &NotAScratchWorkspace{Path: path}
	}
	base, err1 := filepath.EvalSymlinks(root)
	parent, err2 := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(path)))
	if err1 != nil || err2 != nil || base != parent {
		return "", &NotAScratchWorkspace{Path: path}
	}
	return filepath.Join(root, name), nil
}
