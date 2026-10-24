package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

func decodeAny(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("reply is not JSON: %v\n%s", err, b)
	}
	return v
}

const featureFlagsReply = `{"flags":{"script_tools":true},"configs":{` +
	`"script_tool_models":{"models":["gpt-6-sol"],"note":"kept"},` +
	`"other_config":{"limit":3}}}`

func TestAddScriptModelsAddsTheModelAndKeepsTheRest(t *testing.T) {
	out, added := addScriptModels([]byte(featureFlagsReply), []string{"deepseek-v4.1-flash-1017"})
	if !reflect.DeepEqual(added, []string{"deepseek-v4.1-flash-1017"}) {
		t.Fatalf("added %v", added)
	}
	got, want := decodeAny(t, out), decodeAny(t, []byte(featureFlagsReply))
	models := got["configs"].(map[string]any)["script_tool_models"].(map[string]any)
	if !reflect.DeepEqual(models["models"], []any{"gpt-6-sol", "deepseek-v4.1-flash-1017"}) {
		t.Fatalf("models %v", models["models"])
	}
	if models["note"] != "kept" {
		t.Fatalf("the section lost its other fields: %v", models)
	}
	if !reflect.DeepEqual(got["flags"], want["flags"]) {
		t.Fatalf("flags changed: %v", got["flags"])
	}
	if !reflect.DeepEqual(got["configs"].(map[string]any)["other_config"], want["configs"].(map[string]any)["other_config"]) {
		t.Fatalf("another config changed: %v", got["configs"])
	}
}

func TestAddScriptModelsLeavesAListedModelAlone(t *testing.T) {
	out, added := addScriptModels([]byte(featureFlagsReply), []string{"gpt-6-sol"})
	if added != nil || string(out) != featureFlagsReply {
		t.Fatalf("added %v, reply %s", added, out)
	}
}

func TestAddScriptModelsLeavesAReplyWithoutTheConfigAlone(t *testing.T) {
	reply := `{"flags":{},"configs":{"other_config":{"limit":3}}}`
	if out, added := addScriptModels([]byte(reply), []string{"deepseek-v4.1-flash-1017"}); added != nil || string(out) != reply {
		t.Fatalf("added %v, reply %s", added, out)
	}
}

func TestAddScriptModelsLeavesABrokenReplyAlone(t *testing.T) {
	reply := `{"configs":`
	if out, added := addScriptModels([]byte(reply), []string{"deepseek-v4.1-flash-1017"}); added != nil || string(out) != reply {
		t.Fatalf("added %v, reply %s", added, out)
	}
}

func startScriptProxy(t *testing.T, upstream string, models func() []string) *Proxy {
	t.Helper()
	px := NewProxy(upstream, &keys.KeyRotator{}, nil)
	px.SetScriptModels(models)
	if _, err := px.Start("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Shutdown(t.Context()) })
	return px
}

// The feature flags droid reads carry the models Settings added, and the
// reply no longer claims Factory's encoding: the proxy sends it plain.
func TestProxyAddsScriptModelsToFeatureFlags(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = io.WriteString(zw, featureFlagsReply)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != featureFlagsPath {
			t.Errorf("upstream path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer upstream.Close()

	px := startScriptProxy(t, upstream.URL, func() []string { return []string{"deepseek-v4.1-flash-1017"} })
	resp, err := http.Get(px.BaseURL() + featureFlagsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Fatalf("the reply still says %q", enc)
	}
	cfg := decodeAny(t, body)["configs"].(map[string]any)["script_tool_models"].(map[string]any)
	if !reflect.DeepEqual(cfg["models"], []any{"gpt-6-sol", "deepseek-v4.1-flash-1017"}) {
		t.Fatalf("models %v", cfg["models"])
	}
}

func TestProxyLeavesFeatureFlagsAloneWithoutScriptModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, featureFlagsReply)
	}))
	defer upstream.Close()

	px := startScriptProxy(t, upstream.URL, nil)
	resp, err := http.Get(px.BaseURL() + featureFlagsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != featureFlagsReply {
		t.Fatalf("reply %s", body)
	}
}
