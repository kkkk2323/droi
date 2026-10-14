package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
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

func TestAWindowFollowingAnotherComputerConnectsToItsGatewayWithoutWaitingForThisDaemon(t *testing.T) {
	other := &computer.Paired{Gateway: "http://box:41417", Token: "box-token", Name: "Ubuntu box", ID: "box-1"}
	waiting := errors.New("this computer's Daemon is not up")
	got, err := windowURL(other, nil, errors.New("address in use"), "local-token", func(context.Context) (string, error) { return "", waiting })(context.Background())
	if err != nil || got != "ws://box:41417/daemon?token=box-token" {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestAWindowFollowingNothingConnectsThroughItsOwnGateway(t *testing.T) {
	gw, err := gateway.Start(gateway.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	got, err := windowURL(nil, gw, nil, "local-token", daemonUp)(context.Background())
	if err != nil || got != gw.DaemonSocketURL("local-token") {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestConnectingIsGatedOnTheTargetsLogin(t *testing.T) {
	idle := controller.Status{}
	for _, tc := range []struct {
		name          string
		status        controller.Status
		following     bool
		hasCredential bool
		want          bool
	}{
		{"this computer, signed in", idle, false, true, true},
		{"this computer, no login yet", idle, false, false, false},
		{"another computer, whatever this one holds", idle, true, false, true},
		{"connected already", controller.Status{Connected: true}, true, true, false},
		{"reconnecting by itself", controller.Status{Reconnecting: true}, false, true, false},
	} {
		if got := shouldConnect(tc.status, tc.following, func() bool { return tc.hasCredential }); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestThisComputersLoginIsNotLookedAtWhileFollowingAnother(t *testing.T) {
	asked := false
	shouldConnect(controller.Status{}, true, func() bool { asked = true; return false })
	if asked {
		t.Fatal("the window asked this Host whether it holds a login")
	}
}

func TestASavedAddressThatCannotBeUsedIsAnErrorTheWindowShowsAndLeavesNoGatewayClient(t *testing.T) {
	bad := &computer.Paired{Gateway: "not an address", Token: "box-token", Name: "Ubuntu box", ID: "box-1"}
	got, err := windowURL(bad, nil, nil, "local-token", daemonUp)(context.Background())
	if got != "" || err == nil || !strings.Contains(err.Error(), "Ubuntu box") {
		t.Fatalf("%q, %v", got, err)
	}
	if c := windowClient(bad, nil, "local-token"); c != nil {
		t.Fatalf("a Gateway Client for an address that cannot be used: %+v", c)
	}
}

func TestTheWindowsGatewayClientReachesTheFollowedComputerOrThisOne(t *testing.T) {
	other := &computer.Paired{Gateway: "http://box:41417", Token: "box-token", Name: "Ubuntu box", ID: "box-1"}
	if windowClient(other, nil, "local-token") == nil {
		t.Fatal("no Gateway Client for the followed computer")
	}
	gw, err := gateway.Start(gateway.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	if windowClient(nil, gw, "local-token") == nil {
		t.Fatal("no Gateway Client for this computer")
	}
	if windowClient(nil, nil, "local-token") != nil {
		t.Fatal("a Gateway Client without a Gateway")
	}
}

func TestKeepingTheWindowConnectedStopsWithTheComputerItConnects(t *testing.T) {
	home := t.TempDir()
	h := host.New(host.Config{UserData: t.TempDir(), Home: home, Env: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}})
	ctl := controller.New(controller.Config{URL: "ws://127.0.0.1:1", MaxConnectAttempts: 1})
	t.Cleanup(func() { ctl.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		connectWhenSignedIn(ctx, ctl, h, true)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("still keeping a closed computer's Controller connected")
	}
}

func TestDroiHostRunsOnLinuxOnly(t *testing.T) {
	if hostUnsupported("linux") != "" {
		t.Fatal("droi host is refused on Linux")
	}
	for _, goos := range []string{"darwin", "windows"} {
		if msg := hostUnsupported(goos); !strings.Contains(msg, "Linux only") {
			t.Fatalf("%s: %q", goos, msg)
		}
	}
}
