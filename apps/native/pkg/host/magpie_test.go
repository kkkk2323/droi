package host

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// magpieServer stands in for a Magpie gateway answering its model list.
func magpieServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return magpieServerFunc(t, func() string { return body })
}

// magpieServerFunc is magpieServer with a list that changes between reads.
func magpieServerFunc(t *testing.T, answer func() string) *httptest.Server {
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
		_, _ = io.WriteString(w, answer())
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
			"id": MagpieModelID("codex/gpt-6.1-sol"), "model": "codex/gpt-6.1-sol",
			"displayName": "Magpie · GPT-6.1-Sol", "baseUrl": "http://127.0.0.1:3425/v1",
			"apiKey": "magpie", "provider": MagpieProvider, "maxOutputTokens": 128000,
		},
		{
			"id": MagpieModelID("workbuddy/glm-5.3"), "model": "workbuddy/glm-5.3",
			"displayName": "Magpie · GLM 5.3", "baseUrl": "http://127.0.0.1:3425/v1",
			"apiKey": "magpie", "provider": MagpieProvider, "maxOutputTokens": 32000,
			"noImageSupport": true,
		},
		{
			"id": MagpieModelID("workbuddy/glm-5.3-flash"), "model": "workbuddy/glm-5.3-flash",
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

// droid resolves a custom model only by an id that starts with "custom:"; an
// entry without it is listed and then ignored, and the Session runs another
// model without saying so.
func TestMagpieModelIDsAreCustomIDsDroidResolves(t *testing.T) {
	for _, model := range []string{"workbuddy/glm-5.3-flash", "plain-model", "a/b.c"} {
		id := MagpieModelID(model)
		if !strings.HasPrefix(id, "custom:") {
			t.Errorf("%q is %q, which droid does not resolve", model, id)
		}
		if strings.Count(id, ":") != 1 || strings.ContainsAny(id, " /") {
			t.Errorf("%q holds more than the prefix's colon, a space or a slash", id)
		}
	}
}

// An id depends on its model alone: adding a model, reordering the list or
// renaming a display name moves no Session to another model.
func TestMagpieModelIDsDoNotMove(t *testing.T) {
	one := MagpieCustomModels(MagpieCatalog{Models: []MagpieModel{{ID: "a/b.c", Name: "B"}}})
	if len(one) != 1 {
		t.Fatalf("entries %v", one)
	}
	before := one[0].(map[string]any)["id"]
	// "a/b-c" comes out the same in letters but is another model, and it sorts
	// first: it must not take the id the other one had.
	both := MagpieCustomModels(MagpieCatalog{Models: []MagpieModel{
		{ID: "a/b-c", Name: "C"},
		{ID: "a/b.c", Name: "B"},
	}})
	after := both[1].(map[string]any)["id"]
	if before != after {
		t.Errorf("a/b.c moved from %q to %q", before, after)
	}
	if both[0].(map[string]any)["id"] == after {
		t.Errorf("a/b-c took the same id %q", after)
	}
	// A display name is the picker's, not the id's.
	renamed := MagpieCustomModels(MagpieCatalog{Models: []MagpieModel{{ID: "a/b.c", Name: "Renamed"}}})
	if renamed[0].(map[string]any)["id"] != before {
		t.Errorf("renaming moved the id to %q", renamed[0].(map[string]any)["id"])
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
	if len(got.CustomModels) != 1 || got.CustomModels[0]["id"] != MagpieModelID("workbuddy/glm-5.3") {
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

// A refresh that found the same models must not restart the Daemon under a
// Session that is working.
func TestMagpieCatalogComparison(t *testing.T) {
	a := MagpieCatalog{BaseURL: "http://127.0.0.1:3425", Models: []MagpieModel{{ID: "a/b"}, {ID: "c/d"}}}
	b := MagpieCatalog{BaseURL: "http://127.0.0.1:3425", Models: []MagpieModel{{ID: "c/d"}, {ID: "a/b"}}}
	if !sameMagpieModels(a, b) {
		t.Error("the same models in another order are a change")
	}
	b.Models = append(b.Models, MagpieModel{ID: "e/f"})
	if sameMagpieModels(a, b) {
		t.Error("a new model is not a change")
	}
	b = a
	b.BaseURL = "http://127.0.0.1:3999"
	if sameMagpieModels(a, b) {
		t.Error("another gateway is not a change")
	}
	b = a
	b.Err = "connection refused"
	if !sameMagpieModels(a, b) {
		t.Error("a failed read kept the models, which is what the Daemon has")
	}
}

// A refresh tells its caller whether the Daemon has to start again, and it
// compares with what the Daemon was given, not with the cache.
func TestRefreshMagpieReportsAChange(t *testing.T) {
	first := magpieServer(t, `{"data":[{"id":"a/b"}]}`)
	other := magpieServer(t, `{"data":[{"id":"c/d"}]}`)
	h := testHost(t)
	enableMagpie(t, h, first.URL)
	h.runtimeOverlay()
	if h.RefreshMagpie() {
		t.Error("the same list was taken for a change, so the Daemon would restart for nothing")
	}
	enableMagpie(t, h, other.URL)
	if !h.RefreshMagpie() {
		t.Error("another gateway's models were taken for the same")
	}
	h.runtimeOverlay()
	if h.RefreshMagpie() {
		t.Error("the Daemon was given them, and still a restart was asked for")
	}
}

// The Settings page reads a stale catalog in the background as it draws, which
// happens just as the user opens it to press Refresh: that read must not hide
// the change the Daemon has not seen yet.
func TestRefreshAfterABackgroundReadStillStartsTheDaemon(t *testing.T) {
	var listed atomic.Value
	listed.Store(`{"data":[{"id":"a/b"}]}`)
	srv := magpieServerFunc(t, func() string { return listed.Load().(string) })
	h := testHost(t)
	enableMagpie(t, h, srv.URL)
	h.runtimeOverlay()

	listed.Store(`{"data":[{"id":"a/b"},{"id":"c/d"}]}`)
	h.magpieMu.Lock()
	h.magpie.Read = time.Now().Add(-2 * time.Minute)
	h.magpieMu.Unlock()
	h.Magpie()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.magpieCached().Models) != 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(h.magpieCached().Models) != 2 {
		t.Fatal("the background read did not take")
	}
	if !h.RefreshMagpie() {
		t.Error("Refresh found no change though the Daemon still has one model")
	}
}

// enableMagpie points the Host at a gateway with Magpie on.
func enableMagpie(t *testing.T, h *Host, url string) {
	t.Helper()
	if err := h.Settings.Update(func(st *Settings) {
		st.MagpieEnabled = true
		st.MagpieBaseURL = OptString(url)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeMagpieBaseURL(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"http://127.0.0.1:3425", "http://127.0.0.1:3425"},
		{"http://127.0.0.1:3425/", "http://127.0.0.1:3425"},
		{" 127.0.0.1:3425 ", "http://127.0.0.1:3425"},
		{"https://magpie.lan/", "https://magpie.lan"},
		{"", ""},
	} {
		if got := NormalizeMagpieBaseURL(c.in); got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

// testHost is a Host on a temporary folder, with nothing to start.
func testHost(t *testing.T) *Host {
	t.Helper()
	dir := t.TempDir()
	return New(Config{UserData: dir, Home: dir, Env: func(string) string { return "" }})
}
