package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func startTestGateway(t *testing.T) *Gateway {
	t.Helper()
	gw, err := Start(Options{
		PairingToken: func() string { return "pairing-token" },
		Version:      "9.9.9",
		ComputerName: "Ubuntu box",
		ComputerID:   "box-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	return gw
}

func TestFetchMetaReadsTheComputerAGatewayDescribes(t *testing.T) {
	gw := startTestGateway(t)
	meta, err := FetchMeta(context.Background(), gw.URL())
	if err != nil || meta.Name != "Ubuntu box" || meta.ComputerID != "box-1" || meta.Version != "9.9.9" {
		t.Fatalf("%+v, %v", meta, err)
	}
}

func TestFetchMetaRefusesAnythingThatIsNotADroiGateway(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>router</html>"))
	}))
	t.Cleanup(web.Close)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"app":"Something else","name":"x","computerId":"y"}`))
	}))
	t.Cleanup(other.Close)
	missing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(missing.Close)
	for name, url := range map[string]string{"HTML": web.URL, "another app": other.URL, "no /meta": missing.URL} {
		if _, err := FetchMeta(context.Background(), url); !errors.Is(err, ErrNotGateway) {
			t.Errorf("%s: %v, want ErrNotGateway", name, err)
		}
	}
}

func TestFetchMetaSaysWhenNothingAnswers(t *testing.T) {
	gw := startTestGateway(t)
	url := gw.URL()
	gw.Close()
	_, err := FetchMeta(context.Background(), url)
	if err == nil || errors.Is(err, ErrNotGateway) {
		t.Fatalf("%v, want an unreachable error", err)
	}
}

func TestCheckTokenTellsAcceptedFromRejected(t *testing.T) {
	gw := startTestGateway(t)
	if err := CheckToken(context.Background(), gw.URL(), "pairing-token"); err != nil {
		t.Fatalf("accepted token: %v", err)
	}
	if err := CheckToken(context.Background(), gw.URL(), "wrong"); !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestSocketURLIsTheDaemonSocketAClientConnectsTo(t *testing.T) {
	got, err := SocketURL("http://box:41417", "a b")
	if err != nil || got != "ws://box:41417/daemon?token=a+b" {
		t.Fatalf("%q, %v", got, err)
	}
	if got, _ := SocketURL("https://box:41417", "t"); got != "wss://box:41417/daemon?token=t" {
		t.Fatalf("%q", got)
	}
	// The window's Client for the Gateway's plain endpoints reads the same URL.
	if _, err := NewClient("ws://box:41417/daemon?token=t"); err != nil {
		t.Fatal(err)
	}
}
