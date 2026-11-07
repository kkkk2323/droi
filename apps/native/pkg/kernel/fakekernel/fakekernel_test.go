package fakekernel_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// The Kernel offers `bash` where a shell it trusts exists — Git Bash on Windows — and `powershell` everywhere
// on Windows. A test that names one tool and writes POSIX shell into it is a test that only passes on the
// machine it was written on, so both the tool and the syntax come from here.
func shellTool() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

// echoCommand is a command that prints text with whichever shell the platform offers.
func echoCommand(text string) string {
	if runtime.GOOS == "windows" {
		return "Write-Output '" + text + "'"
	}
	return "echo " + text
}

// writeEnvCommand writes $HOME, an environment variable and the working directory into files.
func writeEnvCommand(home, key, cwd string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(
			"Set-Content -NoNewline -Path '%s' -Value $env:USERPROFILE; "+
				"Set-Content -NoNewline -Path '%s' -Value $env:FACTORY_API_KEY; "+
				"Set-Content -NoNewline -Path '%s' -Value (Get-Location).Path",
			home, key, cwd,
		)
	}
	return fmt.Sprintf(
		"printf '%%s' \"$HOME\" > %q; printf '%%s' \"${FACTORY_API_KEY:-none}\" > %q; pwd > %q",
		home, key, cwd,
	)
}

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
		{Match: "run the tool", Call: &fakekernel.Call{Name: shellTool(), Arguments: map[string]any{
			"command": echoCommand("written-by-the-kernel"),
		}}},
		// The follow-up request's last user message is still "run the tool": a tool result is a tool
		// message, not a user one, so the step after a tool call matches the same prompt.
		{Match: "run the tool", Text: "the command printed it"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)

	sub := k.Submit(t, c, id, "run the tool")
	entries := k.WaitForEntries(t, c, sub, func(entries []fakekernel.Entry) bool {
		for _, entry := range entries {
			if entry.Kind == "pi.tool-result" {
				return true
			}
		}
		return false
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
// sees a HOME of its own, not the one the test process inherited. A read of it must show the Kernel's, no key
// may be inherited, and the default working directory must not be the source tree.
func TestFakeKernelIsolatesTheEnvironment(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	t.Setenv("FACTORY_API_KEY", "not-a-real-key")
	seen := t.TempDir()
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Call: &fakekernel.Call{Name: shellTool(), Arguments: map[string]any{
			"command": writeEnvCommand(
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
	if got := strings.TrimSpace(string(key)); got != "" && got != "none" {
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
	// os.Executable, not os.Args[0]: on Windows the runner passes a bare name, and exec.Command resolves a
	// name without a separator through PATH, where the test binary is not.
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no test binary to re-run: %v", err)
	}
	cmd := exec.Command(self, "-test.run=^TestFakeKernelUnmatchedFailsTheTest$", "-test.v")
	cmd.Env = append(os.Environ(), "FAKEKERNEL_STRICT_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("an unscripted prompt did not fail the test:\n%s", output)
	}
	if !strings.Contains(string(output), "matched the prompt") {
		t.Fatalf("the failure does not say what went unmatched:\n%s", output)
	}
}

// TestFakeKernelWaitCannotReuseThePreviousTurn is the false pass a turn boundary fixes: a wait for "the reply"
// used to match the previous turn's entry, including a reply still in flight when the next prompt arrived.
func TestFakeKernelWaitCannotReuseThePreviousTurn(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "first", Text: "shared reply", Delay: 400 * time.Millisecond},
		{Match: "second", Text: "different reply"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)

	// The second prompt arrives while the first reply is still streaming: exactly the case "entries above
	// the id I submitted at" gets wrong, because the first reply's entry lands after that id.
	first := k.Submit(t, c, id, "first question")
	second := k.Submit(t, c, id, "second question")

	// The second submission must be answered by its own reply, never by the one in flight.
	got := k.WaitForReply(t, c, second, "different reply", 60*time.Second)
	if strings.Contains(got, "shared reply") {
		t.Fatalf("the second turn's wait returned the first turn's reply: %q", got)
	}
	// And the first submission's own answer is the one that was in flight.
	k.WaitForReply(t, c, first, "shared reply", 60*time.Second)

	firstRecord := k.Record(t, c, first, 30*time.Second)
	secondRecord := k.Record(t, c, second, 30*time.Second)
	if firstRecord.Answer == secondRecord.Answer {
		t.Fatalf("both submissions name entry %d as their answer", firstRecord.Answer)
	}
	if secondRecord.Entry <= firstRecord.Answer {
		t.Fatalf("the second submission's entry %d is not after the first answer %d",
			secondRecord.Entry, firstRecord.Answer)
	}
}

// TestFakeKernelEntriesAfterIsOldestFirst checks the order a turn is read in: a viewer draws a tool call before
// its result and the reply after it, and the RPC's default page is newest first.
func TestFakeKernelEntriesAfterIsOldestFirst(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "run the tool", Call: &fakekernel.Call{Name: shellTool(), Arguments: map[string]any{
			"command": echoCommand("ordered"),
		}}},
		{Match: "run the tool", Text: "and then a reply"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	sub := k.Submit(t, c, id, "run the tool")
	k.WaitForReply(t, c, sub, "and then a reply", 60*time.Second)

	entries := k.EntriesAfter(t, c, sub, 30*time.Second)
	if len(entries) < 3 {
		t.Fatalf("the turn has %d entries, want the user's, the tool's and the reply: %+v", len(entries), entries)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].ID <= entries[i-1].ID {
			t.Fatalf("entries are not oldest first: %d then %d", entries[i-1].ID, entries[i].ID)
		}
	}
	if entries[0].Kind != "pi.user" {
		t.Fatalf("the turn starts with %q, not the user's own entry", entries[0].Kind)
	}
	last := entries[len(entries)-1]
	if last.Kind != "pi.assistant" || !strings.Contains(last.Text, "and then a reply") {
		t.Fatalf("the turn ends with %q / %q", last.Kind, last.Text)
	}
}

// TestFakeKernelQueuedSubmissionHasNoEntries is one half of a false pass the review found: a submission that
// has not been placed has no entry of its own, and turning that zero into "from the beginning" answered the
// whole previous transcript.
func TestFakeKernelQueuedSubmissionHasNoEntries(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "first", Text: "previous reply", Delay: 300 * time.Millisecond},
		{Match: "second", Text: "own reply"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	first := k.Submit(t, c, id, "first")
	for deadline := time.Now().Add(5 * time.Second); len(k.Requests()) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the model was never asked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	second := k.Submit(t, c, id, "second")
	if record := k.Record(t, c, second, 5*time.Second); record.Status != "queued" {
		t.Fatalf("the second submission is %q, want queued", record.Status)
	}
	if entries := k.EntriesAfter(t, c, second, 5*time.Second); len(entries) != 0 {
		t.Fatalf("a queued submission answered %d entries of the previous turn: %+v", len(entries), entries)
	}
	k.WaitForReply(t, c, second, "own reply", 30*time.Second)
	k.WaitForReply(t, c, first, "previous reply", 30*time.Second)
}

// TestFakeKernelSettledTurnStopsAtItsOwnAnswer is the other half: without an end boundary, a later
// submission's entries join the earlier turn, and a wait for the earlier turn is satisfied by the later reply.
func TestFakeKernelSettledTurnStopsAtItsOwnAnswer(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		{Match: "first", Text: "first answer"},
		{Match: "second", Text: "later unrelated answer"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	first := k.Submit(t, c, id, "first")
	k.WaitForReply(t, c, first, "first answer", 30*time.Second)
	second := k.Submit(t, c, id, "second")
	k.WaitForReply(t, c, second, "later unrelated answer", 30*time.Second)

	record := k.Record(t, c, first, 5*time.Second)
	entries := k.EntriesAfter(t, c, first, 5*time.Second)
	for _, entry := range entries {
		if entry.ID > record.Answer {
			t.Fatalf("the first turn includes entry %d, after its own answer %d", entry.ID, record.Answer)
		}
	}
	if len(entries) == 0 {
		t.Fatal("the first turn is empty")
	}
	// A wait on the first turn must not be satisfied by the second one's reply. Waits that cannot be
	// satisfied fail the test they are in, so this runs as a child whose failure is the assertion.
	if os.Getenv("FAKEKERNEL_ENTRIES_CHILD") != "" {
		k.WaitForEntries(t, c, first, func(entries []fakekernel.Entry) bool {
			for _, entry := range entries {
				if strings.Contains(entry.Text, "later unrelated answer") {
					return true
				}
			}
			return false
		}, 2*time.Second)
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no test binary to re-run: %v", err)
	}
	cmd := exec.Command(self, "-test.run=^TestFakeKernelSettledTurnStopsAtItsOwnAnswer$", "-test.v")
	cmd.Env = append(os.Environ(), "FAKEKERNEL_ENTRIES_CHILD=1", "FAKEKERNEL_STRICT_CHILD=")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a wait on the first turn accepted the second turn's reply:\n%s", output)
	}
	if !strings.Contains(string(output), "settled with") {
		t.Fatalf("the failure does not say the turn settled without a match:\n%s", output)
	}
}

// TestFakeKernelAbortedTurnIsTerminal is the last false pass: a submission that ended without an answer is
// final, so a wait on it must fail at once with the Kernel's reason — and its entries must stop where its own
// turn stopped, not run through the next turn's reply.
func TestFakeKernelAbortedTurnIsTerminal(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
		// The delay keeps the run in flight long enough to abort it, which is the state under test.
		{Match: "first", Text: "too late", Delay: 2 * time.Second},
		{Match: "second", Text: "second answer"},
	}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	slow := k.Submit(t, c, id, "first")
	for deadline := time.Now().Add(5 * time.Second); len(k.Requests()) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the model was never asked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.Abort(t.Context(), kernel.AbortParams{ID: id}); err != nil {
		t.Fatalf("conversation.abort: %v", err)
	}
	record := k.Record(t, c, slow, 30*time.Second)
	if record.Status != "unanswered" {
		t.Fatalf("the aborted submission is %q, want unanswered: %+v", record.Status, record)
	}
	if record.Reason == "" {
		t.Fatal("an unanswered submission has no reason")
	}
	// Its turn has a definite end: the entries it wrote, and nothing after them.
	entries := k.EntriesAfter(t, c, slow, 30*time.Second)
	for _, entry := range entries {
		if record.Last != 0 && entry.ID > record.Last {
			t.Fatalf("the aborted turn includes entry %d, after its own last %d", entry.ID, record.Last)
		}
	}
	// The later turn's reply is not part of it.
	second := k.Submit(t, c, id, "second")
	k.WaitForReply(t, c, second, "second answer", 30*time.Second)
	after := k.EntriesAfter(t, c, slow, 30*time.Second)
	for _, entry := range after {
		if strings.Contains(entry.Text, "second answer") {
			t.Fatalf("the aborted turn now holds the next turn's reply: %+v", entry)
		}
	}

	// And a wait on it fails immediately, saying why, rather than after its timeout.
	if os.Getenv("FAKEKERNEL_ABORTED_CHILD") != "" {
		started := time.Now()
		k.WaitForReply(t, c, slow, "anything", 10*time.Second)
		t.Fatalf("a wait on an aborted turn returned after %s instead of failing", time.Since(started))
	}
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no test binary to re-run: %v", err)
	}
	child := exec.Command(self, "-test.run=^TestFakeKernelAbortedTurnIsTerminal$", "-test.v")
	child.Env = append(os.Environ(), "FAKEKERNEL_ABORTED_CHILD=1")
	run, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("a wait on an aborted turn did not fail:\n%s", run)
	}
	if !strings.Contains(string(run), "was never answered") {
		t.Fatalf("the failure does not say the turn was unanswered:\n%s", run)
	}
}

// TestFakeKernelTerminalSubmissionRejectsBeforeMatching is the false pass that survived the previous round: the
// predicate ran before the status was checked, so an aborted submission "matched" on its own user entry.
//
// It runs itself as a child, because the failure it asserts on is a test failure.
func TestFakeKernelTerminalSubmissionRejectsBeforeMatching(t *testing.T) {
	if os.Getenv("FAKEKERNEL_TERMINAL_CHILD") != "" {
		k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{
			{Match: "first", Text: "too late", Delay: 3 * time.Second},
		}})
		c := k.Dial(t)
		id := k.CreateConversation(t, c)
		sub := k.Submit(t, c, id, "first")
		for deadline := time.Now().Add(5 * time.Second); len(k.Requests()) == 0; {
			if time.Now().After(deadline) {
				t.Fatal("the model was never asked")
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := c.Abort(t.Context(), kernel.AbortParams{ID: id}); err != nil {
			t.Fatalf("conversation.abort: %v", err)
		}
		// The user's own entry is in the turn, so this predicate finds a match — and must still not succeed.
		k.WaitForEntries(t, c, sub, func(entries []fakekernel.Entry) bool {
			for _, entry := range entries {
				if entry.Kind == "pi.user" {
					return true
				}
			}
			return false
		}, 5*time.Second)
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no test binary to re-run: %v", err)
	}
	child := exec.Command(self, "-test.run=^TestFakeKernelTerminalSubmissionRejectsBeforeMatching$", "-test.v")
	child.Env = append(os.Environ(), "FAKEKERNEL_TERMINAL_CHILD=1")
	run, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("an aborted submission matched a predicate before its status was read:\n%s", run)
	}
	if !strings.Contains(string(run), "was never answered") {
		t.Fatalf("the failure does not say the turn was unanswered:\n%s", run)
	}
}

// TestFakeKernelDoneSubmissionStillMatches shows the other side of the ordering: a settled turn with an answer
// still runs the predicate, so a test can assert on entries it already knows it has.
func TestFakeKernelDoneSubmissionStillMatches(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{Steps: []fakekernel.Step{{Text: "an answer"}}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	sub := k.Submit(t, c, id, "hello")
	entries := k.WaitForEntries(t, c, sub, func(entries []fakekernel.Entry) bool {
		for _, entry := range entries {
			if entry.Kind == "pi.assistant" {
				return true
			}
		}
		return false
	}, 30*time.Second)
	if len(entries) == 0 {
		t.Fatal("a done turn matched nothing")
	}
}
