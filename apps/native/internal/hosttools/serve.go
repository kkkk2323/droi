package hosttools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/brand"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

const (
	// toolsWait is how long a tool server tries to read its tools from a
	// Host that does not answer yet.
	toolsWait  = 5 * time.Second
	toolsRetry = 250 * time.Millisecond
	// callTimeout bounds one forwarded call.
	callTimeout = 3 * time.Minute
)

// Main is `<droi> host-tools <name>`: the tool server name, served on
// standard input and output. It returns the exit code.
func Main(args []string) int {
	return serveMain(args, os.LookupEnv, os.Stdin, os.Stdout, os.Stderr)
}

func serveMain(args []string, lookupEnv func(string) (string, bool), in io.Reader, out, errOut io.Writer) int {
	prefix := brand.Current().ID + " " + Command + ":"
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintf(errOut, "usage: %s %s <name>\n", brand.Current().ID, Command)
		return 2
	}
	base, _ := lookupEnv(EnvURL)
	token, _ := lookupEnv(EnvToken)
	if base == "" || token == "" {
		fmt.Fprintln(errOut, prefix, EnvURL, "and", EnvToken, "must be set")
		return 2
	}
	f := &forwarder{
		base:  strings.TrimSuffix(base, "/") + "/servers/" + url.PathEscape(args[0]),
		token: token,
		http:  newForwarderClient(),
	}
	tools, err := f.tools()
	if err != nil {
		// Served without tools all the same: a server that exits only shows
		// the Daemon a failure without its reason.
		fmt.Fprintln(errOut, prefix, err)
		tools = json.RawMessage("[]")
	}
	err = mcpserver.Serve(in, out, mcpserver.Server{
		Name:    args[0],
		Title:   args[0],
		Version: "1.0.0",
		Tools:   tools,
		Call:    f.call,
	})
	if err != nil {
		fmt.Fprintln(errOut, prefix, err)
		return 1
	}
	return 0
}

// forwarder carries one tool server's requests to the Bridge.
type forwarder struct {
	base  string
	token string
	http  *http.Client
}

// newForwarderClient reaches the Bridge on loopback, never through a proxy.
func newForwarderClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil}}
}

// unreachable is a Host that did not answer at all, as opposed to one that
// answered with a failure.
type unreachable struct{ err error }

func (u unreachable) Error() string {
	return fmt.Sprintf("%s cannot be reached; it may have quit: %v", brand.Name(), u.err)
}

func (f *forwarder) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, f.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := f.http.Do(req)
	if err != nil {
		return nil, unreachable{err}
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, unreachable{err}
	}
	if res.StatusCode != http.StatusOK {
		return nil, &statusError{code: res.StatusCode, text: strings.TrimSpace(string(b))}
	}
	return b, nil
}

type statusError struct {
	code int
	text string
}

func (e *statusError) Error() string {
	switch e.code {
	case http.StatusUnauthorized:
		return fmt.Sprintf("%s refused this tool server's token (401): the Daemon was started by an earlier launch; restart it", brand.Name())
	case http.StatusNotFound:
		return fmt.Sprintf("%s answered 404: %s", brand.Name(), e.text)
	}
	return fmt.Sprintf("%s answered %d: %s", brand.Name(), e.code, e.text)
}

// tools reads the server's tools, trying again for a while on a Host that
// does not answer or fails: one starting up. A refusal is final.
func (f *forwarder) tools() (json.RawMessage, error) {
	deadline := time.Now().Add(toolsWait)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), toolsWait)
		b, err := f.do(ctx, http.MethodGet, "/tools", nil)
		cancel()
		if err == nil {
			if !json.Valid(b) {
				return nil, fmt.Errorf("%s answered tools that are not JSON", brand.Name())
			}
			return b, nil
		}
		var se *statusError
		if errors.As(err, &se) && se.code < 500 || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(toolsRetry)
	}
}

func (f *forwarder) call(tool string, args map[string]any, sessionID string) (mcpserver.Result, error) {
	body, err := json.Marshal(callRequest{Tool: tool, Args: args, SessionID: sessionID})
	if err != nil {
		return mcpserver.Result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	b, err := f.do(ctx, http.MethodPost, "/call", body)
	if err != nil {
		return mcpserver.Result{}, fmt.Errorf("%s did not run: %w", tool, err)
	}
	var res struct {
		Content           []mcpserver.Content `json:"content"`
		StructuredContent json.RawMessage     `json:"structuredContent"`
		IsError           bool                `json:"isError"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return mcpserver.Result{}, fmt.Errorf("%s answered %s with something that is not a result: %w", brand.Name(), tool, err)
	}
	out := mcpserver.Result{Content: res.Content, IsError: res.IsError}
	if out.Content == nil {
		out.Content = []mcpserver.Content{}
	}
	if len(res.StructuredContent) > 0 {
		out.StructuredContent = res.StructuredContent
	}
	return out, nil
}
