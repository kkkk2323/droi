package app

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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
		// A Script still going is the run of a call Droid waits on, not a background task.
		{Role: "assistant", Content: []map[string]any{{"type": "tool_use", "id": "toolu_sc", "name": "Script",
			"input": map[string]any{"script": "await tools.Execute({command: 'go test ./...'})"}}}},
		{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "toolu_sc", "content": []map[string]any{
			{"type": "text", "text": "[Script running · 30.0 s elapsed · 1 pending call]"}, {"type": "text", "text": "{\"toolCallId\":\"toolu_sc\",\"status\":\"running\"}"}}}}},
		{Role: "assistant", Text: "The release runs in the background."},
	}}}}
}

func TestABackgroundCommandShowsWhileItRuns(t *testing.T) {
	h := newHarness(t, backgroundJobScenario(t), "")
	h.openSession("Ship the release")
	h.until("the header's count", func() bool { return h.hasText("1 background task") && h.hasText("Running in background · ") })
	h.until("the sidebar's mark", func() bool { return h.hasText("1 background task running") })
	if h.hasText("2 background tasks") {
		t.Fatalf("the running Script counts as a background task: %q", h.tt.Texts())
	}

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

// The Daemon lists the command among the Session's background processes,
// so its row has Stop, which ends it through the Daemon.
func TestABackgroundProcessStopsFromTheList(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "backgroundProcess", Command: "npm run dev", Reply: "The dev server runs.", Pid: os.Getpid()}), "")
	h.openSession("Deploy")
	h.send("Start the dev server")
	h.until("the header's count", func() bool { return h.hasText("1 background task") && h.hasText("The dev server runs.") })
	h.click("1 background task")
	h.until("Stop", func() bool { return h.hasButton("Stop Run in the background") })
	h.click("Stop Run in the background")
	id := h.a.Route().SessionID
	h.until("the task stopped", func() bool {
		return !h.hasText("1 background task") && h.a.ctl.Store().Session(id).BackgroundProcesses() == nil
	})
}

// A Script a turn left running counts as a background task while the
// Daemon counts work for it; its notification and its end show where they
// woke the Session.
func TestABackgroundScriptShowsAndWakesTheSession(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "backgroundScript", Command: "ocr-status", ToolOutput: "2 pages/s",
		Reply: "I will report.", Notification: "04:05Z 2.0 pages/s", NotificationReply: "The OCR runs at 2 pages a second.", DelayMs: 2500}), "")
	h.openSession("Deploy")
	h.send("Check on the OCR")
	h.until("the Script's count", func() bool { return h.hasText("I will report.") && h.hasText("1 background task") })
	h.click("1 background task")
	h.until("its row", func() bool { return h.hasText("Script · Execute") })
	h.click("1 background task")
	h.until("the wake", func() bool {
		return h.hasText("Background Script sent a notification") && h.hasText("The OCR runs at 2 pages a second.")
	})
	h.until("the Script's end", func() bool {
		return h.hasText("Background Script finished") && h.hasText("The background Script finished.") && !h.hasText("1 background task")
	})
	h.click("Background Script sent a notification: Execute")
	h.until("what it told and the call it made", func() bool { return h.hasText("04:05Z 2.0 pages/s") && h.hasText("ocr-status") })
}

// A Script the transcript says still runs counts only while the Daemon
// counts work for the Session: one from before the Daemon started is gone.
func TestABackgroundScriptCountsWhileTheDaemonCountsIt(t *testing.T) {
	running := "{\"toolCallId\":\"toolu_bg\",\"status\":\"running\"}"
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Watch the OCR", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
		{Role: "user", Text: "Check on the OCR every 15 minutes"},
		{Role: "assistant", Content: []map[string]any{{"type": "tool_use", "id": "toolu_bg", "name": "Script",
			"input": map[string]any{"script": "// @script: {\"startInBackground\": true}\nawait tools.Execute({command: 'ocr-status'})\nnotify('x')"}}}},
		{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "toolu_bg", "content": []map[string]any{
			{"type": "text", "text": "[Script running · 0.0 s elapsed · 0 pending calls]"}, {"type": "text", "text": running}}}}},
		{Role: "assistant", Text: "I will report every 15 minutes."},
	}}}}, "")
	h.openSession("Watch the OCR")
	h.until("the transcript", func() bool { return h.hasText("I will report every 15 minutes.") })
	h.settle()
	if h.hasText("1 background task") {
		t.Fatalf("a Script the Daemon does not count shows: %q", h.tt.Texts())
	}

	id := h.a.Route().SessionID
	now := float64(time.Now().UnixMilli())
	notify := func(n map[string]any) {
		t.Helper()
		if err := h.d.Notify(id, n); err != nil {
			t.Fatal(err)
		}
	}
	message := func(m map[string]any) {
		now++
		m["createdAt"], m["updatedAt"] = now, now
		notify(map[string]any{"type": "create_message", "message": m})
	}
	message(map[string]any{"id": "repl-tool-call:bg-1", "role": "assistant", "visibility": "user_only", "content": []any{map[string]any{
		"type": "tool_use", "id": "bg-1", "name": "Execute", "input": map[string]any{"command": "ocr-status"},
		"scriptExecution": map[string]any{"runId": "toolu_bg", "outerToolUseId": "toolu_bg"}}}})
	message(map[string]any{"id": "script:notify:" + id + ":toolu_bg:bg-2", "role": "user", "visibility": "llm_only",
		"content": []any{map[string]any{"type": "text", "text": "Script notification:\n2.0 pages/s"}}})
	notify(map[string]any{"type": "droid_working_state_changed", "newState": "idle", "backgroundWork": 1})
	h.until("the count and the wake", func() bool {
		return h.hasText("1 background task") && h.hasText("Background Script sent a notification")
	})
	h.click("1 background task")
	h.click("Output of Execute")
	h.until("its last notification", func() bool { return h.hasText("2.0 pages/s") && h.hasText("Last notification · ") })
	h.click("1 background task")

	message(map[string]any{"id": "script-completion:" + id + ":toolu_bg:toolu_bg", "role": "user", "visibility": "llm_only",
		"content": []any{map[string]any{"type": "text", "text": "Script toolu_bg reached its terminal state with result {\"toolCallId\":\"toolu_bg\",\"status\":\"completed\",\"result\":null}. This is a new asynchronous lifecycle event."}}})
	notify(map[string]any{"type": "droid_working_state_changed", "newState": "idle", "backgroundWork": 0})
	h.until("the end", func() bool { return !h.hasText("1 background task") && h.hasText("Background Script finished") })
}
