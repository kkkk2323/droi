package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeWorkstreams is the Host's side, recording what it was asked.
type fakeWorkstreams struct {
	mu    sync.Mutex
	calls []any
	fail  error
}

func (f *fakeWorkstreams) record(call any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.fail
}

func (f *fakeWorkstreams) List(context.Context) ([]Workstream, error) {
	if err := f.record("list"); err != nil {
		return nil, err
	}
	return []Workstream{{
		ID: "payments-1a2b3c", Title: "Payments", Workspace: "/w/pay", SharedFolder: "/data/workstreams/payments-1a2b3c/shared",
		OrchestratorID: "lead", AgentModel: "glm-5.3-flash", CreatedAt: "2026-11-05T10:00:00Z",
		Agents: []WorkstreamAgent{{SessionID: "a1", Title: "API", WorktreePath: "/wt/a1", Branch: "main-wt", BaseBranch: "main", CreatedAt: "2026-11-05T10:01:00Z", Awaiting: true,
			LastReport: &WorkstreamReport{Kind: "done", Text: "Moved /pay", At: "2026-11-05T10:05:00Z"}}},
	}, {ID: "empty", Title: "Empty"}}, nil
}

func (f *fakeWorkstreams) Create(_ context.Context, w NewWorkstream) (WorkstreamCreated, error) {
	if err := f.record(w); err != nil {
		return WorkstreamCreated{}, err
	}
	return WorkstreamCreated{WorkstreamID: "payments-1a2b3c", OrchestratorID: "lead"}, nil
}

func (f *fakeWorkstreams) SetAgentSettings(_ context.Context, s WorkstreamSettings) error {
	return f.record(s)
}

func (f *fakeWorkstreams) Archive(_ context.Context, id string) ([]string, error) {
	if err := f.record("archive " + id); err != nil {
		return nil, err
	}
	return []string{"/wt/a1"}, nil
}

func workstreamsGateway(t *testing.T) (*Gateway, *Client, *fakeWorkstreams) {
	ws := &fakeWorkstreams{}
	g, c := startClientGateway(t, func(o *Options) { o.Workstreams = ws })
	return g, c, ws
}

func workstreamURL(gatewayURL, token, endpoint string, q url.Values) string {
	if q == nil {
		q = url.Values{}
	}
	q.Set(TokenQuery, token)
	return gatewayURL + endpoint + "?" + q.Encode()
}

func do(t *testing.T, method, u, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, u, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAClientListsStartsChangesAndArchivesWorkstreams(t *testing.T) {
	_, c, ws := workstreamsGateway(t)
	ctx := t.Context()
	list, err := c.Workstreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Agents[0].LastReport.Kind != "done" || list[0].SharedFolder == "" || list[1].Agents == nil {
		t.Fatalf("%+v", list)
	}
	in := NewWorkstream{Title: "Payments", Workspace: "/w/pay", Goal: "Move to gRPC.", Model: "claude-opus-4-1", ReasoningEffort: "high", AgentModel: "glm-5.3-flash"}
	made, err := c.CreateWorkstream(ctx, in)
	if err != nil || made != (WorkstreamCreated{WorkstreamID: "payments-1a2b3c", OrchestratorID: "lead"}) {
		t.Fatal(made, err)
	}
	set := WorkstreamSettings{ID: "payments-1a2b3c", AgentModel: "gpt-6", AgentReasoningEffort: "low"}
	if err := c.SetWorkstreamAgentSettings(ctx, set); err != nil {
		t.Fatal(err)
	}
	kept, err := c.ArchiveWorkstream(ctx, "payments-1a2b3c")
	if err != nil || !reflect.DeepEqual(kept, []string{"/wt/a1"}) {
		t.Fatal(kept, err)
	}
	if want := []any{"list", in, set, "archive payments-1a2b3c"}; !reflect.DeepEqual(ws.calls, want) {
		t.Fatalf("calls %#v", ws.calls)
	}
}

func TestWorkstreamRequestsNeedATokenTheRightMethodAndAWholeRequest(t *testing.T) {
	g, _, ws := workstreamsGateway(t)
	if status, _ := do(t, "GET", workstreamURL(g.URL(), "nope", WorkstreamsPath, nil), ""); status != 401 {
		t.Fatal(status)
	}
	if status, _ := do(t, "POST", workstreamURL(g.URL(), testToken, WorkstreamsPath, nil), ""); status != 405 {
		t.Fatal(status)
	}
	if status, _ := do(t, "GET", workstreamURL(g.URL(), testToken, WorkstreamCreatePath, nil), ""); status != 405 {
		t.Fatal(status)
	}
	if status, body := do(t, "POST", workstreamURL(g.URL(), testToken, WorkstreamCreatePath, nil), "not json"); status != 400 || body != `{"error":"The request is not JSON"}` {
		t.Fatal(status, body)
	}
	if status, _ := do(t, "POST", workstreamURL(g.URL(), testToken, WorkstreamSettingsPath, nil), `{"agentModel":"x"}`); status != 400 {
		t.Fatal(status)
	}
	if status, _ := do(t, "POST", workstreamURL(g.URL(), localToken, WorkstreamArchivePath, nil), ""); status != 400 {
		t.Fatal(status)
	}
	big := `{"title":"T","workspace":"/w","goal":"` + strings.Repeat("x", maxWorkstreamBody) + `"}`
	if status, body := do(t, "POST", workstreamURL(g.URL(), testToken, WorkstreamCreatePath, nil), big); status != 413 || body != `{"error":"The request is too large"}` {
		t.Fatal(status, body)
	}
	if len(ws.calls) != 0 {
		t.Fatal(ws.calls)
	}
	status, body := do(t, "POST", workstreamURL(g.URL(), testToken, WorkstreamCreatePath, nil), `{"title":"T","workspace":"/w","goal":"G"}`)
	if status != 201 || body != `{"workstreamId":"payments-1a2b3c","orchestratorId":"lead"}` {
		t.Fatal(status, body)
	}
	status, body = do(t, "POST", workstreamURL(g.URL(), testToken, WorkstreamArchivePath, url.Values{WorkstreamIDQuery: {"w"}}), "")
	if status != 200 || body != `{"keptWorktrees":["/wt/a1"]}` {
		t.Fatal(status, body)
	}
}

func TestARefusedWorkstreamRequestAnswers400WithTheReasonAnyOtherFailure500(t *testing.T) {
	_, c, ws := workstreamsGateway(t)
	ws.fail = &RefusedRequest{Reason: "Enter the goal."}
	_, err := c.CreateWorkstream(t.Context(), NewWorkstream{Title: "T"})
	var status *StatusError
	if !errors.As(err, &status) || status.Status != 400 || status.Message != "Enter the goal." {
		t.Fatalf("%#v", err)
	}
	ws.fail = errors.New("the Daemon is not running")
	_, err = c.ArchiveWorkstream(t.Context(), "w")
	if !errors.As(err, &status) || status.Status != 500 || status.Message != "the Daemon is not running" {
		t.Fatalf("%#v", err)
	}
}

func TestAGatewayWithoutWorkstreamsIs404(t *testing.T) {
	_, c := startClientGateway(t, nil)
	if _, err := c.Workstreams(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := c.CreateWorkstream(t.Context(), NewWorkstream{}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
