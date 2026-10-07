package memorywork

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

func TestAMemorySessionAnswersInJSONAndIsArchived(t *testing.T) {
	d := fakedaemon.Start(t, fakedaemon.Scenario{
		Turn:             &fakedaemon.Turn{Kind: "structured", Structured: map[string]any{"entries": []any{}}},
		ValidDirectories: []string{"/Users/dev/acme"},
	})
	run := NewRunner(func(ctx context.Context) (*droid.Client, error) {
		return droid.Dial(ctx, droid.Options{URL: d.URL, Credential: &droid.Credential{APIKey: "fk-test"}})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply, err := run(ctx, Request{
		Title: "Memory: extract from 12345678", Cwd: "/Users/dev/acme", ModelID: "glm-5.3-flash",
		Prompt: "Extract.", Input: `{"transcript":""}`,
		Schema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(reply, &got); err != nil || got["entries"] == nil {
		t.Fatalf("reply %s", reply)
	}
	init, err := d.WaitForRequest("daemon.initialize_session", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Tags                         []struct{ Name string } `json:"tags"`
		PrivacyLevel                 string                  `json:"privacyLevel"`
		AutoRejectPermissionRequests bool                    `json:"autoRejectPermissionRequests"`
		StructuredOutputFormat       struct{ Type string }   `json:"structuredOutputFormat"`
		SystemPrompt                 struct{ Append string } `json:"systemPrompt"`
		ModelID                      string                  `json:"modelId"`
	}
	_ = json.Unmarshal(init.Params, &p)
	if len(p.Tags) != 1 || p.Tags[0].Name != "droi.memory" || p.PrivacyLevel != "private" || !p.AutoRejectPermissionRequests ||
		p.StructuredOutputFormat.Type != "json_schema" || p.SystemPrompt.Append != "Extract." || p.ModelID != "glm-5.3-flash" {
		t.Errorf("initialize_session %s", init.Params)
	}
	if _, err := d.WaitForRequest("daemon.archive_session", 1); err != nil {
		t.Error("the Memory Session was not archived")
	}
}
