package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// On the Kernel, a task call is a subagent card: it says how the subagent stands and opens the child conversation
// the Kernel ran it in.
func TestAKernelSubagentIsACardThatOpensItsConversation(t *testing.T) {
	scriptKernel(t, func(sc *fakekernel.Scenario) {
		sc.Steps = append([]fakekernel.Step{
			{Match: "Explore the repo", Call: &fakekernel.Call{Name: "task", ID: "call_task", Arguments: map[string]any{
				"subagent_type": "explorer", "description": "Look around", "prompt": "List the files here"}}},
			{Match: "List the files here", Text: "Two files."},
			{Match: "Explore the repo", Text: "Done exploring."},
		}, sc.Steps...)
	})
	h := kernelHarness(t)
	h.openSession("Add dark mode toggle")
	h.send("Explore the repo")
	h.until("the subagent's card", func() bool {
		if h.hasButton("Allow once") {
			h.click("Allow once")
		}
		return h.hasText("Done exploring.") && h.hasButton("Open subagent session") && h.hasText("Look around")
	})
	h.shot("subagent-card")
	h.click("Open subagent session")
	h.until("the subagent's conversation", func() bool { return h.hasText("Two files.") })
}

// Settings → Droids lists the Kernel's droids, saves a person's own into the Kernel's droids folder, and deletes it.
func TestSettingsDroidsSavesAndDeletesYourOwn(t *testing.T) {
	h := kernelHarness(t)
	h.openSettingsTab("droids")
	h.until("the built-in droids", func() bool { return h.hasText("worker") && h.hasText("explorer") })
	h.shot("droids-list")
	h.click("New droid")
	h.replaceText("Droid name", "reviewer")
	h.replaceText("Droid description", "Reviews a diff")
	h.replaceText("Droid prompt", "List each risk.")
	h.shot("droids-editor")
	h.clickShown("Save droid")
	file := filepath.Join(h.kernel.k.UserDir, "droids", "reviewer.md")
	h.until("the droid in the list", func() bool { return h.hasText("reviewer") && !h.hasButton("Save droid") })
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, "Reviews a diff") || !strings.Contains(s, "List each risk.") {
		t.Errorf("the file is %q", s)
	}
	h.shot("droids-saved")
	h.clickShown("Delete")
	h.clickShown("Click again to confirm")
	h.until("the droid gone", func() bool { return !h.hasText("reviewer") })
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
}
