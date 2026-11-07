package fakekernel_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// TestFakeKernelAnswersAScriptedTurn is the package's own proof: a real Kernel, a real conversation, a
// scripted reply, and the view push a client draws from. Every view test that migrates to the Kernel will do
// these four things.
func TestFakeKernelAnswersAScriptedTurn(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "hello there", Chunks: []string{"hi ", "there"}, Text: "hi there"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)

	reply := k.Ask(t, c, id, "hello there", "hi there", 30*time.Second)
	if strings.TrimSpace(reply) != "hi there" {
		t.Fatalf("reply = %q, want %q", reply, "hi there")
	}

	// The push a viewer draws from: a whole view of that conversation, holding the reply.
	push := k.WaitForView(t, c, func(push fakekernel.ViewPush) bool {
		if push.ID != id || !push.Whole || push.View == nil {
			return false
		}
		for _, entry := range push.View.Entries {
			if entry.Kind == "pi.assistant" {
				return true
			}
		}
		return false
	}, 30*time.Second)
	if push.View.Conversation.ID != id {
		t.Fatalf("view is for conversation %d, want %d", push.View.Conversation.ID, id)
	}

	// The model saw the prompt the Kernel built, which is what a test of prompt assembly reads.
	requests := k.Requests()
	if len(requests) == 0 {
		t.Fatal("the Kernel made no model request")
	}
	if prompt := requests[0].Prompt(); !strings.Contains(prompt, "hello there") {
		t.Fatalf("the first request's prompt = %q, want it to hold the message", prompt)
	}
	if k.Unmatched() != 0 {
		t.Fatalf("%d requests matched no step", k.Unmatched())
	}
}

// TestFakeKernelRunsAScriptedToolCall checks the other half of scripting: the model asks for a tool, the
// Kernel really runs it, and the result lands in the transcript. A view test of tool output needs this to be
// real rather than a fixture.
func TestFakeKernelRunsAScriptedToolCall(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "list files", Call: &fakekernel.Call{Name: "bash", Arguments: map[string]any{
			"command": "echo written-by-the-kernel",
		}}},
		// The follow-up request's last user message is still "list files": a tool result is a tool message,
		// not a user one, so the step that follows a tool call matches the same prompt.
		{Match: "list files", Text: "the command printed it"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)

	sub := k.Submit(t, c, id, "list files")
	entries := k.WaitForEntries(t, c, sub, func(entries []fakekernel.Entry) bool {
		found := false
		for _, entry := range entries {
			if entry.Kind == "pi.tool-result" {
				found = true
			}
		}
		return found
	}, 60*time.Second)
	var tool string
	for _, entry := range entries {
		if entry.Kind == "pi.tool-result" {
			tool = entry.Text
		}
	}
	if !strings.Contains(tool, "written-by-the-kernel") {
		t.Fatalf("the tool result = %q, want the command's output", tool)
	}
	k.WaitForReply(t, c, sub, "the command printed it", 30*time.Second)
}

// TestFakeKernelIsolatesTheEnvironment is the safety property: a test's Kernel — and every tool it runs —
// sees a HOME of its own, not the one the test process inherited. A read of $HOME must show the Kernel's, no
// key may be inherited, and the default working directory must not be the source tree.
func TestFakeKernelIsolatesTheEnvironment(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("FACTORY_API_KEY", "not-a-real-key")
	seen := t.TempDir()
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Call: &fakekernel.Call{Name: "bash", Arguments: map[string]any{
			"command": fmt.Sprintf(
				"printf '%%s' \"$HOME\" > %q; printf '%%s' \"${FACTORY_API_KEY:-none}\" > %q; pwd > %q",
				filepath.Join(seen, "home"), filepath.Join(seen, "key"), filepath.Join(seen, "cwd")),
		}}},
		{Text: "done"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "look around", "done", 60*time.Second)

	home, _ := os.ReadFile(filepath.Join(seen, "home"))
	key, _ := os.ReadFile(filepath.Join(seen, "key"))
	cwd, _ := os.ReadFile(filepath.Join(seen, "cwd"))
	if got := strings.TrimSpace(string(home)); got != k.HomeDir {
		t.Errorf("the tool's HOME = %q, want the Kernel's own %q", got, k.HomeDir)
	}
	if got := strings.TrimSpace(string(key)); got != "none" {
		t.Errorf("the tool saw FACTORY_API_KEY = %q; a test must not inherit a key", got)
	}
	if strings.Contains(string(cwd), "pkg/kernel/fakekernel") {
		t.Errorf("the tool ran in %q, which is the source tree", strings.TrimSpace(string(cwd)))
	}
}

// TestFakeKernelUnmatchedFailsTheTest proves the strict default: an unscripted prompt fails the test that
// started the Kernel, instead of falling through to a fallback and letting its assertions pass on nothing.
//
// It runs itself as a child process, because the failure it asserts on **is** a test failure.
func TestFakeKernelUnmatchedFailsTheTest(t *testing.T) {
	if os.Getenv("FAKEKERNEL_STRICT_CHILD") != "" {
		k := fakekernel.Start(t, fakekernel.Scenario{})
		c := k.Dial(t)
		id := k.CreateConversation(t, c)
		k.Submit(t, c, id, "a prompt no step describes")
		// Wait for the answer so the request has certainly happened, then return: the cleanup the package
		// registered is what must fail this test.
		time.Sleep(2 * time.Second)
		return
	}
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skip("no test binary to re-run")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFakeKernelUnmatchedFailsTheTest$", "-test.v")
	cmd.Env = append(os.Environ(), "FAKEKERNEL_STRICT_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("an unscripted prompt did not fail the test:\n%s", output)
	}
	if !strings.Contains(string(output), "matched the prompt") {
		t.Fatalf("the failure does not say what went unmatched:\n%s", output)
	}
}

// TestFakeKernelWaitCannotReuseThePreviousTurn is the other false pass: a wait for "the reply" used to match
// the previous turn's entry.
func TestFakeKernelWaitCannotReuseThePreviousTurn(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "first", Text: "shared reply"},
		{Match: "second", Text: "different reply", Delay: 100 * time.Millisecond},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "first", "shared reply", 30*time.Second)

	// The second turn's wait is bound to its own boundary, so the first turn's entry cannot answer it: asking
	// for the first turn's text would hang until the deadline. Getting the second reply, and not the first, is
	// the assertion.
	sub := k.Submit(t, c, id, "second")
	got := k.WaitForReply(t, c, sub, "different reply", 30*time.Second)
	if strings.Contains(got, "shared reply") {
		t.Fatalf("the second turn's wait returned the first turn's reply: %q", got)
	}
}
