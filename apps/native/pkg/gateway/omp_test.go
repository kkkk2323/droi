package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeOMP struct {
	available bool
	sessions  map[string]OMPSession
	messages  map[string][]OMPMessage
	models    map[string][]OMPModel
	prompts   []string
	aborts    []string
	setModels []string
}

func (f *fakeOMP) Available() bool { return f.available }

func (f *fakeOMP) List() []OMPSession {
	out := make([]OMPSession, 0, len(f.sessions))
	for _, s := range f.sessions {
		out = append(out, s)
	}
	return out
}

func (f *fakeOMP) Create(ctx context.Context, cwd, model, title string) (OMPSession, error) {
	id := "omp-1"
	s := OMPSession{ID: id, Title: title, Cwd: cwd, Model: model, Engine: "omp", UpdatedAt: 1}
	if f.sessions == nil {
		f.sessions = map[string]OMPSession{}
	}
	f.sessions[id] = s
	return s, nil
}

func (f *fakeOMP) Get(id string) (OMPSession, []OMPMessage, error) {
	s, ok := f.sessions[id]
	if !ok {
		return OMPSession{}, nil, errors.New("session not found")
	}
	return s, f.messages[id], nil
}

func (f *fakeOMP) Prompt(ctx context.Context, id, message string) error {
	if _, ok := f.sessions[id]; !ok {
		return errors.New("session not found")
	}
	f.prompts = append(f.prompts, id+":"+message)
	return nil
}

func (f *fakeOMP) Abort(ctx context.Context, id string) error {
	f.aborts = append(f.aborts, id)
	return nil
}

func (f *fakeOMP) Models(ctx context.Context, id string) ([]OMPModel, error) {
	return f.models[id], nil
}

func (f *fakeOMP) SetModel(ctx context.Context, id, modelID string) error {
	s, ok := f.sessions[id]
	if !ok {
		return errors.New("session not found")
	}
	s.Model = modelID
	f.sessions[id] = s
	f.setModels = append(f.setModels, modelID)
	return nil
}

func (f *fakeOMP) Subscribe(ctx context.Context, id string, write func([]byte) error) error {
	b, _ := json.Marshal(map[string]any{"type": "snapshot", "session": f.sessions[id]})
	if err := write(b); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestOMPSessionsRequireToken(t *testing.T) {
	f := &fakeOMP{available: true}
	g := startGateway(t, withOMP(f))
	resp, err := http.Get(g.URL() + OMPSessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestOMPListCreatePromptAndModels(t *testing.T) {
	f := &fakeOMP{
		available: true,
		sessions: map[string]OMPSession{
			"s1": {ID: "s1", Title: "hi", Cwd: "/tmp", Engine: "omp"},
		},
		messages: map[string][]OMPMessage{
			"s1": {{ID: "m1", Role: "user", Text: "hello"}},
		},
		models: map[string][]OMPModel{
			"s1": {{ID: "gpt", Name: "GPT", Provider: "openai"}},
		},
	}
	g := startGateway(t, withOMP(f))
	tok := testToken

	resp, err := http.Get(ompURL(g.URL(), OMPSessionsPath, tok, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	var list struct {
		Available bool         `json:"available"`
		Sessions  []OMPSession `json:"sessions"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if !list.Available || len(list.Sessions) != 1 || list.Sessions[0].ID != "s1" {
		t.Fatalf("%+v", list)
	}

	body := strings.NewReader(`{"cwd":"/work","model":"m","title":"t"}`)
	req, _ := http.NewRequest(http.MethodPost, ompURL(g.URL(), OMPSessionsPath, tok, ""), body)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	var created OMPSession
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if created.ID == "" || created.Cwd != "/work" {
		t.Fatalf("%+v", created)
	}

	resp, err = http.Get(ompURL(g.URL(), OMPSessionPath, tok, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Session  OMPSession   `json:"session"`
		Messages []OMPMessage `json:"messages"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Session.ID != "s1" || len(got.Messages) != 1 || got.Messages[0].Text != "hello" {
		t.Fatalf("%+v", got)
	}

	req, _ = http.NewRequest(http.MethodPost, ompURL(g.URL(), OMPPromptPath, tok, ""),
		strings.NewReader(`{"sessionId":"s1","message":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || len(f.prompts) != 1 {
		t.Fatalf("prompt %d %+v", resp.StatusCode, f.prompts)
	}

	resp, err = http.Get(ompURL(g.URL(), OMPModelsPath, tok, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var models struct {
		Models []OMPModel `json:"models"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&models)
	if len(models.Models) != 1 || models.Models[0].ID != "gpt" {
		t.Fatalf("%+v", models)
	}

	req, _ = http.NewRequest(http.MethodPost, ompURL(g.URL(), OMPModelPath, tok, ""),
		strings.NewReader(`{"sessionId":"s1","modelId":"gpt"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || f.sessions["s1"].Model != "gpt" {
		t.Fatalf("set model %d %+v", resp.StatusCode, f.sessions["s1"])
	}
}

func TestMetaReportsOMPAvailable(t *testing.T) {
	g := startGateway(t, withOMP(&fakeOMP{available: true}))
	resp, err := http.Get(g.URL() + MetaPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got Meta
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if !got.OMPAvailable {
		t.Fatalf("%+v", got)
	}
}

func TestMetaReportsOMPUnavailable(t *testing.T) {
	g := startGateway(t, withOMP(&fakeOMP{available: false}))
	resp, err := http.Get(g.URL() + MetaPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ompAvailable":false`) {
		t.Fatalf("%s", b)
	}
}

func TestOMPEventsStream(t *testing.T) {
	f := &fakeOMP{
		available: true,
		sessions:  map[string]OMPSession{"s1": {ID: "s1", Title: "t", Cwd: "/"}},
	}
	g := startGateway(t, withOMP(f))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ompURL(g.URL(), OMPEventsPath, testToken, "s1"), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), "snapshot") {
		t.Fatalf("%q", buf[:n])
	}
}

func withOMP(f OMPAccess) Options {
	opts := baseOptions(nil)
	opts.OMP = f
	return opts
}

func ompURL(base, path, token, sessionID string) string {
	u := base + path + "?" + TokenQuery + "=" + token
	if sessionID != "" {
		u += "&" + SessionIDQuery + "=" + sessionID
	}
	return u
}
