package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"testing"
)

type fakeSessionList struct {
	mu    sync.Mutex
	order SessionListOrder
}

func (f *fakeSessionList) Order() SessionListOrder {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.order
}

func (f *fakeSessionList) SetPinned(kind, key string, pinned bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := &f.order.PinnedSessions
	if kind == PinWorkspace {
		list = &f.order.PinnedWorkspaces
	}
	*list = slices.DeleteFunc(*list, func(k string) bool { return k == key })
	if pinned {
		*list = append(*list, key)
	}
	return nil
}

func sessionListURL(gatewayURL, token, endpoint string, q url.Values) string {
	if q == nil {
		q = url.Values{}
	}
	q.Set(TokenQuery, token)
	return gatewayURL + endpoint + "?" + q.Encode()
}

// A phone reads the window's pins and Workspace order, and pins through it.
func TestTheSessionListOrderIsSharedAndPinnedThroughTheGateway(t *testing.T) {
	list := &fakeSessionList{order: SessionListOrder{PinnedSessions: []string{"s1"}, WorkspaceOrder: []string{"/w/b", "/w/a"}, SessionSort: "created"}}
	opts := baseOptions(nil)
	opts.SessionList = list
	g := startGateway(t, opts)

	resp, err := http.Get(sessionListURL(g.URL(), testToken, SessionListPath, nil))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var got SessionListOrder
	if resp.StatusCode != 200 || json.Unmarshal(b, &got) != nil {
		t.Fatalf("GET answered %d %s", resp.StatusCode, b)
	}
	if !slices.Equal(got.WorkspaceOrder, []string{"/w/b", "/w/a"}) || got.SessionSort != "created" || got.PinnedWorkspaces == nil {
		t.Fatalf("order %s", b)
	}

	pin := func(kind, key, pinned string) int {
		resp, _ := post(t, sessionListURL(g.URL(), testToken, SessionPinPath, url.Values{PinKindQuery: {kind}, PinKeyQuery: {key}, PinnedQuery: {pinned}}))
		return resp.StatusCode
	}
	if s := pin(PinWorkspace, "/w/a", "true"); s != 204 {
		t.Fatalf("pin answered %d", s)
	}
	if s := pin(PinSession, "s1", "false"); s != 204 {
		t.Fatalf("unpin answered %d", s)
	}
	if o := list.Order(); !slices.Equal(o.PinnedWorkspaces, []string{"/w/a"}) || len(o.PinnedSessions) != 0 {
		t.Fatalf("pins %+v", o)
	}
	if s := pin("folder", "x", "true"); s != 400 {
		t.Fatalf("a bad kind answered %d", s)
	}
	if resp, _ := post(t, sessionListURL(g.URL(), "nope", SessionPinPath, nil)); resp.StatusCode != 401 {
		t.Fatalf("a bad token answered %d", resp.StatusCode)
	}

	without := startGateway(t, baseOptions(nil))
	resp, err = http.Get(sessionListURL(without.URL(), testToken, SessionListPath, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("a Host without a list answered %d", resp.StatusCode)
	}
}
