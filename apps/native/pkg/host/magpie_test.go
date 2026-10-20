package host

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// magpieServer stands in for a Magpie gateway answering its model list.
func magpieServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+magpieKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReadMagpieModelsReadsTheGateway(t *testing.T) {
	srv := magpieServer(t, `{"object":"list","data":[
		{"id":"workbuddy/glm-5.3-flash","display_name":"GLM 5.3 Flash","max_output_tokens":32000,
		 "modalities":{"input":["text"]}},
		{"id":"codex/gpt-6.1-sol","display_name":"GPT-6.1-Sol","max_output_tokens":128000,
		 "modalities":{"input":["text","image"]}}
	]}`)
	models, err := ReadMagpieModels(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "workbuddy/glm-5.3-flash" || models[1].MaxOutputTokens != 128000 {
		t.Fatalf("models %+v", models)
	}
	if models[0].Images() || !models[1].Images() {
		t.Errorf("modalities: %+v", models)
	}
}

func TestReadMagpieModelsRejectsAnotherService(t *testing.T) {
	for _, body := range []string{`{"hello":"world"}`, `not json`, ``} {
		if _, err := ReadMagpieModels(magpieServer(t, body).URL); err == nil {
			t.Errorf("%q was taken for a Magpie model list", body)
		}
	}
}

func TestMagpieCustomModels(t *testing.T) {
	cat := MagpieCatalog{BaseURL: "http://127.0.0.1:3425/", Models: []MagpieModel{
		{ID: "workbuddy/glm-5.3", Name: "GLM 5.3", MaxOutputTokens: 32000},
		{ID: "codex/gpt-6.1-sol", Name: "GPT-6.1-Sol", MaxOutputTokens: 128000,
			Modalities: MagpieModalities{Input: []string{"text", "image"}}},
		{ID: "workbuddy/glm-5.3-flash"},
		{ID: ""},
	}}
	got := MagpieCustomModels(cat)
	if len(got) != 3 {
		t.Fatalf("%d entries: %+v", len(got), got)
	}
	want := []map[string]any{
		{
			"id": "magpie-codex-gpt-6-1-sol", "model": "codex/gpt-6.1-sol",
			"displayName": "Magpie · GPT-6.1-Sol", "baseUrl": "http://127.0.0.1:3425/v1",
			"apiKey": "magpie", "provider": MagpieProvider, "maxOutputTokens": 128000,
		},
		{
			"id": "magpie-workbuddy-glm-5-3", "model": "workbuddy/glm-5.3",
			"displayName": "Magpie · GLM 5.3", "baseUrl": "http://127.0.0.1:3425/v1",
			"apiKey": "magpie", "provider": MagpieProvider, "maxOutputTokens": 32000,
			"noImageSupport": true,
		},
		{
			"id": "magpie-workbuddy-glm-5-3-flash", "model": "workbuddy/glm-5.3-flash",
			"displayName": "Magpie · workbuddy/glm-5.3-flash", "baseUrl": "http://127.0.0.1:3425/v1",
			"apiKey": "magpie", "provider": MagpieProvider, "maxOutputTokens": magpieDefaultOutput,
			"noImageSupport": true,
		},
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("entry %d\n got %v\nwant %v", i, got[i], want[i])
		}
	}
	// The same list twice is the same ids, so a Session keeps its model.
	if again := MagpieCustomModels(cat); !reflect.DeepEqual(again, got) {
		t.Errorf("the second read differs: %v", again)
	}
}

func TestMagpieModelIDsDoNotCollide(t *testing.T) {
	taken := map[string]bool{}
	first := MagpieModelID("a/b.c", taken)
	taken[first] = true
	if second := MagpieModelID("a/b-c", taken); second != first+"-2" {
		t.Errorf("%q then %q", first, second)
	}
	if got := MagpieModelID("!!!", map[string]bool{}); got != "magpie-model" {
		t.Errorf("a name with no letters: %q", got)
	}
}

func TestTheHostOffersMagpieModelsToTheDaemon(t *testing.T) {
	srv := magpieServer(t, `{"data":[{"id":"workbuddy/glm-5.3","display_name":"GLM 5.3","max_output_tokens":32000}]}`)
	h := testHost(t)
	if err := h.Settings.Update(func(st *Settings) {
		st.MagpieEnabled = true
		st.MagpieBaseURL = OptString(srv.URL)
	}); err != nil {
		t.Fatal(err)
	}
	path := h.runtimeOverlay()
	if path == "" {
		t.Fatal("no overlay with Magpie on")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		CustomModels []map[string]any `json:"customModels"`
		Hooks        map[string]any   `json:"hooks"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.CustomModels) != 1 || got.CustomModels[0]["id"] != "magpie-workbuddy-glm-5-3" {
		t.Fatalf("overlay %s", b)
	}
	if got.Hooks != nil {
		t.Errorf("Memory's hooks came with Magpie's models: %s", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	// The CLI's own settings are not written.
	if _, err := os.Stat(filepath.Join(h.SharedFactoryHome(), "settings.json")); !os.IsNotExist(err) {
		t.Errorf("settings.json written: %v", err)
	}
}

func TestTheHostKeepsTheLastModelsWhenMagpieIsGone(t *testing.T) {
	srv := magpieServer(t, `{"data":[{"id":"workbuddy/glm-5.3"}]}`)
	h := testHost(t)
	if err := h.Settings.Update(func(st *Settings) { st.MagpieBaseURL = OptString(srv.URL) }); err != nil {
		t.Fatal(err)
	}
	h.RefreshMagpie()
	if cat := h.Magpie(); !cat.Running() || len(cat.Models) != 1 {
		t.Fatalf("catalog %+v", cat)
	}
	srv.Close()
	h.RefreshMagpie()
	cat := h.Magpie()
	if cat.Running() || cat.Err == "" {
		t.Errorf("the gateway is gone but the catalog says it runs: %+v", cat)
	}
	if len(cat.Models) != 1 {
		t.Errorf("the last models were dropped: %+v", cat)
	}
	if got := MagpieCustomModels(cat); len(got) != 1 {
		t.Errorf("the picker lost its models: %v", got)
	}
}

// testHost is a Host on a temporary folder, with nothing to start.
func testHost(t *testing.T) *Host {
	t.Helper()
	dir := t.TempDir()
	return New(Config{UserData: dir, Home: dir, Env: func(string) string { return "" }})
}
