package droid_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func ptr[T any](v T) *T { return &v }

// must(f())(t) is f's result, failing t on f's error.
func must[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// TestDaemonMethods calls the session-management, settings, history,
// skills, commands, MCP, git and file methods against the Fake Daemon,
// whose answers follow the TS SDK's schemas, and checks that they decode.
func TestDaemonMethods(t *testing.T) {
	var history []fakedaemon.Message
	for i := range 30 {
		history = append(history, fakedaemon.Message{Role: []string{"user", "assistant"}[i%2], Text: fmt.Sprintf("login message %d", i)})
	}
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Messages: history, Extra: map[string]any{
				"git": map[string]any{"branch": "feat/login", "files": []map[string]any{{"path": "src/auth.ts", "status": "modified", "additions": 12, "deletions": 3}}},
			}},
			{Title: "Write docs", Cwd: "/Users/dev/acme-docs"},
		},
		Input: map[string]any{
			"commands":    []map[string]any{{"name": "review", "description": "Review the diff"}},
			"skills":      []map[string]any{{"name": "tdd", "description": "Test first"}},
			"mcpServers":  []map[string]any{{"name": "linear", "type": "http", "tools": []map[string]any{{"name": "create_issue"}, {"name": "search"}}}},
			"mcpRegistry": []map[string]any{{"name": "sentry", "description": "Errors", "type": "http", "url": "https://mcp.sentry.dev/mcp"}},
			"files":       map[string]any{"/Users/dev/acme-web/README.md": map[string]any{"mimeType": "text/markdown", "base64": base64.StdEncoding.EncodeToString([]byte("# Acme\n"))}},
		},
	})
	c := dial(t, d, nil)
	ctx := context.Background()
	id, other := d.Sessions[0].SessionID, d.Sessions[1].SessionID
	must(c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id, MessageLimit: ptr[int64](10)}))(t)

	t.Run("settings", func(t *testing.T) {
		must(c.UpdateSessionSettings(ctx, protocol.UpdateSessionSettingsParams{SessionID: id, ReasoningEffort: "high"}))(t)
		defaults := must(c.GetDefaultSettings(ctx))(t)
		if len(defaults.AvailableModels) == 0 {
			t.Fatal("no models")
		}
		res := must(c.UpdateSessionDefaults(ctx, protocol.UpdateSessionDefaultsParams{ReasoningEffort: "low"}))(t)
		if !res.Success || res.Defaults.ReasoningEffort != "low" {
			t.Fatalf("defaults: %+v", res)
		}
	})

	t.Run("manage", func(t *testing.T) {
		if r := must(c.RenameSession(ctx, protocol.RenameSessionParams{SessionID: other, Title: "Docs v2"}))(t); !r.Success {
			t.Fatal("rename failed")
		}
		if r := must(c.ArchiveSession(ctx, protocol.ArchiveSessionParams{SessionID: other}))(t); r.ArchivedAt == "" {
			t.Fatal("no archivedAt")
		}
		listed := must(c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{}))(t)
		if len(listed.Sessions) != 1 {
			t.Fatalf("archived session listed: %d", len(listed.Sessions))
		}
		must(c.UnarchiveSession(ctx, protocol.UnarchiveSessionParams{SessionID: other}))(t)
		page := must(c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{Limit: ptr(1.0)}))(t)
		if len(page.Sessions) != 1 || !page.HasMore || page.NextCursor == nil {
			t.Fatalf("first page: %+v", page)
		}
		next := must(c.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{Limit: ptr(1.0), EndBefore: page.NextCursor}))(t)
		if len(next.Sessions) != 1 || next.Sessions[0].SessionID == page.Sessions[0].SessionID {
			t.Fatalf("second page: %+v", next)
		}
		found := must(c.SearchSessions(ctx, protocol.SearchSessionsParams{Query: "login message 7"}))(t)
		if len(found.Sessions) != 1 || found.Sessions[0].SessionID != id {
			t.Fatalf("search: %+v", found)
		}
		must(c.ListOpenedSessions(ctx, protocol.ListOpenedSessionsParams{}))(t)
		if v := must(c.ValidateWorkingDirectory(ctx, protocol.ValidateWorkingDirectoryParams{WorkingDirectory: "/Users/dev/acme-web"}))(t); !v.IsValid {
			t.Fatal("known directory invalid")
		}
	})

	t.Run("history", func(t *testing.T) {
		first := must(c.GetSessionMessages(ctx, protocol.GetSessionMessagesParams{SessionID: id, Limit: ptr(10.0)}))(t)
		if len(first.Messages) != 10 || !first.HasMore || first.NextCursor == "" {
			t.Fatalf("first page: %d hasMore=%v", len(first.Messages), first.HasMore)
		}
		older := must(c.GetSessionMessages(ctx, protocol.GetSessionMessagesParams{SessionID: id, Limit: ptr(10.0), Cursor: first.NextCursor}))(t)
		if len(older.Messages) != 10 || older.Messages[0].ID == first.Messages[0].ID {
			t.Fatalf("older page: %d", len(older.Messages))
		}
		bd := must(c.GetContextBreakdown(ctx, protocol.GetContextBreakdownParams{SessionID: id}))(t)
		if bd.UsedTokens == 0 || bd.ContextBudget == 0 {
			t.Fatalf("breakdown: %+v", bd)
		}
		if r := must(c.CompactSession(ctx, protocol.CompactSessionParams{SessionID: id}))(t); r.NewSessionID == "" {
			t.Fatal("compaction made no session")
		}
	})

	t.Run("skills and commands", func(t *testing.T) {
		skills := must(c.ListSkills(ctx, protocol.ListSkillsParams{SessionID: id}))(t)
		if len(skills.Skills) != 1 || skills.Skills[0].Name != "tdd" {
			t.Fatalf("skills: %+v", skills.Skills)
		}
		if r := must(c.SetSkillDisabled(ctx, protocol.SetSkillDisabledParams{SessionID: id, SkillName: "tdd", Disabled: true}))(t); !r.Success {
			t.Fatal("skill not disabled")
		}
		cmds := must(c.ListCommands(ctx, protocol.ListCommandsParams{SessionID: id}))(t)
		if len(cmds.Commands) != 1 || cmds.Commands[0].Name != "review" {
			t.Fatalf("commands: %+v", cmds.Commands)
		}
	})

	t.Run("mcp", func(t *testing.T) {
		servers := must(c.ListMCPServers(ctx, protocol.ListMCPServersParams{SessionID: id}))(t)
		if len(servers.Servers) != 1 || servers.Servers[0].Name != "linear" {
			t.Fatalf("servers: %+v", servers.Servers)
		}
		tools := must(c.ListMCPTools(ctx, protocol.ListMCPToolsParams{SessionID: id}))(t)
		if len(tools.Tools) != 2 {
			t.Fatalf("tools: %+v", tools.Tools)
		}
		must(c.ToggleMCPTool(ctx, protocol.ToggleMCPToolParams{SessionID: id, ServerName: "linear", ToolName: "search", Enabled: false}))(t)
		must(c.ToggleMCPServer(ctx, protocol.ToggleMCPServerParams{SessionID: id, ServerName: "linear", Enabled: false, SettingsLevel: "user"}))(t)
		reg := must(c.ListMCPRegistry(ctx, protocol.ListMCPRegistryParams{SessionID: id}))(t)
		if len(reg.Servers) != 1 {
			t.Fatalf("registry: %+v", reg.Servers)
		}
		must(c.AddMCPServer(ctx, protocol.AddMCPServerParams{SessionID: id, Name: "sentry", Type: "http", URL: "https://mcp.sentry.dev/mcp"}))(t)
		must(c.RemoveMCPServer(ctx, protocol.RemoveMCPServerParams{SessionID: id, ServerName: "sentry", SettingsLevel: "user"}))(t)
	})

	t.Run("git and files", func(t *testing.T) {
		diff := must(c.GetGitDiff(ctx, protocol.GetGitDiffParams{SessionID: id}))(t)
		v := must(diff.Value())(t)
		ok, isOK := v.(*protocol.DaemonGetGitDiffSuccessResult)
		if !isOK || ok.Data.Branch != "feat/login" || len(ok.Data.Files) != 1 {
			t.Fatalf("diff: %#v", v)
		}
		noGit := must(c.GetGitDiff(ctx, protocol.GetGitDiffParams{SessionID: other}))(t)
		if u, _ := noGit.Value(); u == nil || noGit.Success {
			t.Fatalf("diff outside a repository: %#v", u)
		}
		f := must(c.GetWorkspaceFileContent(ctx, protocol.GetWorkspaceFileContentParams{SessionID: id, FilePath: "README.md"}))(t)
		if b, _ := base64.StdEncoding.DecodeString(f.Content); string(b) != "# Acme\n" {
			t.Fatalf("file: %+v", f)
		}
	})
}
