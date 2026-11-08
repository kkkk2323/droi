package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// droidTranscript writes a droid session's transcript into home's ~/.factory/sessions, from its lines.
func droidTranscript(t *testing.T, home, id string, lines ...map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".factory", "sessions", "-Users-dev-acme-web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range lines {
		raw, _ := json.Marshal(l)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func droidStart(id, title string, extra map[string]any) map[string]any {
	l := map[string]any{"type": "session_start", "id": id, "title": title, "cwd": "/Users/dev/acme-web", "version": 2}
	for k, v := range extra {
		l[k] = v
	}
	return l
}

func droidMessage(id, at string, message map[string]any) map[string]any {
	return map[string]any{"type": "message", "id": id, "timestamp": at, "message": message}
}

// kernelHostWithDroid is a Host that runs a Kernel (never started: the window's Kernel is the harness's), whose
// ~/.factory/sessions has one session with a conversation, one with nothing said and a subagent's.
func kernelHostWithDroid(t *testing.T) (*host.Host, string) {
	t.Helper()
	home := t.TempDir()
	droidTranscript(t, home, "11111111-0000-0000-0000-000000000001",
		droidStart("11111111-0000-0000-0000-000000000001", "Fix the flaky upload test", nil),
		droidMessage("m1", "2026-01-02T10:00:00Z", map[string]any{"role": "user", "content": "the upload test fails sometimes"}),
		droidMessage("m2", "2026-01-02T10:00:05Z", map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "thinking", "thinking": "hmm", "signature": "x"},
			map[string]any{"type": "text", "text": "Let me read it."},
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": map[string]any{"file_path": "upload_test.go"}},
		}}),
		droidMessage("m3", "2026-01-02T10:00:06Z", map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "func TestUpload(t *testing.T) {}"},
		}}),
		droidMessage("m4", "2026-01-02T10:00:09Z", map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "The test races the server; I added a wait."},
		}}),
	)
	droidTranscript(t, home, "11111111-0000-0000-0000-000000000002",
		droidStart("11111111-0000-0000-0000-000000000002", "Nothing said here", nil),
		droidMessage("h1", "2026-01-03T10:00:00Z", map[string]any{"role": "user", "content": []any{}, "visibility": "user_only", "hookEventName": "SessionStart"}),
	)
	droidTranscript(t, home, "11111111-0000-0000-0000-000000000003",
		droidStart("11111111-0000-0000-0000-000000000003", "A subagent's task", map[string]any{"callingSessionId": "11111111-0000-0000-0000-000000000001"}),
		droidMessage("s1", "2026-01-02T10:00:07Z", map[string]any{"role": "user", "content": "look for the race"}),
	)
	bin := filepath.Join(t.TempDir(), "kernel")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": home, host.KernelEnv: "1", host.KernelBinEnv: bin}
	return host.New(host.Config{UserData: t.TempDir(), Home: home, Env: func(k string) string { return env[k] }}), home
}

// droidImportHarness opens Settings on a window whose Kernel is the harness's and whose Host has droid sessions.
func droidImportHarness(t *testing.T) (*harness, *host.Host) {
	t.Helper()
	h0, _ := kernelHostWithDroid(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	if h.kernel == nil {
		t.Skip("no Kernel for the window")
	}
	h.until("the sign-in page", func() bool { return h.hasText("Sign in to Droi") })
	h.click("Settings")
	h.until("the import", func() bool { return h.hasText("droid sessions") })
	h.click("Find droid sessions")
	h.until("the sessions", func() bool { return h.hasText("Fix the flaky upload test") })
	return h, h0
}

// clickShown scrolls Settings until the control named name is in the window, then clicks it.
func (h *harness) clickShown(name string) {
	h.t.Helper()
	r, ok := h.tt.Find(name)
	for i := 0; ok && r.Y+r.H > refH-8 && i < 30; i++ {
		h.tt.Scroll(refW/2, refH/2, 0, 200)
		h.frame()
		r, ok = h.tt.Find(name)
	}
	h.click(name)
}

// conversationTitled is the Kernel's conversation with title, if there is one.
func (h *harness) conversationTitled(title string) (kernel.Summary, bool) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := h.testConn().List(ctx, kernel.ListParams{})
	if err != nil {
		h.t.Fatal(err)
	}
	for _, s := range list.Items {
		if s.Title == title {
			return s, true
		}
	}
	return kernel.Summary{}, false
}

// Settings lists droid's sessions with a conversation in them, imports none until the user picks one, and brings
// the picked one over with its history: the answer's tool call and its result, and the session marked seen.
func TestSettingsImportsThePickedDroidSession(t *testing.T) {
	h, h0 := droidImportHarness(t)
	if h.hasText("Nothing said here") || h.hasText("A subagent's task") {
		t.Fatal("a session with nothing said, or a subagent's, is offered")
	}
	if _, ok := h.conversationTitled("Fix the flaky upload test"); ok {
		t.Fatal("a session was imported before the user picked it")
	}
	h.clickShown("Pick Fix the flaky upload test")
	h.clickShown("Import the picked sessions")
	h.until("the import's answer", func() bool { return h.hasText("Imported 1 of 1 sessions.") })

	conv, ok := h.conversationTitled("Fix the flaky upload test")
	if !ok {
		t.Fatal("the session is not a conversation")
	}
	if conv.Workspace != "/Users/dev/acme-web" || conv.ViewedAt == nil || *conv.ViewedAt < conv.LastActivityAt {
		t.Fatalf("the conversation is %+v", conv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := h.testConn().Entries(ctx, kernel.EntriesParams{ID: conv.ID, Query: json.RawMessage(`{"order":"ascending"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var all strings.Builder
	for _, e := range page.Items {
		if strings.HasPrefix(e.Kind, "pi.") || e.Kind == kernel.EntryNote {
			kinds = append(kinds, e.Kind)
			all.Write(e.Model)
			all.Write(e.Data)
		}
	}
	want := []string{kernel.EntryUser, kernel.EntryAssistant, kernel.EntryToolResult, kernel.EntryAssistant, kernel.EntryNote}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("the entries are %v, want %v", kinds, want)
	}
	for _, s := range []string{`"toolCall"`, `"toolCallId":"toolu_1"`, "func TestUpload", "I added a wait", "Imported from droid"} {
		if !strings.Contains(all.String(), s) {
			t.Errorf("the history has no %s", s)
		}
	}
	if strings.Contains(all.String(), "hmm") {
		t.Error("droid's thinking came over")
	}
	if !h.hasText(" · Imported") {
		t.Fatal("the row does not say it was imported")
	}
	if _, err := os.Stat(filepath.Join(h0.UserData(), "droid-imports.json")); err != nil {
		t.Fatalf("no record of the import: %v", err)
	}
}

// A session whose import fails is named with why, and can be kept as Markdown.
func TestADroidSessionThatFailsCanBeKeptAsMarkdown(t *testing.T) {
	h, h0 := droidImportHarness(t)
	h.kernel.k.Close()
	h.clickShown("Pick Fix the flaky upload test")
	h.clickShown("Import the picked sessions")
	h.until("the failure", func() bool { return h.hasText("Fix the flaky upload test did not come over") })
	h.clickShown("Export Fix the flaky upload test as Markdown")
	path := filepath.Join(h0.UserData(), "droid-import", "11111111-0000-0000-0000-000000000001.md")
	h.until("the export", func() bool { return h.hasText("Saved to") })
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"# Fix the flaky upload test", "the upload test fails sometimes", "Tool `Read`", "I added a wait"} {
		if !strings.Contains(string(raw), s) {
			t.Errorf("the Markdown has no %q:\n%s", s, raw)
		}
	}
}
