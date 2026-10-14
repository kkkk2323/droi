package computer

import (
	"context"
	"errors"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

func startGateway(t *testing.T) *gateway.Gateway {
	t.Helper()
	gw, err := gateway.Start(gateway.Options{
		PairingToken: func() string { return "pairing-token" },
		ComputerName: "Ubuntu box",
		ComputerID:   "box-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	return gw
}

func TestNothingSavedMeansThisComputer(t *testing.T) {
	if got := Load(prefs.Memory()); got != nil {
		t.Fatalf("%+v", got)
	}
}

func TestAPairedComputerIsKeptAndForgotten(t *testing.T) {
	file := t.TempDir() + "/preferences.json"
	want := Paired{Gateway: "http://box:41417", Token: "t", Name: "Ubuntu box", ID: "box-1"}
	Save(prefs.Open(file), want)
	if got := Load(prefs.Open(file)); got == nil || *got != want {
		t.Fatalf("%+v", got)
	}
	s := prefs.Open(file)
	Clear(s)
	if got := Load(prefs.Open(file)); got != nil {
		t.Fatalf("still following %+v", got)
	}
}

func TestAnUnreadableRecordMeansThisComputer(t *testing.T) {
	s := prefs.Memory()
	s.Set(prefsKey, "{not json")
	if Load(s) != nil {
		t.Fatal("followed a computer from a broken record")
	}
	s.Set(prefsKey, `{"gateway":"http://box:1"}`)
	if Load(s) != nil {
		t.Fatal("followed a computer without a token")
	}
}

func TestVerifyReadsTheComputerAPairingLinkReaches(t *testing.T) {
	gw := startGateway(t)
	got, err := Verify(context.Background(), " "+gw.URL()+"/#pair=pairing-token ")
	want := Paired{Gateway: gw.URL(), Token: "pairing-token", Name: "Ubuntu box", ID: "box-1"}
	if err != nil || got != want {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestVerifyRefusesALinkWhoseTokenIsRejected(t *testing.T) {
	gw := startGateway(t)
	if _, err := Verify(context.Background(), gw.URL()+"/#pair=wrong"); !errors.Is(err, gateway.ErrTokenRejected) {
		t.Fatalf("%v", err)
	}
}

func TestVerifyRefusesWhatIsNotAGatewayOrNotALink(t *testing.T) {
	if _, err := Verify(context.Background(), "hello world"); !errors.Is(err, gateway.ErrNotPairingLink) {
		t.Fatalf("%v", err)
	}
	if _, err := Verify(context.Background(), "abc123"); !errors.Is(err, gateway.ErrNoGatewayAddress) {
		t.Fatalf("%v", err)
	}
}

func TestSocketURLPresentsTheToken(t *testing.T) {
	if got := (Paired{Gateway: "http://box:41417", Token: "t"}).SocketURL(); got != "ws://box:41417/daemon?token=t" {
		t.Fatalf("%q", got)
	}
}
