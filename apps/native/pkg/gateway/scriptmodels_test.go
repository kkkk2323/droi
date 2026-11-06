package gateway

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestAClientReadsTheScriptModels(t *testing.T) {
	want := ScriptModelList{Factory: []string{"claude-opus-5-5"}, Added: []string{"gemini-4-argon"}, Known: true}
	g, c := startClientGateway(t, func(o *Options) { o.ScriptModels = func() ScriptModelList { return want } })
	got, err := c.ScriptModels(context.Background())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if status, _ := do(t, "GET", workstreamURL(g.URL(), "nope", ScriptModelsPath, nil), ""); status != 401 {
		t.Errorf("without the token: %d", status)
	}
	if status, _ := do(t, "POST", workstreamURL(g.URL(), testToken, ScriptModelsPath, nil), ""); status != 405 {
		t.Errorf("POST: %d", status)
	}
}

func TestAnEmptyScriptModelListIsEmptyListsNotNull(t *testing.T) {
	g, _ := startClientGateway(t, func(o *Options) { o.ScriptModels = func() ScriptModelList { return ScriptModelList{} } })
	status, body := do(t, "GET", workstreamURL(g.URL(), testToken, ScriptModelsPath, nil), "")
	if status != 200 || body != `{"factory":[],"added":[],"known":false}` {
		t.Fatalf("%d %s", status, body)
	}
}

func TestAHostWithoutScriptModelsAnswersNotFound(t *testing.T) {
	_, c := startClientGateway(t, nil)
	if _, err := c.ScriptModels(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
