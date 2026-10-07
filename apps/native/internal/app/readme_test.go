package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

// TestReadmeScreenshots is not a test: it renders the README's pictures
// from the Fake Daemon into docs/.
//
//	DROI_README_SHOTS=1 go test ./apps/native/internal/app -run TestReadmeScreenshots
func TestReadmeScreenshots(t *testing.T) {
	if os.Getenv("DROI_README_SHOTS") == "" {
		t.Skip("set DROI_README_SHOTS=1 to render the README's pictures")
	}
	docs := filepath.Join("..", "..", "..", "..", "docs")
	save := func(h *harness, name string) {
		t.Helper()
		// Tall enough for the whole Session, from the question to the composer.
		h.tt.SetSize(1280, 1000)
		h.settle()
		path := filepath.Join(docs, name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		writePNG(path, h.tt.Image())
	}
	session := func(h *harness) {
		h.openSession("Fix the login race")
		h.until("the reply", func() bool { return h.hasText("Tasks, 3 of 4 done") })
	}

	t.Run("session", func(t *testing.T) {
		h := newHarness(t, readmeScenario(nil), "")
		session(h)
		save(h, "screenshot.png")
	})
	t.Run("dark", func(t *testing.T) {
		h := newHarness(t, readmeScenario(nil), "dark")
		session(h)
		save(h, "screenshots/dark.png")
	})
	t.Run("new session", func(t *testing.T) {
		h := newHarness(t, readmeScenario(nil), "")
		h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
		h.click("New session")
		h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
		save(h, "screenshots/new-session.png")
	})
	t.Run("permission", func(t *testing.T) {
		turn := &fakedaemon.Turn{Kind: "permission", Command: "pnpm db:migrate --env staging", Reply: "Migrated staging."}
		h := newHarness(t, readmeScenario(turn), "")
		h.openSession("Ship the billing migration")
		h.send("Run the migration on staging")
		h.until("the permission card", func() bool { return h.hasText("pnpm db:migrate --env staging") })
		save(h, "screenshots/permission.png")
	})
	t.Run("remote access", func(t *testing.T) {
		g := &fakeRemote{lan: []string{"192.168.1.20"}}
		h0 := testHost(t)
		h := newHarnessWith(t, readmeScenario(nil), "", func(cfg *Config) { cfg.Host = h0; cfg.Remote = g })
		h.a.Go(Route{Name: "settings", Tab: "remote"})
		h.settle()
		clickSwitchBeside(h, []string{"Off: only this window"}, func() bool { return h0.Settings.Get().RemoteAccess })
		h.until("the QR code", func() bool { _, ok := h.tt.Find("Pairing QR code"); return ok })
		save(h, "screenshots/remote-access.png")
	})
}

// readmeScenario is a computer with a few Workspaces and one Session that
// shows most of what a transcript holds; turn is how the agent answers.
func readmeScenario(turn *fakedaemon.Turn) fakedaemon.Scenario {
	user := func(s string) []fakedaemon.Message { return []fakedaemon.Message{{Role: "user", Text: s}} }
	settings := map[string]any{"modelId": "claude-opus-4-1", "reasoningEffort": "high", "autonomyLevel": "medium"}
	return fakedaemon.Scenario{
		Turn: turn,
		Input: map[string]any{
			"defaults":          settings,
			"contextUsedTokens": 48200,
		},
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Upgrade to Postgres 18", Cwd: "/Users/dev/infra", Messages: user("Upgrade the database to Postgres 18")},
			{Title: "Push notifications for receipts", Cwd: "/Users/dev/mobile-app", Messages: user("Send a push notification when a receipt is ready")},
			{Title: "Ship the billing migration", Cwd: "/Users/dev/billing-service", Messages: user("Prepare the invoices migration"), Extra: map[string]any{"settings": settings}},
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
