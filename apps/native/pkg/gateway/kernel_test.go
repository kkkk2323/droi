package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

const kernelToken = "kernel-launch-token"

// standInKernel echoes every frame back unchanged and records the headers each socket was dialled with.
type standInKernel struct {
	srv *httptest.Server

	mu       sync.Mutex
	headers  []http.Header
	received []string
	sockets  []*websocket.Conn
}

func startStandInKernel(t *testing.T) *standInKernel {
	t.Helper()
	k := &standInKernel{}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" || r.Header.Get("Authorization") != "Bearer "+kernelToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		k.mu.Lock()
		k.headers = append(k.headers, r.Header.Clone())
		k.sockets = append(k.sockets, c)
		k.mu.Unlock()
		for {
			typ, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			k.mu.Lock()
			k.received = append(k.received, string(data))
			k.mu.Unlock()
			if c.Write(context.Background(), typ, data) != nil {
				return
			}
		}
	}))
	t.Cleanup(func() {
		k.mu.Lock()
		for _, s := range k.sockets {
			_ = s.CloseNow()
		}
		k.mu.Unlock()
		k.srv.Close()
	})
	return k
}

func (k *standInKernel) url() string { return "ws" + strings.TrimPrefix(k.srv.URL, "http") + "/rpc" }

func (k *standInKernel) closeAll() {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, s := range k.sockets {
		_ = s.Close(websocket.StatusNormalClosure, "")
	}
}

func kernelOptions(d *standInDaemon, k *standInKernel) Options {
	opts := baseOptions(d)
	opts.KernelURL = func() string { return "" }
	if k != nil {
		opts.KernelURL = k.url
	}
	opts.KernelToken = func() string { return kernelToken }
	return opts
}

func kernelSocketURL(gatewayURL, token string) string {
	return "ws" + strings.TrimPrefix(gatewayURL, "http") + KernelPath + "?" + TokenQuery + "=" + url.QueryEscape(token)
}

func TestAKernelFrameCrossesTheGatewayUnchangedBothWays(t *testing.T) {
	k := startStandInKernel(t)
	g := startGateway(t, kernelOptions(startStandInDaemon(t), k))
	c := connect(t, kernelSocketURL(g.URL(), testToken))
	// What the Daemon's bridge rewrites must cross untouched: the placeholder, a Session start, odd spacing and
	// key order, and a line separator.
	frame := `{"method" : "daemon.initialize_session","params":{"apiKey":"` + APIKeyPlaceholder + `","z":1,"a":"\u2028"},"jsonrpc":"2.0","id":"1"}`
	send(t, c, frame)
	if got := nextMessage(t, c); got != frame {
		t.Fatalf("the Client got %q", got)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.received) != 1 || k.received[0] != frame {
		t.Fatalf("the Kernel got %q", k.received)
	}
	h := k.headers[0]
	if h.Get("Authorization") != "Bearer "+kernelToken || h.Get(kernel.ClientHeader) != kernel.ClientPhone {
		t.Fatalf("the Kernel was dialled with %v", h)
	}
}

func TestTheKernelSocketTakesThePairingTokenAndRefusesAWrongOne(t *testing.T) {
	g := startGateway(t, kernelOptions(nil, startStandInKernel(t)))
	err := dialError(t, kernelSocketURL(g.URL(), "wrong"))
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("a wrong token: %v", err)
	}
	for token, want := range map[string]int{testToken: http.StatusNoContent, localToken: http.StatusNoContent, "wrong": http.StatusUnauthorized} {
		res, err := freshClient.Get(g.URL() + KernelPath + "?" + TokenQuery + "=" + url.QueryEscape(token))
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("the check with %q answered %d, want %d", token, res.StatusCode, want)
		}
	}
}

func TestAKernelThatIsNotRunningIsUnavailable(t *testing.T) {
	for name, opts := range map[string]Options{
		"stopped":   kernelOptions(startStandInDaemon(t), nil),
		"no kernel": baseOptions(startStandInDaemon(t)),
		"no token": func() Options {
			o := kernelOptions(nil, startStandInKernel(t))
			o.KernelToken = func() string { return "" }
			return o
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			g := startGateway(t, opts)
			err := dialError(t, kernelSocketURL(g.URL(), testToken))
			if !strings.Contains(err.Error(), "503") {
				t.Fatalf("%v", err)
			}
			eventually(t, func() bool {
				g.mu.Lock()
				defer g.mu.Unlock()
				return len(g.bridges) == 0
			})
		})
	}
}

func TestAKernelThatGoesAwayClosesThePhonesSocket(t *testing.T) {
	k := startStandInKernel(t)
	g := startGateway(t, kernelOptions(nil, k))
	c := connect(t, kernelSocketURL(g.URL(), testToken))
	send(t, c, "{}")
	_ = nextMessage(t, c)
	k.closeAll()
	waitClosed(t, c)
	eventually(t, func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return len(g.bridges) == 0
	})
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRevokingThePairingTokenEndsAKernelBridge(t *testing.T) {
	g := startGateway(t, kernelOptions(nil, startStandInKernel(t)))
	c := connect(t, kernelSocketURL(g.URL(), testToken))
	send(t, c, "{}")
	_ = nextMessage(t, c)
	g.Revoke(TokenPairing)
	waitClosed(t, c)
}

func TestMetaSaysWhetherTheComputerRunsAKernel(t *testing.T) {
	for want, opts := range map[bool]Options{true: kernelOptions(nil, nil), false: baseOptions(nil)} {
		g := startGateway(t, opts)
		res, err := freshClient.Get(g.URL() + MetaPath)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		var meta Meta
		if err := json.Unmarshal(body, &meta); err != nil || meta.Kernel != want {
			t.Fatalf("kernel %v: %s %v", want, body, err)
		}
		if strings.Contains(string(body), kernelToken) {
			t.Fatal("the Kernel's token in /meta")
		}
	}
}

type fakeWorkspaces struct {
	added []string
}

func (f *fakeWorkspaces) Inspect(_ context.Context, path string) (Workspace, error) {
	if path == "/missing" {
		return Workspace{}, &NotAWorkspace{Reason: "/missing is not a usable directory."}
	}
	return Workspace{Path: path, IsGitRepository: true, Branches: []string{"main"}, CurrentBranch: "main"}, nil
}

func (f *fakeWorkspaces) Changes(_ context.Context, path string) (GitChanges, error) {
	if path == "/broken" {
		return GitChanges{}, errors.New("git failed")
	}
	return GitChanges{IsGitRepository: true, Branch: "main"}, nil
}

func (f *fakeWorkspaces) AddWorktree(_ context.Context, path, base, prompt string) (WorktreeMade, error) {
	f.added = append(f.added, path+"|"+base+"|"+prompt)
	return WorktreeMade{Path: path + "-tree", Branch: "droi/x", RepoRoot: path}, nil
}

func TestTheWorkspaceEndpointsAnswerWhatTheHostSays(t *testing.T) {
	ws := &fakeWorkspaces{}
	opts := kernelOptions(nil, nil)
	opts.Workspaces = ws
	g := startGateway(t, opts)
	ask := func(method, endpoint, token string, q url.Values) (int, string) {
		t.Helper()
		q.Set(TokenQuery, token)
		req, _ := http.NewRequest(method, g.URL()+endpoint+"?"+q.Encode(), nil)
		res, err := freshClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if s, _ := ask("GET", WorkspacePath, "wrong", url.Values{"path": {"/r"}}); s != 401 {
		t.Errorf("a wrong token: %d", s)
	}
	if s, body := ask("GET", WorkspacePath, testToken, url.Values{"path": {"/r"}}); s != 200 || !strings.Contains(body, `"currentBranch":"main"`) {
		t.Errorf("a workspace: %d %s", s, body)
	}
	if s, body := ask("GET", WorkspacePath, testToken, url.Values{"path": {"/missing"}}); s != 400 || !strings.Contains(body, "not a usable directory") {
		t.Errorf("a missing folder: %d %s", s, body)
	}
	if s, _ := ask("GET", WorkspacePath, testToken, url.Values{}); s != 400 {
		t.Errorf("no folder: %d", s)
	}
	if s, body := ask("GET", GitChangesPath, testToken, url.Values{"path": {"/r"}}); s != 200 || !strings.Contains(body, `"files":[]`) {
		t.Errorf("changes: %d %s", s, body)
	}
	if s, _ := ask("GET", GitChangesPath, testToken, url.Values{"path": {"/broken"}}); s != 500 {
		t.Errorf("a failing git: %d", s)
	}
	if s, _ := ask("GET", WorktreePath, testToken, url.Values{"path": {"/r"}}); s != 405 {
		t.Errorf("a GET that makes a worktree: %d", s)
	}
	if s, body := ask("POST", WorktreePath, testToken, url.Values{"path": {"/r"}, "base": {"dev"}, "prompt": {"Fix it"}}); s != 201 || !strings.Contains(body, `"branch":"droi/x"`) {
		t.Errorf("a worktree: %d %s", s, body)
	}
	if len(ws.added) != 1 || ws.added[0] != "/r|dev|Fix it" {
		t.Errorf("added %q", ws.added)
	}

	plain := startGateway(t, baseOptions(nil))
	res, err := freshClient.Get(plain.URL() + WorkspacePath + "?path=/r&token=" + testToken)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("a Host without Workspaces: %d", res.StatusCode)
	}
}
