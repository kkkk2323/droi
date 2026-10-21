package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// OMP HTTP surface beside /daemon. The Host injects live session access;
// phones use these instead of a full Daemon protocol shim.

const (
	OMPSessionsPath = "/omp/sessions"
	OMPSessionPath  = "/omp/session"
	OMPPromptPath   = "/omp/session/prompt"
	OMPAbortPath    = "/omp/session/abort"
	OMPModelsPath   = "/omp/session/models"
	OMPModelPath    = "/omp/session/model"
	OMPEventsPath   = "/omp/session/events"
)

// OMPSession is one live OMP session as Gateway exposes it.
type OMPSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Model     string `json:"model,omitempty"`
	Working   bool   `json:"working"`
	Engine    string `json:"engine"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// OMPMessage is a transcript line for phones.
type OMPMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Streaming bool   `json:"streaming,omitempty"`
	ToolName  string `json:"toolName,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
	ToolArgs  string `json:"toolArgs,omitempty"`
	ToolOut   string `json:"toolOut,omitempty"`
	IsError   bool   `json:"isError,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// OMPModel is one model OMP can run.
type OMPModel struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// OMPAccess is what the Host gives the Gateway for OMP.
type OMPAccess interface {
	Available() bool
	List() []OMPSession
	Create(ctx context.Context, cwd, model, title string) (OMPSession, error)
	Get(id string) (OMPSession, []OMPMessage, error)
	Prompt(ctx context.Context, id, message string) error
	Abort(ctx context.Context, id string) error
	Models(ctx context.Context, id string) ([]OMPModel, error)
	SetModel(ctx context.Context, id, modelID string) error
	// Subscribe pushes event frames until ctx ends. Each write is one JSON object.
	Subscribe(ctx context.Context, id string, write func([]byte) error) error
}

func (g *Gateway) answerOMP(w http.ResponseWriter, r *http.Request, path string) {
	if g.opts.OMP == nil {
		reply(w, http.StatusNotFound, nil)
		return
	}
	if g.classify(r.URL.Query().Get(TokenQuery)) == "" {
		reply(w, http.StatusUnauthorized, nil)
		return
	}
	switch path {
	case OMPSessionsPath:
		g.ompSessions(w, r)
	case OMPSessionPath:
		g.ompSession(w, r)
	case OMPPromptPath:
		g.ompPrompt(w, r)
	case OMPAbortPath:
		g.ompAbort(w, r)
	case OMPModelsPath:
		g.ompModels(w, r)
	case OMPModelPath:
		g.ompSetModel(w, r)
	case OMPEventsPath:
		g.ompEvents(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (g *Gateway) ompSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list := g.opts.OMP.List()
		if list == nil {
			list = []OMPSession{}
		}
		reply(w, http.StatusOK, map[string]any{
			"available": g.opts.OMP.Available(),
			"sessions":  list,
		})
	case http.MethodPost:
		var body struct {
			Cwd   string `json:"cwd"`
			Model string `json:"model"`
			Title string `json:"title"`
		}
		if err := decodeJSONBody(r, &body); err != nil {
			reply(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}
		if strings.TrimSpace(body.Cwd) == "" {
			reply(w, http.StatusBadRequest, errorBody{Error: "cwd required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		sess, err := g.opts.OMP.Create(ctx, body.Cwd, body.Model, body.Title)
		if err != nil {
			reply(w, http.StatusBadGateway, errorBody{Error: err.Error()})
			return
		}
		reply(w, http.StatusCreated, sess)
	default:
		reply(w, http.StatusMethodNotAllowed, nil)
	}
}

func (g *Gateway) ompSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	id := r.URL.Query().Get(SessionIDQuery)
	if id == "" {
		reply(w, http.StatusBadRequest, errorBody{Error: "sessionId required"})
		return
	}
	sess, msgs, err := g.opts.OMP.Get(id)
	if err != nil {
		reply(w, http.StatusNotFound, errorBody{Error: err.Error()})
		return
	}
	if msgs == nil {
		msgs = []OMPMessage{}
	}
	reply(w, http.StatusOK, map[string]any{
		"session":  sess,
		"messages": msgs,
	})
}

func (g *Gateway) ompPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		Message   string `json:"message"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		reply(w, http.StatusBadRequest, errorBody{Error: err.Error()})
		return
	}
	if body.SessionID == "" || strings.TrimSpace(body.Message) == "" {
		reply(w, http.StatusBadRequest, errorBody{Error: "sessionId and message required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := g.opts.OMP.Prompt(ctx, body.SessionID, body.Message); err != nil {
		reply(w, http.StatusBadGateway, errorBody{Error: err.Error()})
		return
	}
	reply(w, http.StatusNoContent, nil)
}

func (g *Gateway) ompAbort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		reply(w, http.StatusBadRequest, errorBody{Error: err.Error()})
		return
	}
	if body.SessionID == "" {
		reply(w, http.StatusBadRequest, errorBody{Error: "sessionId required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := g.opts.OMP.Abort(ctx, body.SessionID); err != nil {
		reply(w, http.StatusBadGateway, errorBody{Error: err.Error()})
		return
	}
	reply(w, http.StatusNoContent, nil)
}

func (g *Gateway) ompModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	id := r.URL.Query().Get(SessionIDQuery)
	if id == "" {
		reply(w, http.StatusBadRequest, errorBody{Error: "sessionId required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	models, err := g.opts.OMP.Models(ctx, id)
	if err != nil {
		reply(w, http.StatusBadGateway, errorBody{Error: err.Error()})
		return
	}
	if models == nil {
		models = []OMPModel{}
	}
	reply(w, http.StatusOK, map[string]any{"models": models})
}

func (g *Gateway) ompSetModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		ModelID   string `json:"modelId"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		reply(w, http.StatusBadRequest, errorBody{Error: err.Error()})
		return
	}
	if body.SessionID == "" || body.ModelID == "" {
		reply(w, http.StatusBadRequest, errorBody{Error: "sessionId and modelId required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := g.opts.OMP.SetModel(ctx, body.SessionID, body.ModelID); err != nil {
		reply(w, http.StatusBadGateway, errorBody{Error: err.Error()})
		return
	}
	reply(w, http.StatusNoContent, nil)
}

func (g *Gateway) ompEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		reply(w, http.StatusMethodNotAllowed, nil)
		return
	}
	id := r.URL.Query().Get(SessionIDQuery)
	if id == "" {
		reply(w, http.StatusBadRequest, errorBody{Error: "sessionId required"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		reply(w, http.StatusInternalServerError, errorBody{Error: "streaming unsupported"})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	write := func(b []byte) error {
		if _, err := w.Write([]byte("data: ")); err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := g.opts.OMP.Subscribe(ctx, id, write); err != nil && ctx.Err() == nil {
		_ = write(encodeJSON(errorBody{Error: err.Error()}))
	}
}

func decodeJSONBody(r *http.Request, dst any) error {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, dst)
}
