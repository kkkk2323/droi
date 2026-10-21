package host

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/omp"
)

// OMPGateway adapts OMPManager to gateway.OMPAccess for Remote Clients.
type OMPGateway struct {
	M *OMPManager
}

func (a OMPGateway) Available() bool {
	if a.M == nil {
		return false
	}
	return a.M.Available()
}

func (a OMPGateway) List() []gateway.OMPSession {
	if a.M == nil {
		return nil
	}
	live := a.M.LiveSummaries()
	out := make([]gateway.OMPSession, 0, len(live))
	for _, s := range live {
		h := a.M.Get(s.SessionID)
		item := gateway.OMPSession{
			ID:        s.SessionID,
			Title:     s.Title,
			Cwd:       s.Cwd,
			Engine:    OMPEngine,
			UpdatedAt: s.UpdatedAt,
		}
		if h != nil {
			item.Model = h.Model
			if h.Session != nil {
				item.Working = h.Session.Working()
				if item.Model == "" {
					item.Model = h.Session.Model()
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func (a OMPGateway) Create(ctx context.Context, cwd, model, title string) (gateway.OMPSession, error) {
	if a.M == nil {
		return gateway.OMPSession{}, errors.New("omp unavailable")
	}
	h, err := a.M.Create(ctx, OMPCreate{
		Cwd:          cwd,
		Model:        model,
		Title:        title,
		ApprovalMode: omp.ApprovalAlwaysAsk,
	})
	if err != nil {
		return gateway.OMPSession{}, err
	}
	return gateway.OMPSession{
		ID:        h.ID,
		Title:     h.Title,
		Cwd:       h.Cwd,
		Model:     h.Model,
		Engine:    OMPEngine,
		UpdatedAt: h.UpdatedAt.Unix(),
	}, nil
}

func (a OMPGateway) Get(id string) (gateway.OMPSession, []gateway.OMPMessage, error) {
	if a.M == nil {
		return gateway.OMPSession{}, nil, errors.New("omp unavailable")
	}
	h := a.M.Get(id)
	if h == nil || h.Session == nil {
		return gateway.OMPSession{}, nil, errors.New("session not found")
	}
	sess := gateway.OMPSession{
		ID:        h.ID,
		Title:     h.Title,
		Cwd:       h.Cwd,
		Model:     h.Model,
		Working:   h.Session.Working(),
		Engine:    OMPEngine,
		UpdatedAt: h.UpdatedAt.Unix(),
	}
	if sess.Model == "" {
		sess.Model = h.Session.Model()
	}
	msgs := h.Session.Messages()
	out := make([]gateway.OMPMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, gateway.OMPMessage{
			ID:        m.ID,
			Role:      m.Role,
			Text:      m.Text,
			Thinking:  m.Thinking,
			Streaming: m.Streaming,
			ToolName:  m.ToolName,
			ToolID:    m.ToolID,
			ToolArgs:  m.ToolArgs,
			ToolOut:   m.ToolOut,
			IsError:   m.IsError,
			Timestamp: m.Timestamp,
		})
	}
	return sess, out, nil
}

func (a OMPGateway) Prompt(ctx context.Context, id, message string) error {
	h, err := a.handle(id)
	if err != nil {
		return err
	}
	if h.Session.Working() {
		return h.Session.FollowUp(ctx, message, nil)
	}
	_, err = h.Session.Prompt(ctx, message, nil)
	return err
}

func (a OMPGateway) Abort(ctx context.Context, id string) error {
	h, err := a.handle(id)
	if err != nil {
		return err
	}
	return h.Session.Abort(ctx)
}

func (a OMPGateway) Models(ctx context.Context, id string) ([]gateway.OMPModel, error) {
	h, err := a.handle(id)
	if err != nil {
		return nil, err
	}
	list, err := h.Session.GetAvailableModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.OMPModel, 0, len(list))
	for _, m := range list {
		out = append(out, gateway.OMPModel{ID: m.ID, Name: m.Name, Provider: m.Provider})
	}
	return out, nil
}

func (a OMPGateway) SetModel(ctx context.Context, id, modelID string) error {
	h, err := a.handle(id)
	if err != nil {
		return err
	}
	if err := h.Session.SetModelByID(ctx, modelID); err != nil {
		return err
	}
	h.Model = modelID
	return nil
}

func (a OMPGateway) Subscribe(ctx context.Context, id string, write func([]byte) error) error {
	h, err := a.handle(id)
	if err != nil {
		return err
	}
	// Snapshot first.
	sess, msgs, err := a.Get(id)
	if err != nil {
		return err
	}
	init, _ := json.Marshal(map[string]any{"type": "snapshot", "session": sess, "messages": msgs})
	if err := write(init); err != nil {
		return err
	}
	ch := make(chan omp.Event, 32)
	unsub := h.Session.OnEvent(func(ev omp.Event) {
		select {
		case ch <- ev:
		default:
		}
	})
	defer unsub()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			ping, _ := json.Marshal(map[string]any{"type": "ping"})
			if err := write(ping); err != nil {
				return err
			}
		case ev := <-ch:
			frame := map[string]any{"type": ev.Type}
			if ev.Message != nil {
				m := ev.Message
				frame["message"] = gateway.OMPMessage{
					ID: m.ID, Role: m.Role, Text: m.Text, Thinking: m.Thinking,
					Streaming: m.Streaming, ToolName: m.ToolName, ToolID: m.ToolID,
					ToolArgs: m.ToolArgs, ToolOut: m.ToolOut, IsError: m.IsError, Timestamp: m.Timestamp,
				}
			}
			if ev.UI != nil {
				frame["ui"] = map[string]any{"id": ev.UI.ID, "method": ev.UI.Method}
			}
			if ev.Err != nil {
				frame["error"] = ev.Err.Error()
			}
			b, _ := json.Marshal(frame)
			if err := write(b); err != nil {
				return err
			}
		}
	}
}

func (a OMPGateway) handle(id string) (*OMPHandle, error) {
	if a.M == nil {
		return nil, errors.New("omp unavailable")
	}
	h := a.M.Get(id)
	if h == nil || h.Session == nil {
		return nil, errors.New("session not found")
	}
	return h, nil
}
