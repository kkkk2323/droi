package controller

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
)

// FailureReason says why the Controller could not connect, as the TS SDK's
// ConnectionFailureReason does for the reasons a local Daemon can have.
type FailureReason string

const (
	// ReasonNoToken: Config.Credential gave no credential.
	ReasonNoToken FailureReason = "no_token"
	// ReasonAuthRejected: the Daemon or its Gateway refused the credential.
	ReasonAuthRejected FailureReason = "auth_rejected"
	// ReasonDaemonNotSpawned: nothing listens yet, or the Gateway says the
	// Daemon is not running (HTTP 503).
	ReasonDaemonNotSpawned FailureReason = "daemon_not_spawned"
	// ReasonDaemonUnreachable: the WebSocket could not be opened.
	ReasonDaemonUnreachable FailureReason = "daemon_unreachable"
	// ReasonDaemonTimeout: the Daemon did not answer daemon.authenticate.
	ReasonDaemonTimeout FailureReason = "daemon_timeout"
	// ReasonConnectionLost: an open connection ended.
	ReasonConnectionLost FailureReason = "connection_lost"
	ReasonUnknown        FailureReason = "unknown"
)

// ConnectionError is a failure to connect or a lost connection.
type ConnectionError struct {
	Reason FailureReason
	// Retryable is false for failures that trying again cannot fix, such as
	// a refused credential.
	Retryable bool
	Err       error
}

func (e *ConnectionError) Error() string {
	if e.Err == nil {
		return "droid: " + string(e.Reason)
	}
	return fmt.Sprintf("droid: %s: %v", e.Reason, e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

func classify(err error) *ConnectionError {
	var ce *ConnectionError
	if errors.As(err, &ce) {
		return ce
	}
	var ae *droid.AuthError
	var de *droid.DialError
	var te *droid.TimeoutError
	switch {
	case errors.As(err, &ae):
		return &ConnectionError{Reason: ReasonAuthRejected, Err: err}
	case errors.As(err, &de):
		switch {
		case de.StatusCode == 401 || de.StatusCode == 403:
			return &ConnectionError{Reason: ReasonAuthRejected, Err: err}
		case de.StatusCode == 503 || errors.Is(err, syscall.ECONNREFUSED):
			return &ConnectionError{Reason: ReasonDaemonNotSpawned, Retryable: true, Err: err}
		}
		return &ConnectionError{Reason: ReasonDaemonUnreachable, Retryable: true, Err: err}
	case errors.As(err, &te):
		return &ConnectionError{Reason: ReasonDaemonTimeout, Retryable: true, Err: err}
	case errors.Is(err, context.Canceled), errors.Is(err, ErrClosed):
		return &ConnectionError{Reason: ReasonUnknown, Err: err}
	}
	return &ConnectionError{Reason: ReasonUnknown, Retryable: true, Err: err}
}

var (
	// ErrClosed is the error of a Controller after Close.
	ErrClosed = errors.New("droid: controller closed")
	// ErrNotConnected is the error of a call while there is no connection.
	ErrNotConnected = errors.New("droid: not connected")
	// ErrSessionNotFound is a Session the Daemon does not know.
	ErrSessionNotFound = errors.New("droid: session not found")
	// ErrPromptNotFound is an answer to a permission or AskUser prompt that is
	// not pending.
	ErrPromptNotFound = errors.New("droid: no such pending prompt")
)

// codeEntityNotFound is the Daemon's JSON-RPC code for an unknown entity.
const codeEntityNotFound = -32004
