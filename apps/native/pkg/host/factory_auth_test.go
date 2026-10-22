package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// orgServer is WorkOS (under /workos) and Factory's /api/cli/org, for a
// user whose device-flow token comes without an organization.
type orgServer struct {
	*httptest.Server
	mu        sync.Mutex
	orgs      string // the /api/cli/org answer
	orgAsked  int
	refreshes []string // organization_id of each refresh
}

func newOrgServer(t *testing.T, deviceToken, orgs string) *orgServer {
	s := &orgServer{orgs: orgs}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		r.ParseForm()
		switch {
		case r.URL.Path == "/workos/authorize/device":
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_uri":"https://x","expires_in":60,"interval":1}`))
		case r.URL.Path == "/workos/authenticate" && r.Form.Get("grant_type") == deviceGrant:
			w.Write([]byte(`{"access_token":"` + deviceToken + `","refresh_token":"r1"}`))
		case r.URL.Path == "/workos/authenticate" && r.Form.Get("grant_type") == "refresh_token":
			if r.Form.Get("refresh_token") != "r1" {
				t.Errorf("refresh form %v", r.Form)
			}
			s.refreshes = append(s.refreshes, r.Form.Get("organization_id"))
			w.Write([]byte(`{"access_token":"` + jwt(map[string]any{"sub": "u", "org_id": "org_w", "external_org_id": "fs_w"}) + `","refresh_token":"r2"}`))
		case r.URL.Path == "/api/cli/org":
			if r.Header.Get("Authorization") != "Bearer "+deviceToken {
				t.Errorf("/api/cli/org authorization %q", r.Header.Get("Authorization"))
			}
			s.orgAsked++
			w.Write([]byte(s.orgs))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// signIn runs the device flow against s and returns the login handed over.
func signIn(t *testing.T, s *orgServer) (access, refresh string) {
	t.Helper()
	type login struct{ access, refresh string }
	got := make(chan login, 1)
	a := &FactoryAuth{WorkOS: s.URL + "/workos", FactoryAPIBaseURL: s.URL, Handoff: func(access, refresh string) error {
		got <- login{access, refresh}
		return nil
	}}
	if _, err := a.SignIn(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case l := <-got:
		return l.access, l.refresh
	case <-time.After(10 * time.Second):
		t.Fatal("no login was handed over")
	}
	return "", ""
}

func TestASignInWithoutAnOrganizationIsRefreshedIntoTheUsersOrganization(t *testing.T) {
	s := newOrgServer(t, jwt(map[string]any{"sub": "u", "email": "a@x.dev"}), `{"workosOrgIds":["org_w"]}`)
	access, refresh := signIn(t, s)
	if DecodeJWT(access)["external_org_id"] != "fs_w" || refresh != "r2" {
		t.Fatalf("handed over %v, %q", DecodeJWT(access), refresh)
	}
	if len(s.refreshes) != 1 || s.refreshes[0] != "org_w" {
		t.Fatalf("refreshed into %v", s.refreshes)
	}
}

func TestASignInWithoutAnOrganizationIsKeptWhenFactoryNamesNone(t *testing.T) {
	device := jwt(map[string]any{"sub": "u", "email": "a@x.dev"})
	s := newOrgServer(t, device, `{"workosOrgIds":[]}`)
	access, refresh := signIn(t, s)
	if access != device || refresh != "r1" || len(s.refreshes) != 0 {
		t.Fatalf("handed over %v, %q after refreshes %v", DecodeJWT(access), refresh, s.refreshes)
	}
}

func TestASignInWithAnOrganizationIsNotAskedAboutIt(t *testing.T) {
	device := jwt(map[string]any{"sub": "u", "org_id": "org_w", "external_org_id": "fs_w"})
	s := newOrgServer(t, device, `{"workosOrgIds":["org_w"]}`)
	access, _ := signIn(t, s)
	if access != device || s.orgAsked != 0 || len(s.refreshes) != 0 {
		t.Fatalf("asked %d times, refreshed %v", s.orgAsked, s.refreshes)
	}
}
