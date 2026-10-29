package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// The Session Times (ADR 0032), which every Client orders its Session list
// by (packages/daemon-layer/src/gateway.ts).
const (
	// SessionTimesPath answers a sessions.TimesSnapshot to a GET: every
	// Session's Times, or with AfterQuery those changed after that
	// revision, once there is a change or longPoll has passed.
	SessionTimesPath = "/session-times"
	// SessionViewedPath notes, to a POST with SessionIDQuery, a Client
	// showing the Session; it answers 204.
	SessionViewedPath = "/session-times/viewed"
	AfterQuery        = "after"
)

// longPoll is how long a GET of SessionTimesPath with AfterQuery waits for
// a change before it answers none.
const longPoll = 25 * time.Second

// SessionTimes is the Host's Session Times: the bridges note in it the
// messages Clients send and the turns that end, and the endpoints serve it.
type SessionTimes interface {
	Sent(sessionID string)
	Working(sessionID string)
	Ended(sessionID string)
	Viewed(sessionID string)
	Since(after uint64) sessions.TimesSnapshot
	Wait(ctx context.Context, after uint64) sessions.TimesSnapshot
}

func (g *Gateway) answerSessionTimes(r *http.Request, endpoint string, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	times := g.opts.SessionTimes
	if times == nil {
		return http.StatusNotFound, nil
	}
	if endpoint == SessionViewedPath {
		if r.Method != http.MethodPost {
			return http.StatusMethodNotAllowed, nil
		}
		id := q.Get(SessionIDQuery)
		if id == "" {
			return http.StatusBadRequest, errorBody{Error: "No session given"}
		}
		times.Viewed(id)
		return http.StatusNoContent, nil
	}
	if r.Method != http.MethodGet {
		return http.StatusMethodNotAllowed, nil
	}
	raw := q.Get(AfterQuery)
	if raw == "" {
		return http.StatusOK, times.Since(0)
	}
	after, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return http.StatusBadRequest, errorBody{Error: "after is not a revision"}
	}
	ctx, cancel := context.WithTimeout(r.Context(), longPoll)
	defer cancel()
	go func() {
		select {
		case <-g.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return http.StatusOK, times.Wait(ctx, after)
}

// SentTo is the Session a Client sends the user's message to in a
// `daemon.add_user_message` frame. A message a Client adds for the model
// alone, or as another role, is none.
func SentTo(frame []byte) (string, bool) {
	var m struct {
		Method string `json:"method"`
		Params struct {
			SessionID  string `json:"sessionId"`
			Role       string `json:"role"`
			Visibility string `json:"visibility"`
		} `json:"params"`
	}
	if json.Unmarshal(frame, &m) != nil || m.Method != addUserMessage || m.Params.SessionID == "" {
		return "", false
	}
	if (m.Params.Role != "" && m.Params.Role != "user") || (m.Params.Visibility != "" && m.Params.Visibility != "both") {
		return "", false
	}
	return m.Params.SessionID, true
}

// WorkingState is the Session and the state a Daemon's
// `droid_working_state_changed` notification reports.
func WorkingState(frame []byte) (sessionID, state string, ok bool) {
	var m struct {
		Method string `json:"method"`
		Params struct {
			SessionID    string `json:"sessionId"`
			Notification struct {
				Type     string `json:"type"`
				NewState string `json:"newState"`
			} `json:"notification"`
		} `json:"params"`
	}
	if json.Unmarshal(frame, &m) != nil || m.Method != sessionNotification || m.Params.Notification.Type != workingStateChanged || m.Params.SessionID == "" {
		return "", "", false
	}
	return m.Params.SessionID, m.Params.Notification.NewState, true
}

// noteTimes notes in the Session Times what a frame from the Client
// (toDaemon) or the Daemon says, before it goes on.
func noteTimes(times SessionTimes, frame []byte, toDaemon bool) {
	if times == nil {
		return
	}
	if toDaemon {
		if !containsString(frame, addUserMessage) {
			return
		}
		if id, ok := SentTo(frame); ok {
			times.Sent(id)
		}
		return
	}
	if !containsString(frame, workingStateChanged) {
		return
	}
	if id, state, ok := WorkingState(frame); ok {
		if state == "idle" {
			times.Ended(id)
		} else {
			times.Working(id)
		}
	}
}
