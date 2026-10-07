package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

// TestReadmeScreenshots is not a test: it renders the README's picture
// from the Fake Daemon into docs/.
//
//	DROI_README_SHOTS=1 go test ./apps/native/internal/app -run TestReadmeScreenshots
func TestReadmeScreenshots(t *testing.T) {
	if os.Getenv("DROI_README_SHOTS") == "" {
		t.Skip("set DROI_README_SHOTS=1 to render the README's picture")
	}
	h := newHarness(t, readmeScenario(), "")
	h.openSession("Fix the login race")
	h.until("the reply", func() bool { return h.hasText("Tasks, 3 of 4 done") })
	// Tall enough for the whole Session, from the question to the composer.
	h.tt.SetSize(1280, 1000)
	h.settle()
	writePNG(filepath.Join("..", "..", "..", "..", "docs", "screenshot.png"), h.tt.Image())
}

// readmeScenario is a computer with a few Workspaces and one Session that
// shows most of what a transcript holds.
func readmeScenario() fakedaemon.Scenario {
	user := func(s string) []fakedaemon.Message { return []fakedaemon.Message{{Role: "user", Text: s}} }
	settings := map[string]any{"modelId": "claude-opus-5-5", "reasoningEffort": "high", "autonomyLevel": "medium"}
	models := []map[string]any{
		{"id": "claude-opus-5-5", "displayName": "Opus 5.5", "shortDisplayName": "Opus 5.5", "modelProvider": "anthropic",
			"supportedReasoningEfforts": []string{"off", "low", "medium", "high"}, "defaultReasoningEffort": "high", "isCustom": false, "tokenMultiplier": 2},
	}
	return fakedaemon.Scenario{
		Input: map[string]any{
			"defaults":          settings,
			"models":            models,
			"contextUsedTokens": 48200,
		},
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Upgrade to Postgres 18", Cwd: "/Users/dev/infra", Messages: user("Upgrade the database to Postgres 18")},
			{Title: "Push notifications for receipts", Cwd: "/Users/dev/mobile-app", Messages: user("Send a push notification when a receipt is ready")},
			{Title: "Ship the billing migration", Cwd: "/Users/dev/billing-service", Messages: user("Prepare the invoices migration")},
			{Title: "Refactor invoice generator", Cwd: "/Users/dev/billing-service", Messages: user("Refactor the invoice generator")},
			{Title: "Add dark mode toggle", Cwd: "/Users/dev/acme-web", Messages: user("Add a dark mode toggle to the settings page")},
			{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: readmeHistory(), Extra: map[string]any{
				"settings": settings,
				"git": map[string]any{"branch": "fix/login-race", "files": []map[string]any{
					{"path": "src/auth/login.ts", "status": "modified", "additions": 1, "deletions": 1},
					{"path": "src/auth/login.test.ts", "status": "added", "additions": 24, "deletions": 0},
				}},
			}},
		},
	}
}

func readmeHistory() []fakedaemon.Message {
	tool := func(id, name string, input map[string]any) map[string]any {
		return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
	}
	result := func(id, content string) fakedaemon.Message {
		return fakedaemon.Message{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": id, "content": content}}}
	}
	todos := strings.Join([]string{
		"1. [completed] Find why login fails after the token expires",
		"2. [completed] Await the token refresh in login()",
		"3. [completed] Add a regression test for the expired-token path",
		"4. [in_progress] Run the whole auth suite",
	}, "\n")
	return []fakedaemon.Message{
		{Role: "user", Text: "Login sometimes fails right after the token expires. Can you find out why?"},
		{Role: "assistant", Content: []map[string]any{
			{"type": "thinking", "thinking": "A race between the refresh and the session read. Check whether login() awaits refreshToken().", "signature": "", "durationMs": 2300},
			tool("call_1", "Grep", map[string]any{"pattern": "refreshToken", "path": "src/auth"}),
			tool("call_2", "Read", map[string]any{"file_path": "src/auth/login.ts"}),
		}},
		result("call_1", "src/auth/login.ts:2\nsrc/auth/refresh.ts:14"),
		result("call_2", "export async function login(user) {\n  refreshToken(user)\n  return session(user)\n}"),
		{Role: "assistant", Content: []map[string]any{
			tool("call_3", "TodoWrite", map[string]any{"todos": todos}),
			tool("call_4", "Edit", map[string]any{"file_path": "src/auth/login.ts", "old_str": "  refreshToken(user)", "new_str": "  await refreshToken(user)"}),
			tool("call_5", "Execute", map[string]any{"command": "pnpm vitest run src/auth", "summary": "Run the auth tests"}),
		}},
		result("call_3", "TODO List Updated"),
		result("call_4", "Edited src/auth/login.ts"),
		result("call_5", "Test Files  4 passed (4)\n     Tests  27 passed (27)"),
		{Role: "assistant", Text: strings.Join([]string{
			"`login()` called `refreshToken()` without awaiting it, so `session()` could read the stale token. It now waits for the refresh:",
			"",
			"```ts",
			"export async function login(user) {",
			"  await refreshToken(user)",
			"  return session(user)",
			"}",
			"```",
			"",
			"| File | Change |",
			"| --- | --- |",
			"| `src/auth/login.ts` | await the refresh |",
			"| `src/auth/login.test.ts` | cover the expired-token path |",
			"",
			"The 27 auth tests pass. I am running the whole suite next.",
		}, "\n")},
	}
}
