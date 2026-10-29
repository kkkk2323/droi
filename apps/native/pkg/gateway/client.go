package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// ErrNotFound is a Gateway that does not serve the endpoint asked: it has no
// Scratch folder to make Workspaces in.
var ErrNotFound = errors.New("the Gateway does not serve this")

// StatusError is a Gateway's refusal or failure, with the reason it gave.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("the Gateway answered %d", e.Status)
}

// requestTimeout bounds a call, so a Gateway that does not answer never holds
// the window.
const requestTimeout = 15 * time.Second

// Client is a Client's side of the Gateway's plain endpoints (Scratch
// Workspaces, session files). It is made from the Daemon socket URL the Client
// already connects with, so one address and token reach both.
type Client struct {
	base  url.URL
	token string
	http  *http.Client
	// poll has no timeout of its own: a long poll's context bounds it.
	poll *http.Client
}

// NewClient is the Client for the Gateway behind socketURL, a Daemon socket
// URL as DaemonSocketURL makes it.
func NewClient(socketURL string) (*Client, error) {
	u, err := url.Parse(socketURL)
	if err != nil {
		return nil, err
	}
	token := u.Query().Get(TokenQuery)
	if (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.Path != DaemonPath || token == "" {
		return nil, fmt.Errorf("%q is not a Gateway's Daemon socket URL", socketURL)
	}
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	return &Client{base: url.URL{Scheme: scheme, Host: u.Host}, token: token, http: &http.Client{Timeout: requestTimeout}, poll: &http.Client{}}, nil
}

// maxTimes bounds an answer of the Session Times, every Session's of a
// computer with tens of thousands of them.
const maxTimes = 64 << 20

// SessionTimes is the Gateway's Session Times: every Session's for after
// 0, else what changed after that revision, once something has or the
// Gateway's long poll has passed. ErrNotFound is a Gateway that keeps none.
func (c *Client) SessionTimes(ctx context.Context, after uint64) (sessions.TimesSnapshot, error) {
	var snap sessions.TimesSnapshot
	var query url.Values
	if after != 0 {
		query = url.Values{AfterQuery: {strconv.FormatUint(after, 10)}}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, longPoll+requestTimeout)
		defer cancel()
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}
	err := c.send(ctx, c.poll, maxTimes, http.MethodGet, SessionTimesPath, query, &snap)
	return snap, err
}

// SessionViewed tells the Gateway that the Session is on screen, which
// reads it on every Client.
func (c *Client) SessionViewed(ctx context.Context, sessionID string) error {
	return c.call(ctx, http.MethodPost, SessionViewedPath, url.Values{SessionIDQuery: {sessionID}}, nil)
}

// CreateScratch makes a Scratch Workspace on the Gateway's computer and
// returns its path.
func (c *Client) CreateScratch(ctx context.Context) (string, error) {
	var made pathBody
	if err := c.call(ctx, http.MethodPost, ScratchPath, nil, &made); err != nil {
		return "", err
	}
	return made.Path, nil
}

// TrashScratch moves a Scratch Workspace of the Gateway's computer to its
// Trash; an empty one is removed.
func (c *Client) TrashScratch(ctx context.Context, path string) error {
	return c.call(ctx, http.MethodPost, ScratchTrashPath, url.Values{ScratchPathQuery: {path}}, nil)
}

// RestoreScratch recreates a Scratch Workspace folder that is gone, so the
// Sessions in it open again.
func (c *Client) RestoreScratch(ctx context.Context, path string) error {
	return c.call(ctx, http.MethodPost, ScratchRestorePath, url.Values{ScratchPathQuery: {path}}, nil)
}

// SessionFile is where the Daemon keeps the Session's transcript, "" when it
// has none.
func (c *Client) SessionFile(ctx context.Context, sessionID string) (string, error) {
	var found pathBody
	err := c.call(ctx, http.MethodGet, SessionFilePath, url.Values{SessionIDQuery: {sessionID}}, &found)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return found.Path, err
}

// call sends one request with the token and decodes a JSON answer into out
// when there is one; any status but 2xx is an error.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, out any) error {
	return c.send(ctx, c.http, 1<<20, method, path, query, out)
}

// send is call through hc, with an answer of up to limit bytes.
func (c *Client) send(ctx context.Context, hc *http.Client, limit int64, method, path string, query url.Values, out any) error {
	u := c.base
	u.Path = path
	q := url.Values{TokenQuery: {c.token}}
	for k, v := range query {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		var refusal errorBody
		_ = json.Unmarshal(body, &refusal)
		return &StatusError{Status: resp.StatusCode, Message: refusal.Error}
	}
	if resp.StatusCode == http.StatusNoContent || out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("the Gateway's answer (HTTP %d) is not JSON: %w", resp.StatusCode, err)
	}
	return nil
}
