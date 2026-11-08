package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// The folders of the Gateway's computer, for a Client that reaches its Kernel: the Kernel runs commands but answers
// nothing about a folder before a conversation is made there, so the Host answers it, with git on this computer.
const (
	// WorkspacePath answers a GET with a Workspace for ?path=.
	WorkspacePath = "/workspace"
	// GitChangesPath answers a GET with the GitChanges of ?path=.
	GitChangesPath = "/git-changes"
	// WorktreePath makes a worktree on a POST: ?path= the folder, ?base= the branch it starts from ("" for HEAD),
	// ?prompt= the first message, which names it. It answers 201 with a WorktreeMade.
	WorktreePath = "/worktrees"

	WorkspaceBaseQuery   = "base"
	WorkspacePromptQuery = "prompt"
)

// Workspaces answers what a Client asks about this computer's folders.
type Workspaces interface {
	// Inspect is the folder at path, made absolute and real; a *NotAWorkspace when it is not one a Session can
	// work in.
	Inspect(ctx context.Context, path string) (Workspace, error)
	// Changes is what path's working tree changed; IsGitRepository false outside a repository.
	Changes(ctx context.Context, path string) (GitChanges, error)
	// AddWorktree makes a worktree of path's repository on a new branch; a *NotAWorkspace when path is not in one.
	AddWorktree(ctx context.Context, path, base, prompt string) (WorktreeMade, error)
}

// NotAWorkspace is a folder a Client asked about that cannot be one: the sentence says why, and answers 400.
type NotAWorkspace struct{ Reason string }

func (e *NotAWorkspace) Error() string { return e.Reason }

// Workspace is a folder and its repository's branches.
type Workspace struct {
	Path            string   `json:"path"`
	IsGitRepository bool     `json:"isGitRepository"`
	Branches        []string `json:"branches"`
	CurrentBranch   string   `json:"currentBranch"`
}

// GitChanges is a working tree against HEAD.
type GitChanges struct {
	IsGitRepository bool            `json:"isGitRepository"`
	Branch          string          `json:"branch"`
	Detached        bool            `json:"detached"`
	Files           []GitChangeFile `json:"files"`
	Additions       int             `json:"additions"`
	Deletions       int             `json:"deletions"`
}

// GitChangeFile is one changed file.
type GitChangeFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// WorktreeMade is a worktree the Host made: the folder a Session works in, its branch, and the repository it is of.
type WorktreeMade struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	RepoRoot string `json:"repoRoot"`
}

func (g *Gateway) answerWorkspaces(r *http.Request, endpoint string, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	ws := g.opts.Workspaces
	if ws == nil {
		return http.StatusNotFound, nil
	}
	want := http.MethodGet
	if endpoint == WorktreePath {
		want = http.MethodPost
	}
	if r.Method != want {
		return http.StatusMethodNotAllowed, nil
	}
	path := q.Get(ScratchPathQuery)
	if path == "" {
		return http.StatusBadRequest, errorBody{Error: "No folder given"}
	}
	var (
		body   any
		err    error
		status = http.StatusOK
	)
	switch endpoint {
	case WorkspacePath:
		var w Workspace
		if w, err = ws.Inspect(r.Context(), path); w.Branches == nil {
			w.Branches = []string{}
		}
		body = w
	case GitChangesPath:
		var c GitChanges
		if c, err = ws.Changes(r.Context(), path); c.Files == nil {
			c.Files = []GitChangeFile{}
		}
		body = c
	default:
		body, err = ws.AddWorktree(r.Context(), path, q.Get(WorkspaceBaseQuery), q.Get(WorkspacePromptQuery))
		status = http.StatusCreated
	}
	if err == nil {
		return status, body
	}
	var refused *NotAWorkspace
	if errors.As(err, &refused) {
		return http.StatusBadRequest, errorBody{Error: err.Error()}
	}
	return http.StatusInternalServerError, errorBody{Error: err.Error()}
}
