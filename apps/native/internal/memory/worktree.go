package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// mainCheckout is the same place in the main checkout when path is inside a
// linked git worktree, and path otherwise. A linked worktree's .git is a
// file naming its repository in the main checkout's .git (gitdir:), whose
// commondir leads back to that .git; a submodule's has no commondir. path
// is a real path.
func mainCheckout(path string) string {
	for dir := path; ; {
		dotGit := filepath.Join(dir, ".git")
		if info, err := os.Lstat(dotGit); err == nil {
			if !info.Mode().IsRegular() {
				return path
			}
			main, ok := worktreeMain(dir, dotGit)
			if !ok {
				return path
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return path
			}
			return filepath.Join(main, rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		dir = parent
	}
}

func worktreeMain(dir, dotGit string) (string, bool) {
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", false
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", false
	}
	gitdir = resolve(dir, gitdir)
	data, err = os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}
	common := resolve(gitdir, string(data))
	// A bare repository's worktrees have no main checkout to share.
	if filepath.Base(common) != ".git" {
		return "", false
	}
	main, err := filepath.EvalSymlinks(filepath.Dir(common))
	if err != nil {
		return "", false
	}
	return main, true
}

func resolve(base, path string) string {
	path = filepath.FromSlash(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}

// MergeWorktrees moves the Project Memory recorded under a linked worktree,
// before worktrees shared their main checkout's, into the main checkout's.
// A worktree that is gone can no longer be traced and stays as it is.
func (s *Store) MergeWorktrees() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.conn.QueryContext(context.Background(), "SELECT key, workspace FROM slots WHERE key != ? AND workspace IS NOT NULL", globalKey)
	if err != nil {
		return err
	}
	type move struct {
		from string
		to   Slot
	}
	var moves []move
	for r.Next() {
		var key, workspace string
		if err := r.Scan(&key, &workspace); err != nil {
			r.Close()
			return err
		}
		if main := mainCheckout(workspace); main != workspace {
			moves = append(moves, move{key, Slot{Scope: ScopeProject, Workspace: main}})
		}
	}
	r.Close()
	if err := r.Err(); err != nil {
		return err
	}
	for _, m := range moves {
		err := s.transaction(func() error {
			if err := s.ensureSlot(m.to); err != nil {
				return err
			}
			if _, err := s.exec("UPDATE entries SET slot = ? WHERE slot = ?", slotKey(m.to), m.from); err != nil {
				return err
			}
			_, err := s.exec("DELETE FROM slots WHERE key = ?", m.from)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
