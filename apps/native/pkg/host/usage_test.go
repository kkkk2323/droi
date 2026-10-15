package host

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const limitsBody = `{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":0,"windowEnd":null},"weekly":{"usedPercent":12,"windowEnd":"2099-10-15T09:24:36.485Z"},"monthly":{"usedPercent":38,"windowEnd":"2099-10-19T14:30:36.292Z"}},"core":{"fiveHour":{"usedPercent":0,"windowEnd":null},"weekly":{"usedPercent":0,"windowEnd":null},"monthly":{"usedPercent":0,"windowEnd":null}}},"overagePreference":"droidCore","extraUsageBalanceCents":250}`

// usageServer is Factory (and WorkOS under /workos) as the Host reaches
// them; it records what each billing request carried.
type usageServer struct {
	*httptest.Server
	mu       sync.Mutex
	asked    []http.Header
	renewals int
	status   int
}

func newUsageServer(t *testing.T) *usageServer {
	s := &usageServer{status: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch r.URL.Path {
		case "/api/billing/limits":
			s.asked = append(s.asked, r.Header.Clone())
			w.WriteHeader(s.status)
			w.Write([]byte(limitsBody))
		case "/workos/authenticate":
			r.ParseForm()
			if r.Form.Get("refresh_token") != "rb" || r.Form.Get("organization_id") != "org_b" {
				t.Errorf("renewal form %v", r.Form)
			}
			s.renewals++
			w.Write([]byte(`{"access_token":"` + jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b", "exp": float64(time.Now().Add(time.Hour).Unix())}) + `","refresh_token":"rb2"}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// usageHost is accountsHost pointed at s, with a droid that is version
// 0.236.0 and was compiled with Bun 1.4.2.
func usageHost(t *testing.T, s *usageServer) *Host {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	h, _ := accountsHost(t)
	droidPath := filepath.Join(t.TempDir(), "droid")
	os.WriteFile(droidPath, []byte("#!/bin/sh\n# Bun/1.4.2 (darwin)\necho 0.236.0\n"), 0o700)
	h.Settings.Update(func(st *Settings) {
		st.DroidPath = OptString(droidPath)
		st.FactoryAPIBaseURL = OptString(s.URL)
	})
	h.adding.WorkOS = s.URL + "/workos"
	return h
}

func waitUsage(t *testing.T, h *Host, id string) UsageState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st := h.AccountUsage(id); !st.Fetching && (st.Usage != nil || st.Err != "") {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatal("no usage")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUsageIsAskedTheWayDroidAsks(t *testing.T) {
	s := newUsageServer(t)
	h := usageHost(t, s)
	h.RefreshUsage("")
	st := waitUsage(t, h, "")
	if st.Err != "" {
		t.Fatal(st.Err)
	}
	u := st.Usage
	if u.Standard.Monthly.Percent != 38 || u.Standard.Weekly.Percent != 12 || !u.Standard.FiveHour.Ends.IsZero() || u.Overage != "droidCore" || u.ExtraCents != 250 {
		t.Fatalf("%+v", u)
	}
	if w, ok := u.Standard.Highest(time.Now()); !ok || w.Percent != 38 {
		t.Fatalf("highest %+v %v", w, ok)
	}
	got := s.asked[0]
	for k, want := range map[string]string{"X-Factory-Client": "cli", "X-Client-Version": "0.236.0", "User-Agent": "Bun/1.4.2", "Accept": "*/*", "X-Factory-Org-Id": "org_a"} {
		if got.Get(k) != want {
			t.Errorf("%s: %q, want %q", k, got.Get(k), want)
		}
	}
	if !strings.HasPrefix(got.Get("Authorization"), "Bearer h.") {
		t.Errorf("Authorization %q", got.Get("Authorization"))
	}
	// Asked again within the minute, it does not go out again.
	h.RefreshUsage("")
	time.Sleep(50 * time.Millisecond)
	if len(s.asked) != 1 {
		t.Fatalf("%d requests", len(s.asked))
	}
}

func TestUsageSaysWhyItIsMissing(t *testing.T) {
	s := newUsageServer(t)
	s.status = http.StatusForbidden
	h := usageHost(t, s)
	h.RefreshUsage("")
	if st := waitUsage(t, h, ""); st.Err != "Factory answered 403." || st.Usage != nil {
		t.Fatalf("%+v", st)
	}
}

// An added account no one runs has its expired login renewed by Droi; the
// one in use is left to the Daemon.
func TestUsageRenewsTheLoginOfAnAccountNotInUse(t *testing.T) {
	s := newUsageServer(t)
	h := usageHost(t, s)
	expired := jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b", "exp": float64(time.Now().Add(-time.Hour).Unix())})
	if err := h.addAccount(expired, "rb"); err != nil {
		t.Fatal(err)
	}
	id := h.Accounts()[1].ID
	h.RefreshUsage(id)
	if st := waitUsage(t, h, id); st.Err != "" {
		t.Fatal(st.Err)
	}
	if s.renewals != 1 || s.asked[0].Get("Authorization") == "Bearer "+expired {
		t.Fatalf("renewals %d, sent %q", s.renewals, s.asked[0].Get("Authorization"))
	}
	if l := h.cli(h.homeOf(id)).Read(); l == nil || l.RefreshToken != "rb2" {
		t.Fatalf("the renewed login was not kept: %+v", l)
	}

	// In use, an expired login waits for the Daemon.
	h.Settings.Update(func(st *Settings) { st.ActiveAccount = id })
	WriteCliLogin(h.homeOf(id), expired, "rb")
	h.usage = map[string]*usageEntry{}
	h.RefreshUsage(id)
	if st := waitUsage(t, h, id); st.Err != "The login is being renewed." || s.renewals != 1 {
		t.Fatalf("%+v, renewals %d", st, s.renewals)
	}
}
