package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrNotGateway is an address that answers, but not as a Droi Gateway.
	ErrNotGateway = errors.New("this address is not a Droi Gateway")
	// ErrTokenRejected is a Gateway that does not accept the Pairing Token.
	ErrTokenRejected = errors.New("the Gateway does not accept this Pairing Token")
)

// FetchMeta asks the Gateway at gatewayURL (its origin) which computer it
// belongs to. An error that is not ErrNotGateway means nothing answered.
func FetchMeta(ctx context.Context, gatewayURL string) (Meta, error) {
	var meta Meta
	status, body, err := get(ctx, gatewayURL, MetaPath, "")
	if err != nil {
		return meta, err
	}
	if status != http.StatusOK || json.Unmarshal(body, &meta) != nil || meta.App != "Droi" {
		return Meta{}, ErrNotGateway
	}
	return meta, nil
}

// CheckToken asks the Gateway at gatewayURL whether it accepts token, with
// the plain request on the Daemon socket's path that a Client uses to tell a
// bad Pairing Token from an unreachable Daemon.
func CheckToken(ctx context.Context, gatewayURL, token string) error {
	status, _, err := get(ctx, gatewayURL, DaemonPath, token)
	switch {
	case err != nil:
		return err
	case status == http.StatusUnauthorized:
		return ErrTokenRejected
	case status != http.StatusNoContent:
		return ErrNotGateway
	}
	return nil
}

// SocketURL is the Daemon socket of the Gateway at gatewayURL, presenting
// token: where a Client that is not on the Gateway's computer connects.
func SocketURL(gatewayURL, token string) (string, error) {
	u, err := url.Parse(gatewayURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a Gateway address", gatewayURL)
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + u.Host + DaemonPath + "?" + TokenQuery + "=" + url.QueryEscape(token), nil
}

func get(ctx context.Context, gatewayURL, path, token string) (int, []byte, error) {
	target := strings.TrimRight(gatewayURL, "/") + path
	if token != "" {
		target += "?" + TokenQuery + "=" + url.QueryEscape(token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, body, err
}
