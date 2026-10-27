package memory

import (
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

// WorkspaceOf is the Workspace a folder belongs to: the folder itself, or,
// inside a linked git worktree, the same place in the main checkout, so that
// a worktree's Sessions are summarized under their repository. A folder that
// is gone keeps its path.
func WorkspaceOf(cwd string) string {
	if cwd == "" {
		return ""
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return cwd
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return mainCheckout(real)
}
