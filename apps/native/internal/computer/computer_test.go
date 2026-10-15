package computer

import (
	"context"
	"errors"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
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

var (
	box  = Paired{Gateway: "http://box:41417", Token: "t", Name: "Ubuntu box", ID: "box-1"}
	mini = Paired{Gateway: "http://mini:41417", Token: "m", Name: "Mac mini", ID: "mini-1"}
)

func TestAFollowedComputerIsKeptAcrossLaunches(t *testing.T) {
	file := t.TempDir() + "/preferences.json"
	Follow(prefs.Open(file), box)
	s := prefs.Open(file)
	if got := Load(s); got == nil || *got != box {
		t.Fatalf("%+v", got)
	}
	if got := Remembered(s); len(got) != 1 || got[0] != box {
		t.Fatalf("remembered %+v", got)
	}
}

func TestUsingThisComputerKeepsTheOthersRemembered(t *testing.T) {
	s := prefs.Memory()
	Follow(s, box)
	Follow(s, mini)
	if got := Load(s); got == nil || *got != mini {
		t.Fatalf("following %+v", got)
	}
	UseThisComputer(s)
	if got := Load(s); got != nil {
		t.Fatalf("still following %+v", got)
	}
	if got := Remembered(s); len(got) != 2 || got[0] != box || got[1] != mini {
		t.Fatalf("remembered %+v", got)
	}
}

func TestANewLinkForAComputerReplacesWhatWasKeptForIt(t *testing.T) {
	s := prefs.Memory()
	Follow(s, box)
	Follow(s, mini)
	reset := Paired{Gateway: "http://box.tail:41417", Token: "new", Name: "Ubuntu", ID: "box-1"}
	Follow(s, reset)
	if got := Remembered(s); len(got) != 2 || got[0] != reset || got[1] != mini {
		t.Fatalf("remembered %+v", got)
	}
	if got := Load(s); got == nil || *got != reset {
		t.Fatalf("following %+v", got)
	}
}

func TestForgettingAComputer(t *testing.T) {
	s := prefs.Memory()
	Follow(s, box)
	Follow(s, mini)
	Forget(s, "box-1")
	if got := Remembered(s); len(got) != 1 || got[0] != mini {
		t.Fatalf("remembered %+v", got)
	}
	if got := Load(s); got == nil || *got != mini {
		t.Fatalf("forgetting another computer changed the one followed: %+v", got)
	}
	Forget(s, "mini-1")
	if Load(s) != nil || len(Remembered(s)) != 0 {
		t.Fatalf("following %+v, remembered %+v", Load(s), Remembered(s))
	}
}

func TestTheSingleComputerAnEarlierVersionKeptIsMigrated(t *testing.T) {
	file := t.TempDir() + "/preferences.json"
	old := prefs.Open(file)
	old.Set("droi.computer", `{"gateway":"http://box:41417","token":"t","name":"Ubuntu box","id":"box-1"}`)
	s := prefs.Open(file)
	if got := Load(s); got == nil || *got != box {
		t.Fatalf("following %+v", got)
	}
	if got := Remembered(s); len(got) != 1 || got[0] != box {
		t.Fatalf("remembered %+v", got)
	}
	if _, ok := prefs.Open(file).Get("droi.computer"); ok {
		t.Fatal("the old record is still there")
	}
	UseThisComputer(s)
	if got := Load(prefs.Open(file)); got != nil {
		t.Fatalf("the old record came back: %+v", got)
	}
}

func TestAnUnreadableRecordMeansThisComputer(t *testing.T) {
	for name, raw := range map[string]string{
		"broken":             "{not json",
		"without a token":    `{"followed":"box-1","computers":[{"gateway":"http://box:1","id":"box-1"}]}`,
		"without an id":      `{"followed":"","computers":[{"gateway":"http://box:1","token":"t"}]}`,
		"followed is absent": `{"followed":"gone","computers":[]}`,
	} {
		s := prefs.Memory()
		s.Set(prefsKey, raw)
		if Load(s) != nil || len(Remembered(s)) != 0 {
			t.Errorf("%s: following %+v, remembered %+v", name, Load(s), Remembered(s))
		}
	}
	old := prefs.Memory()
	old.Set("droi.computer", `{"gateway":"http://box:1"}`)
	if Load(old) != nil || len(Remembered(old)) != 0 {
		t.Fatal("migrated a computer without a token")
	}
}

func TestVerifyReadsTheComputerAPairingLinkReaches(t *testing.T) {
	gw := startGateway(t)
	got, err := Verify(context.Background(), " "+gw.URL()+"/#pair=pairing-token ", "this-computer")
	want := Paired{Gateway: gw.URL(), Token: "pairing-token", Name: "Ubuntu box", ID: "box-1"}
	if err != nil || got != want {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestVerifyRefusesALinkWhoseTokenIsRejected(t *testing.T) {
	gw := startGateway(t)
	if _, err := Verify(context.Background(), gw.URL()+"/#pair=wrong", "this-computer"); !errors.Is(err, gateway.ErrTokenRejected) {
		t.Fatalf("%v", err)
	}
}

func TestVerifyRefusesWhatIsNotAGatewayOrNotALink(t *testing.T) {
	if _, err := Verify(context.Background(), "hello world", "this-computer"); !errors.Is(err, gateway.ErrNotPairingLink) {
		t.Fatalf("%v", err)
	}
	if _, err := Verify(context.Background(), "abc123", "this-computer"); !errors.Is(err, gateway.ErrNoGatewayAddress) {
		t.Fatalf("%v", err)
	}
}

func TestSocketURLPresentsTheToken(t *testing.T) {
	if got := (Paired{Gateway: "http://box:41417", Token: "t"}).SocketURL(); got != "ws://box:41417/daemon?token=t" {
		t.Fatalf("%q", got)
	}
}

func TestVerifyRefusesAGatewayWithoutAComputerId(t *testing.T) {
	gw, err := gateway.Start(gateway.Options{PairingToken: func() string { return "pairing-token" }, ComputerName: "Old box"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	if _, err := Verify(context.Background(), gw.URL()+"/#pair=pairing-token", "this-computer"); !errors.Is(err, ErrNoComputerID) {
		t.Fatalf("%v", err)
	}
}

func TestVerifyRefusesALinkToThisComputersOwnGateway(t *testing.T) {
	gw := startGateway(t)
	if _, err := Verify(context.Background(), gw.URL()+"/#pair=pairing-token", "box-1"); !errors.Is(err, ErrThisComputer) {
		t.Fatalf("%v", err)
	}
}
