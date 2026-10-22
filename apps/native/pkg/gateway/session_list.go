package gateway

import (
	"net/http"
	"net/url"
)

// The Session list's order, which a phone takes from the computer so that it
// lists the Sessions as the computer's window does
// (packages/daemon-layer/src/gateway.ts).
const (
	// SessionListPath answers a SessionListOrder to a GET.
	SessionListPath = "/session-list"
	// SessionPinPath pins or unpins, to a POST with PinKindQuery,
	// PinKeyQuery and PinnedQuery; it answers 204.
	SessionPinPath = "/session-list/pin"
	PinKindQuery   = "kind"
	PinKeyQuery    = "key"
	PinnedQuery    = "pinned"

	PinSession   = "session"
	PinWorkspace = "workspace"
)

// SessionListOrder is how the computer's window lists its Sessions.
type SessionListOrder struct {
	PinnedSessions   []string `json:"pinnedSessions"`
	PinnedWorkspaces []string `json:"pinnedWorkspaces"`
	// WorkspaceOrder is the Workspace groups' keys in the order the window
	// last showed them; empty when it has not shown this computer's.
	WorkspaceOrder []string `json:"workspaceOrder"`
	// SessionSort is "recent" or "created"; "" is the default, "recent".
	SessionSort string `json:"sessionSort"`
}

// SessionList is the Host's side of the Session list endpoints.
type SessionList interface {
	Order() SessionListOrder
	// SetPinned pins or unpins a Session (PinSession, by id) or a
	// Workspace (PinWorkspace, by group key).
	SetPinned(kind, key string, pinned bool) error
}

func (g *Gateway) answerSessionList(r *http.Request, endpoint string, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	list := g.opts.SessionList
	if list == nil {
		return http.StatusNotFound, nil
	}
	if endpoint == SessionListPath {
		if r.Method != http.MethodGet {
			return http.StatusMethodNotAllowed, nil
		}
		order := list.Order()
		for _, l := range []*[]string{&order.PinnedSessions, &order.PinnedWorkspaces, &order.WorkspaceOrder} {
			if *l == nil {
				*l = []string{}
			}
		}
		return http.StatusOK, order
	}
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, nil
	}
	kind, key, pinned := q.Get(PinKindQuery), q.Get(PinKeyQuery), q.Get(PinnedQuery)
	if (kind != PinSession && kind != PinWorkspace) || key == "" || (pinned != "true" && pinned != "false") {
		return http.StatusBadRequest, errorBody{Error: "Give a kind, a key and pinned"}
	}
	if err := list.SetPinned(kind, key, pinned == "true"); err != nil {
		return http.StatusInternalServerError, errorBody{Error: err.Error()}
	}
	return http.StatusNoContent, nil
}
