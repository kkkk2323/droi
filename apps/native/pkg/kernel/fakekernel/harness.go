package fakekernel

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// Harness is a scripted model and the directories a Kernel runs in, with no
// Kernel of its own.
//
// Start runs a Kernel under this package's own management; a test that wants a
// Supervisor to own the process — because it is restarting it, or watching it
// survive a crash — starts here instead and gives Spawn to
// KernelSupervisor.Spawn. Every launch Spawn makes is a new process on the same
// data directory and the same model, so the conversations stay and the port and
// token do not.
type Harness struct {
	// DataDir is the Kernel's --data: one database, kept across launches.
	DataDir string
	// HomeDir is the Kernel's HOME, and its tools'; never the user's.
	HomeDir string
	// Workspace is the default working directory for conversations and tools.
	Workspace string
	// UserDir is the Kernel's DROI_USER_DIR: this test's own, never the user's.
	UserDir string

	model *scriptedModel
	t     testing.TB
	bin   string
	// kernel is the bare Kernel these helpers talk through: they need the
	// workspace, the test and the model's log, and nothing about a process.
	kernel *Kernel
}

// Harness starts a scripted model and the directories for one Kernel, without
// starting the Kernel. It skips the test when no Kernel binary is available,
// unless DROI_FAKEKERNEL_REQUIRED is set, which makes that a failure.
func NewHarness(t testing.TB, sc Scenario) *Harness {
	t.Helper()
	bin := binary(t)
	if bin == "" {
		if os.Getenv(RequiredEnv) != "" {
			t.Fatalf("fakekernel: no Kernel binary and %s is set: run pnpm build:kernel, or set DROI_KERNEL_BIN", RequiredEnv)
		}
		t.Skip("no Kernel binary: set DROI_KERNEL_BIN or run pnpm build:kernel")
	}
	model := startScriptedModel(t, sc)
	workspace := t.TempDir()
	return &Harness{
		DataDir:   t.TempDir(),
		HomeDir:   t.TempDir(),
		Workspace: workspace,
		UserDir:   t.TempDir(),
		model:     model,
		t:         t,
		bin:       bin,
		kernel:    &Kernel{Workspace: workspace, model: model, logs: &lockedBuffer{}, t: t},
	}
}

// Spawn is the launch a Supervisor owns: KernelSupervisor.Spawn = h.Spawn.
//
// The Supervisor reads the handshake from the returned stdout itself, and gives
// each launch a fresh token, so this builds the command and hands back its pipe.
func (h *Harness) Spawn(port int, token string) (*exec.Cmd, io.ReadCloser, error) {
	cmd := exec.Command(h.bin, "--data", h.DataDir, "--port", strconv.Itoa(port), "--host", "127.0.0.1")
	// A whole temporary environment: the Kernel and every tool it runs see this
	// HOME and this PATH, and nothing else from the parent — no FACTORY_*, no
	// *_API_KEY.
	cmd.Env = append(isolatedEnv(h.HomeDir),
		kernel.TokenEnv+"="+token,
		kernel.UserDirEnv+"="+h.UserDir,
		"DROI_KERNEL_PROVIDERS="+providersJSON(h.model.baseURL),
		"FAKE_PROVIDER_KEY=fake-key",
	)
	// Started in the workspace, not in this package's source directory: a tool
	// that runs `pwd` or writes a relative path must land in a throwaway place.
	cmd.Dir = h.Workspace
	prepareProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

// ModelURL is the scripted model's base URL, for a test that wants to make a
// request of its own.
func (h *Harness) ModelURL() string { return h.model.baseURL }

// Skill writes a personal skill into UserDir, which is what the Kernel reads
// from DROI_USER_DIR.
func (h *Harness) Skill(name, body string) string {
	h.t.Helper()
	path := filepath.Join(h.UserDir, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatalf("fakekernel: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		h.t.Fatalf("fakekernel: %v", err)
	}
	return path
}

// CreateConversation makes a conversation on the fake model's agent and opens
// it on c, so that connection gets conversation.view pushes for it.
func (h *Harness) CreateConversation(t testing.TB, c *kernel.Client) kernel.ID {
	t.Helper()
	return h.kernel.CreateConversation(t, c)
}

// Submit sends one message on c and answers the turn to wait on.
func (h *Harness) Submit(t testing.TB, c *kernel.Client, id kernel.ID, text string) Submission {
	t.Helper()
	return h.kernel.Submit(t, c, id, text)
}

// WaitForReply waits until this submission's own answer holds text.
func (h *Harness) WaitForReply(t testing.TB, c *kernel.Client, sub Submission, text string, within time.Duration) string {
	t.Helper()
	return h.kernel.WaitForReply(t, c, sub, text, within)
}
