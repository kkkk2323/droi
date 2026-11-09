package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// approvalHarness is a window on Kernel whose model, asked "Run it", runs each command in turn and then answers
// "It ran.". The Session defaults' level is the Fake Daemon's "low", at which a command asks.
func approvalHarness(t *testing.T, commands ...string) *harness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the commands are bash's")
	}
	scriptKernel(t, func(sc *fakekernel.Scenario) {
		var steps []fakekernel.Step
		for i, command := range commands {
			call := &fakekernel.Call{Name: "bash", ID: "call_" + strconv.Itoa(i), Arguments: map[string]any{"command": command}}
			steps = append(steps, fakekernel.Step{Match: "Run it", Call: call})
		}
		sc.Steps = append(append(steps, fakekernel.Step{Match: "Run it", Text: "It ran."}), sc.Steps...)
	})
	h := kernelHarness(t)
	// The scenario's folder is a made-up path, where a command could not start.
	conv, _ := h.kernel.id(h.d.Sessions[1].SessionID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := h.testConn().Configure(ctx, kernel.ConfigureParams{ID: conv, Change: &kernel.AgentChange{Cwd: kernel.Some(t.TempDir())}}); err != nil {
		t.Fatal(err)
	}
	h.openSession("Add dark mode toggle")
	return h
}

// A command the policy holds is a card in the composer's place, with the command on it, and the Session waits on
// the person in the list. Allow once runs it; the card goes and the turn ends with the model's answer.
func TestAKernelCommandWaitsOnItsCard(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	h := approvalHarness(t, "touch "+marker)
	id := h.d.Sessions[1].SessionID
	h.send("Run it")
	h.until("the approval card", func() bool { return h.hasText("Droi wants to run bash") })
	if !h.hasText("touch " + marker) {
		t.Errorf("the card does not show the command: %q", h.tt.Texts())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command ran before anyone answered")
	}
	h.a.mu.Lock()
	state := h.a.busy[id]
	h.a.mu.Unlock()
	if state != string(protocol.DroidWorkingStateWaitingForToolConfirmation) {
		t.Errorf("the list holds the Session as %q while it waits", state)
	}
	h.click("Allow once")
	h.until("the model's answer", func() bool { return h.hasText("It ran.") })
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the allowed command did not run: %v", err)
	}
	h.until("the card to go", func() bool { return !h.hasText("Droi wants to run bash") })
}

// A window that connects again while a card waits still shows it, marks the Session as waiting, and its answer
// runs the command.
func TestAReconnectedWindowStillShowsAWaitingCard(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	h := approvalHarness(t, "touch "+marker)
	id := h.d.Sessions[1].SessionID
	h.send("Run it")
	h.until("the approval card", func() bool { return h.hasText("Droi wants to run bash") })
	h.a.UseKernel(h.kernel.k.Dial(t), 2)
	waiting := string(protocol.DroidWorkingStateWaitingForToolConfirmation)
	h.until("the card and the mark on the new connection", func() bool {
		h.a.mu.Lock()
		state := h.a.busy[id]
		h.a.mu.Unlock()
		return state == waiting && h.hasText("Droi wants to run bash") && h.a.sessions.Generation() == 2
	})
	h.click("Allow once")
	h.until("the model's answer", func() bool { return h.hasText("It ran.") })
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the allowed command did not run: %v", err)
	}
}

// Cancel keeps the command from running, and the turn goes on: the model hears that the person said no.
func TestCancelOnAKernelCardKeepsTheCommandFromRunning(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	h := approvalHarness(t, "touch "+marker)
	h.send("Run it")
	h.until("the approval card", func() bool { return h.hasText("Droi wants to run bash") })
	h.click("Cancel")
	h.until("the model's answer", func() bool { return h.hasText("It ran.") })
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a cancelled command ran")
	}
}

// "Always allow bash" lets the next command through without a card, and is kept as the conversation's own rule:
// the window pushes it again to the Kernel it connects to next.
func TestAlwaysAllowOnAKernelCardIsKept(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	h := approvalHarness(t, "touch "+first, "touch "+second)
	conv, _ := h.kernel.id(h.d.Sessions[1].SessionID)
	h.send("Run it")
	h.until("the approval card", func() bool { return h.hasText("Droi wants to run bash") })
	h.click("Always allow bash")
	// A second card would hold the turn, and the answer would never come.
	h.until("the model's answer", func() bool { return h.hasText("It ran.") })
	for _, f := range []string{first, second} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s did not run: %v", filepath.Base(f), err)
		}
	}
	want := kernel.PolicyRule{Kind: "tool", Tool: "bash"}
	h.until("the rule to be kept", func() bool {
		h.a.policy.mu.Lock()
		defer h.a.policy.mu.Unlock()
		return slices.Contains(h.a.policy.own[conv].Rules, want)
	})
}

// The composer's autonomy picker gives a Kernel conversation a level of its own: at high autonomy the command runs
// without a card.
func TestTheAutonomyPickerSetsAKernelConversationsLevel(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	h := approvalHarness(t, "touch "+marker)
	conv, _ := h.kernel.id(h.d.Sessions[1].SessionID)
	h.until("the autonomy picker", func() bool { return h.hasText("Low autonomy") })
	h.click("Autonomy")
	h.click("High autonomy")
	h.until("the level to be the conversation's", func() bool { return h.a.conversationLevel(conv) == "all" })
	h.send("Run it")
	h.until("the model's answer", func() bool { return h.hasText("It ran.") })
	if h.hasText("Droi wants to run bash") {
		t.Fatal("a card at high autonomy")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the command did not run: %v", err)
	}
}
