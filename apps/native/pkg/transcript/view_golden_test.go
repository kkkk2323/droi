package transcript_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// The window's shape, not only its words.
//
// The first version of the window test asserted the row count, the ids and each row's first block, and a row with
// an extra block nobody meant passed it. A golden dumps every field the rows read, so a block that appears or
// disappears, a block kind that changes, a time that moves and a call's result all show up in the diff.
//
// Update with DROI_TRANSCRIPT_GOLDEN=1 go test ./apps/native/pkg/transcript -run TestTheWindow'sStructure
func TestTheWindowStructure(t *testing.T) {
	got := dumpEntries(transcript.BuildView(windowView(t)))

	path := filepath.Join("testdata", "window.golden")
	if os.Getenv("DROI_TRANSCRIPT_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		t.Logf("wrote %s (%d bytes)", path, len(got))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with DROI_TRANSCRIPT_GOLDEN=1 to write it)", err)
	}
	if got != string(want) {
		t.Fatalf("the window's structure changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// windowView is one conversation with the shapes a window draws: a plain-string user message, an answer that
// thinks, says and reads a file, its result with a picture, a second answer with a bash call that failed, and a
// compaction before the last turn.
func windowView(t *testing.T) kernel.View {
	t.Helper()
	return viewOf(
		entry(t, 1, kernel.EntryUser,
			[]map[string]any{{"role": "user", "content": "fix the login race", "timestamp": 1000}}, nil),
		entry(t, 2, kernel.EntryAssistant, assistantMessage(t, 2000, "toolUse",
			map[string]any{"type": "thinking", "thinking": "read the file first"},
			text("let me look at it"),
			map[string]any{"type": "toolCall", "id": "c1", "name": "read",
				"arguments": map[string]any{"path": "src/auth/login.ts"}},
		), nil),
		entry(t, 3, kernel.EntryToolResult, toolResultMessage(t, 3000, "c1", false,
			text("export async function login() {}"),
			map[string]any{"type": "image", "data": "aGk=", "mimeType": "image/png"},
		), nil),
		entry(t, 4, kernel.EntryAssistant, assistantMessage(t, 4000, "stop",
			text("`refreshToken()` is not awaited."),
		), nil),
		entry(t, 5, kernel.EntryUser,
			[]map[string]any{{"role": "user", "content": []any{text("now run the tests")}, "timestamp": 5000}}, nil),
		entry(t, 6, kernel.EntryAssistant, assistantMessage(t, 6000, "toolUse",
			map[string]any{"type": "toolCall", "id": "c2", "name": "bash",
				"arguments": map[string]any{"command": "go test ./..."}},
		), nil),
		entry(t, 7, kernel.EntryToolResult, toolResultMessage(t, 7000, "c2", true, text("exit status 1")), nil),
		entry(t, 8, kernel.EntryCompaction, userMessage(t, 8000,
			text("The conversation history before this point was compacted into the following summary:\n\n<summary>\nWe fixed the login race.\n</summary>")),
			map[string]any{"reason": "auto"}),
		// After the compaction: an answer that does not fold into the one before it, and a request that failed
		// with nothing to say, which folds into that answer and marks it.
		entry(t, 9, kernel.EntryAssistant, assistantMessage(t, 9000, "stop", text("and the changelog")), nil),
		entry(t, 10, kernel.EntryAssistant, assistantMessage(t, 10000, "error"), nil),
	)
}

// dumpEntries is every field the rows read, one line per entry and one per block.
func dumpEntries(entries []*transcript.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "entry id=%s user=%v error=%v at=%v blocks=%d\n", e.ID, e.User, e.IsError, e.CreatedAt, len(e.Blocks))
		for _, block := range e.Blocks {
			fmt.Fprintf(&b, "  block kind=%s id=%s", blockKind(block.Kind), block.ID)
			if block.Text != "" {
				fmt.Fprintf(&b, " text=%q", block.Text)
			}
			if block.Kind == transcript.Picture {
				fmt.Fprintf(&b, " image=%s/%s", block.Image.MediaType, block.Image.Data)
			}
			if block.DurationMs != nil {
				fmt.Fprintf(&b, " durationMs=%v", *block.DurationMs)
			}
			if block.Streaming {
				b.WriteString(" streaming=true")
			}
			for _, call := range block.Calls {
				fmt.Fprintf(&b, "\n    call id=%s name=%s input=%s", call.Use.ID, call.Use.Name, dumpJSON(call.Input))
				if call.ResultView != nil {
					fmt.Fprintf(&b, " result=%s", dumpResult(*call.ResultView))
				} else {
					b.WriteString(" result=none")
				}
			}
			if block.Call != nil {
				fmt.Fprintf(&b, "\n    card call id=%s name=%s", block.Call.Use.ID, block.Call.Use.Name)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func blockKind(k transcript.BlockKind) string {
	switch k {
	case transcript.Text:
		return "text"
	case transcript.Picture:
		return "picture"
	case transcript.Thinking:
		return "thinking"
	case transcript.Tools:
		return "tools"
	case transcript.Subagent:
		return "subagent"
	case transcript.Spec:
		return "spec"
	case transcript.QueuedAnswers:
		return "queued-answers"
	}
	return fmt.Sprintf("kind-%d", k)
}

func dumpResult(v transcript.ToolResultView) string {
	parts := []string{fmt.Sprintf("text=%q", v.Text)}
	parts = append(parts, fmt.Sprintf("pending=%v", v.Pending), fmt.Sprintf("error=%v", v.IsError))
	if len(v.Images) > 0 {
		images := make([]string, len(v.Images))
		for i, img := range v.Images {
			images[i] = img.MediaType + "/" + img.Data
		}
		parts = append(parts, "images="+strings.Join(images, ","))
	}
	if v.Diff != nil {
		parts = append(parts, "diff=true")
	}
	if v.Status != nil {
		parts = append(parts, "status=true")
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func dumpJSON(v map[string]any) string {
	if len(v) == 0 {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	return string(b)
}
