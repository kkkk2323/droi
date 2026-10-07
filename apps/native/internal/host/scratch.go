package host

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Scratch Workspaces (ADR 0008): folders made under the Scratch folder for
// Sessions started without a Workspace. Only a folder that could have been
// made here, directly inside the Scratch folder, is ever touched.
type Scratch struct {
	// Root is read on every call: the Scratch folder is a setting.
	Root func() string
	// MoveToTrash moves a folder to the Trash.
	MoveToTrash func(path string) error
	Now         func() time.Time
}

var scratchName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-[0-9a-f]{6}$`)

// ErrNotScratch is a path that is not a Scratch Workspace.
var ErrNotScratch = errors.New("not a Scratch Workspace")

// Create makes a new, empty folder.
func (s *Scratch) Create() (string, error) {
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
		p := filepath.Join(root, now().Format("2006-01-02")+"-"+hex.EncodeToString(b))
		err := os.Mkdir(p, 0o755)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
}

func (s *Scratch) checked(path string) (string, error) {
	root := s.Root()
	if !filepath.IsAbs(path) || !scratchName.MatchString(filepath.Base(path)) {
		return "", fmt.Errorf("%s: %w", path, ErrNotScratch)
	}
	base, err1 := filepath.EvalSymlinks(root)
	parent, err2 := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(path)))
	if err1 != nil || err2 != nil || base != parent {
		return "", fmt.Errorf("%s: %w", path, ErrNotScratch)
	}
	return filepath.Join(root, filepath.Base(path)), nil
}

// Trash moves the folder to the Trash, or removes it when empty. A folder
// already gone is left at that.
func (s *Scratch) Trash(path string) error {
	p, err := s.checked(path)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil
	}
	if len(entries) == 0 {
		return os.Remove(p)
	}
	return s.MoveToTrash(p)
}

// Restore recreates the folder when it is gone.
func (s *Scratch) Restore(path string) error {
	p, err := s.checked(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o755)
}
