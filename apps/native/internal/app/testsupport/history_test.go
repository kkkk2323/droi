package testsupport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// onePixel is a 1×1 PNG.
const onePixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// TestASessionsHistoryDrawsTheSameTranscriptFromAKernel: a Fake Daemon history imported into a Kernel is drawn
// as the test described it — the question, the answer with its thinking and its tool call answered by its
// result, a question with a picture. It goes through a real Kernel, so the entries are the ones Kernel accepts
// and hands back, not this package's idea of them.
func TestASessionsHistoryDrawsTheSameTranscriptFromAKernel(t *testing.T) {
	sc := fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Reading a file", Cwd: t.TempDir(), Messages: []fakedaemon.Message{
		{Role: "user", Text: "read the file"},
		{Role: "assistant", Content: []map[string]any{
			{"type": "thinking", "thinking": "let me look"},
			{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "a.go"}},
		}},
		{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "t1", "content": "package a"}}},
		{Role: "assistant", Text: "it is package a"},
		{Role: "user", Content: []map[string]any{
			{"type": "text", "text": "and this"},
			{"type": "image", "source": map[string]any{"type": "base64", "data": onePixel, "mediaType": "image/png"}},
		}},
		{Role: "assistant", Text: "a picture"},
	}}}}

	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids, err := ImportSessions(ctx, c, sc, []string{"daemon-1"}, time.UnixMilli(1_790_000_000_000), nil)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, k.Logs())
	}
	opened, err := c.Open(ctx, kernel.OpenParams{ID: ids["daemon-1"], Window: &fakekernel.DefaultWindow})
	if err != nil {
		t.Fatal(err)
	}
	rows := transcript.BuildView(opened.View)
	if len(rows) != 4 {
		t.Fatalf("%d rows, want question, answer, question, answer: %+v", len(rows), rows)
	}
	if !rows[0].User || rows[0].Blocks[0].Text != "read the file" {
		t.Errorf("the first row is %+v", rows[0])
	}
	answer := rows[1].Blocks
	if len(answer) != 3 || answer[0].Kind != transcript.Thinking || answer[1].Kind != transcript.Tools || answer[2].Text != "it is package a" {
		t.Fatalf("the answer is %+v, want thinking, the call, the text", answer)
	}
	call := answer[1].Calls[0]
	if call.Use.Name != "Read" || call.Input["file_path"] != "a.go" {
		t.Errorf("the call is %s %v", call.Use.Name, call.Input)
	}
	if shown := transcript.ReadResult(call); shown.Text != "package a" || shown.Pending || shown.IsError {
		t.Errorf("the call's result reads %+v", shown)
	}
	if q := rows[2]; !q.User || len(q.Blocks) != 2 || q.Blocks[1].Kind != transcript.Picture || q.Blocks[1].Image.Data != onePixel {
		t.Errorf("the question with a picture is %+v", q)
	}
	if rows[1].CreatedAt <= rows[0].CreatedAt || rows[3].CreatedAt <= rows[2].CreatedAt {
		t.Error("the history's times do not move forward")
	}

	list, err := c.List(ctx, kernel.ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list.Items {
		found = found || s.ID == ids["daemon-1"] && s.Title == "Reading a file"
	}
	if !found {
		t.Errorf("the conversation is not listed with its title: %+v", list.Items)
	}
}

// TestAHistoryBlockWithNoKernelCounterpartIsRefused: a block the translation does not know is named, not dropped.
func TestAHistoryBlockWithNoKernelCounterpartIsRefused(t *testing.T) {
	_, err := History([]fakedaemon.Message{{Role: "assistant", Content: []map[string]any{{"type": "document", "source": map[string]any{}}}}}, time.Now())
	var unsupported *UnsupportedScenarioError
	if !errors.As(err, &unsupported) {
		t.Fatalf("a document block translated: %v", err)
	}
}
