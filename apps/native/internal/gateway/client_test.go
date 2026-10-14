package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func startClientGateway(t *testing.T, with func(*Options)) (*Gateway, *Client) {
	t.Helper()
	opts := baseOptions(nil)
	if with != nil {
		with(&opts)
	}
	g := startGateway(t, opts)
	c, err := NewClient(g.DaemonSocketURL(localToken))
	if err != nil {
		t.Fatal(err)
	}
	return g, c
}

func TestAClientIsMadeFromTheDaemonSocketURLItAlreadyHas(t *testing.T) {
	for _, bad := range []string{"", "not a url", "ws://127.0.0.1:1/daemon", "http://127.0.0.1:1/daemon?token=x"} {
		if _, err := NewClient(bad); err == nil {
			t.Errorf("%q made a Client", bad)
		}
	}
}

func TestAClientCreatesAScratchWorkspaceInTheGatewaysScratchFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chats")
	_, c := startClientGateway(t, func(o *Options) {
		o.Scratch = &ScratchFolders{Root: func() string { return root }}
	})
	path, err := c.CreateScratch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != root || !scratchName.MatchString(filepath.Base(path)) {
		t.Fatalf("made %s, want a folder in %s", path, root)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("the folder is not there: %v", err)
	}
}

func TestAClientTrashesAndRestoresAScratchWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chats")
	var trashed []string
	_, c := startClientGateway(t, func(o *Options) {
		o.Scratch = &ScratchFolders{Root: func() string { return root }, MoveToTrash: func(path string) error {
			trashed = append(trashed, path)
			return os.RemoveAll(path)
		}}
	})
	path, err := c.CreateScratch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.TrashScratch(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 || trashed[0] != path || exists(path) {
		t.Fatalf("trashed %v, folder there: %v", trashed, exists(path))
	}
	if err := c.RestoreScratch(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if !exists(path) {
		t.Fatal("the folder was not restored")
	}
}

func TestAClientIsToldWhenTheGatewayWillNotTouchAFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chats")
	_, c := startClientGateway(t, func(o *Options) {
		o.Scratch = &ScratchFolders{Root: func() string { return root }}
	})
	var status *StatusError
	for name, call := range map[string]func() error{
		"trash":   func() error { return c.TrashScratch(t.Context(), "/Users/dev/acme-web") },
		"restore": func() error { return c.RestoreScratch(t.Context(), "/Users/dev/acme-web") },
	} {
		if err := call(); !errors.As(err, &status) || status.Status != 400 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAClientToldThereIsNoScratchFolderGetsErrNotFound(t *testing.T) {
	_, c := startClientGateway(t, nil)
	if _, err := c.CreateScratch(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestAClientCarriesTheGatewaysReasonWhenTheFolderCannotBeMade(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, c := startClientGateway(t, func(o *Options) {
		o.Scratch = &ScratchFolders{Root: func() string { return filepath.Join(blocker, "chats") }}
	})
	_, err := c.CreateScratch(t.Context())
	var status *StatusError
	if !errors.As(err, &status) || status.Status != 500 || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("%v", err)
	}
}

func TestAClientAsksWhereTheDaemonKeepsASessionsTranscript(t *testing.T) {
	_, c := startClientGateway(t, func(o *Options) {
		o.FindSessionFile = func(id string) string {
			if id == "a b&c" {
				return "/home/dev/.factory/sessions/-w/a b&c.jsonl"
			}
			return ""
		}
	})
	got, err := c.SessionFile(t.Context(), "a b&c")
	if err != nil || got != "/home/dev/.factory/sessions/-w/a b&c.jsonl" {
		t.Fatalf("%q %v", got, err)
	}
	if got, err := c.SessionFile(t.Context(), "other"); err != nil || got != "" {
		t.Fatalf("a Session without a transcript: %q %v", got, err)
	}
}

func TestAClientWithAWrongTokenIsRefused(t *testing.T) {
	g, _ := startClientGateway(t, nil)
	c, err := NewClient(g.DaemonSocketURL("wrong"))
	if err != nil {
		t.Fatal(err)
	}
	var status *StatusError
	if _, err := c.SessionFile(t.Context(), "x"); !errors.As(err, &status) || status.Status != 401 {
		t.Fatalf("%v", err)
	}
}

func TestAClientToAGatewayThatIsGoneFailsInsteadOfWaiting(t *testing.T) {
	g, c := startClientGateway(t, nil)
	_ = g.Close()
	if _, err := c.CreateScratch(t.Context()); err == nil {
		t.Fatal("no error")
	}
}
