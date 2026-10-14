package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
)

func daemonUp(context.Context) (string, error) { return "ws://127.0.0.1:1", nil }

func TestTheWindowConnectsToTheGatewayAndNeverToTheDaemon(t *testing.T) {
	gw, err := gateway.Start(gateway.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	got, err := gatewayURL(gw, nil, "local-token", daemonUp)(context.Background())
	if err != nil || got != gw.DaemonSocketURL("local-token") {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestTheWindowWaitsForTheDaemonBeforeItConnects(t *testing.T) {
	gw, err := gateway.Start(gateway.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	waiting := errors.New("the Daemon is not up")
	_, err = gatewayURL(gw, nil, "local-token", func(context.Context) (string, error) { return "", waiting })(context.Background())
	if !errors.Is(err, waiting) {
		t.Fatalf("%v", err)
	}
}

func TestAGatewayThatCannotListenIsAnErrorNotADirectConnection(t *testing.T) {
	got, err := gatewayURL(nil, errors.New("address in use"), "local-token", daemonUp)(context.Background())
	if got != "" || err == nil || !strings.Contains(err.Error(), "Gateway") || !strings.Contains(err.Error(), "address in use") {
		t.Fatalf("%q, %v", got, err)
	}
}
