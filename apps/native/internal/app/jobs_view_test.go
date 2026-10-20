package app

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// backgroundJobScenario is a Session whose Execute left a command running:
// this test's own process, so it stays alive while the test runs.
func backgroundJobScenario(t *testing.T) fakedaemon.Scenario {
	out := filepath.Join(t.TempDir(), "droid-bg.out")
	if err := os.WriteFile(out, []byte("building…\nstep 1 of 3\nstep 2 of 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := "Background process started (PID: " + strconv.Itoa(os.Getpid()) + ")\nCommand: scripts/release.sh 1.58.0\nOutput: " + out +
		"\nStatus: Running in background\n\nThe command's result will be delivered automatically when it finishes."
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Ship the release", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
		{Role: "user", Text: "Release 1.58.0"},
		{Role: "assistant", Content: []map[string]any{{"type": "tool_use", "id": "toolu_bg", "name": "Execute",
			"input": map[string]any{"summary": "Release Droi 1.58.0", "command": "scripts/release.sh 1.58.0", "fireAndForget": true}}}},
		{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "toolu_bg", "content": started}}},
		{Role: "assistant", Text: "The release runs in the background."},
	}}}}
}

func TestABackgroundCommandShowsWhileItRuns(t *testing.T) {
	h := newHarness(t, backgroundJobScenario(t), "")
	h.openSession("Ship the release")
	h.until("the header's count", func() bool { return h.hasText("1 background task") && h.hasText("Running in background · ") })
	h.until("the sidebar's mark", func() bool { return h.hasText("1 background task running") })

	h.click("1 background task")
	h.click("Output of Release Droi 1.58.0")
	h.until("the output's last lines", func() bool { return h.hasText("step 2 of 3") })

	id := h.a.Route().SessionID
	if err := h.d.Notify(id, map[string]any{"type": "create_message", "message": map[string]any{
		"id": "execute-completion:toolu_bg", "role": "user", "content": []any{}, "createdAt": 1, "updatedAt": 1, "visibility": "llm_only"}}); err != nil {
		t.Fatal(err)
	}
	h.until("the task ended", func() bool { return !h.hasText("Running in background") && !h.hasText("1 background task") })
	if !h.hasText("Finished") {
		t.Fatalf("the row does not say it finished: %q", h.tt.Texts())
	}
}
