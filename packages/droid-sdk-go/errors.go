package droid

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coder/websocket"
)

// ErrClosed is the error of a connection the Client closed.
var ErrClosed = errors.New("droid: connection closed")

// JSON-RPC error codes the Daemon uses.
const (
	CodeAuthenticationError = -32001
)

// RPCError is an error the Daemon answered a request with.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("daemon error %d: %s", e.Code, e.Message) }

// DialError is a failure to open the WebSocket; StatusCode is the HTTP
// status of a refused handshake (401: a Gateway refused the token; 503: the
// Daemon is not running behind it).
type DialError struct {
	StatusCode int
	Err        error
}

func (e *DialError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("droid: dial: HTTP %d: %v", e.StatusCode, e.Err)
	}
	return fmt.Sprintf("droid: dial: %v", e.Err)
}
func (e *DialError) Unwrap() error { return e.Err }

// AuthError is a credential the Daemon refused.
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return "droid: authentication rejected: " + e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

// TimeoutError is a request that got no answer in time.
type TimeoutError struct {
	Method string
	Err    error
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("droid: %s: no answer: %v", e.Method, e.Err)
}
func (e *TimeoutError) Unwrap() error { return e.Err }

// ClosedError is a connection the Daemon or the network closed.
type ClosedError struct {
	Status websocket.StatusCode
	Err    error
}

func (e *ClosedError) Error() string {
	return fmt.Sprintf("droid: connection lost (%d): %v", e.Status, e.Err)
}
func (e *ClosedError) Unwrap() error { return e.Err }
