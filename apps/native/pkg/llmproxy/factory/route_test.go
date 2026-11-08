package factory

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestLLMBaseFollowsTheOrganization(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured string
		id         Identity
		want       string
	}{
		{"no region", API, Identity{OrgID: "o"}, API},
		{"EU", API, Identity{Region: "eu"}, APIEU},
		{"its own host", API, Identity{Region: "eu", PremBaseHost: "prem.acme.example/"}, "https://prem.acme.example"},
		{"its own host with a scheme", API, Identity{PremBaseHost: "http://10.0.0.2:8080"}, "http://10.0.0.2:8080"},
		{"Factory's API with a slash", API + "/", Identity{Region: "eu"}, APIEU},
		{"a base somebody chose", "http://127.0.0.1:9000", Identity{Region: "eu", PremBaseHost: "prem.acme.example"}, "http://127.0.0.1:9000"},
	} {
		if got := LLMBase(tc.configured, tc.id); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRefusalsAreToldApart(t *testing.T) {
	org := []byte(`{"error":"Requested active organization is not accessible by this user"}`)
	if !OrgRefused(403, org) || OrgRefused(401, org) || OrgRefused(403, []byte(`{"error":"Forbidden"}`)) {
		t.Fatal("OrgRefused")
	}
	for _, body := range []string{`{"code":"content_policy_rejected"}`, `{"error":{"code":"content_policy_rejected","message":"x"}}`} {
		if !ContentRefused(403, http.Header{}, []byte(body)) {
			t.Fatalf("%s is a content refusal", body)
		}
	}
	vendor := http.Header{"X-Factory-Upstream-Error": {"true"}}
	for name, tc := range map[string]struct {
		status int
		header http.Header
		body   string
	}{
		"another code":       {403, http.Header{}, `{"error":{"code":"forbidden"}}`},
		"not JSON":           {403, http.Header{}, `content_policy_rejected`},
		"a 400":              {400, http.Header{}, `{"code":"content_policy_rejected"}`},
		"the vendor refused": {403, vendor, `{"code":"content_policy_rejected"}`},
	} {
		if ContentRefused(tc.status, tc.header, []byte(tc.body)) {
			t.Errorf("%s is not a content refusal", name)
		}
	}
}

func TestHeadersAreDroids(t *testing.T) {
	h := http.Header{"X-Api-Key": {"sk-client"}, "X-Goog-Api-Key": {"g"}, "X-Factory-Org-Id": {"stale"}, "Openai-Platform": {"x"}}
	Headers(h, Identity{}, "/api/llm/o/v1/chat/completions", "kimi-k3", "s")
	if h.Get("X-Api-Key") != "" || h.Get("X-Goog-Api-Key") != "" || h.Get("X-Factory-Org-Id") != "" || h.Get("OpenAI-Platform") != "" {
		t.Fatalf("a client's keys, a stale org or a platform stayed: %v", h)
	}
	if h.Get("X-Api-Provider") != "fireworks" || h.Get("X-Session-Id") != "s" {
		t.Fatalf("%v", h)
	}

	h = http.Header{}
	Headers(h, Identity{OrgID: "org_1"}, "/api/llm/o/v1/responses", "gpt-5.5", "s")
	if h.Get("X-Api-Provider") != "openai" || h.Get("OpenAI-Platform") == "" || h.Get("X-Factory-Org-Id") != "org_1" {
		t.Fatalf("%v", h)
	}

	h = http.Header{}
	Headers(h, Identity{}, "/api/llm/a/v1/messages", "minimax-m2.7", "s")
	if h.Get("X-Api-Provider") != "fireworks" || h.Get("X-Api-Key") != "placeholder" {
		t.Fatalf("an Anthropic-wire model of another vendor: %v", h)
	}

	// A model droid does not offer names its route's vendor.
	for path, want := range map[string]string{"/api/llm/o/v1/chat/completions": "openai", "/api/llm/a/v1/messages": "anthropic"} {
		if got := Upstream(path, "something-new"); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[45][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestSessionIDsAreStableUUIDsOfOneInstall(t *testing.T) {
	a := SessionID("install-1", "7")
	if a != SessionID("install-1", "7") {
		t.Fatal("one conversation has two session ids")
	}
	if a == SessionID("install-1", "8") || a == SessionID("install-2", "7") {
		t.Fatal("a session id is shared")
	}
	for _, id := range []string{a, newUUID()} {
		if !uuidPattern.MatchString(id) {
			t.Fatalf("%q is not a UUID", id)
		}
	}
}

// Every model of the table is one droid's registry has: the excerpts of droid's binary upstream keeps.
func TestTheModelsAreDroids(t *testing.T) {
	var registry strings.Builder
	for _, f := range []string{"droid-0.240.0-models.txt", "droid-0.241.1-gemini.txt", "droid-0.251.0-deepseek-1017.txt", "droid-0.240.0-fast.txt"} {
		b, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		registry.Write(b)
	}
	seen := map[string]bool{}
	for _, m := range Models() {
		if seen[m.ID] {
			t.Fatalf("%s is in the table twice", m.ID)
		}
		seen[m.ID] = true
		if !strings.Contains(registry.String(), `"`+m.ID+`"`) {
			t.Errorf("%s is not in droid's registry", m.ID)
		}
		switch m.Wire {
		case WireAnthropic, WireResponses, WireChat, WireGemini:
		default:
			t.Errorf("%s: wire %q", m.ID, m.Wire)
		}
		if got, ok := ModelByID(m.ID); !ok || got != m {
			t.Errorf("ModelByID(%s)", m.ID)
		}
	}
	if len(Models()) != 47 {
		t.Fatalf("%d models, want upstream's 47", len(Models()))
	}
}
