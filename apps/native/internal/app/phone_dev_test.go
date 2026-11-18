package app

import (
	"os"
	"os/signal"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// TestPhoneDevGateway serves the README's Fake Daemon through a Gateway on
// this computer, for the Phone App on a simulator to pair with, until
// interrupted:
//
//	DROI_PHONE_DEV=1 go test ./apps/native/internal/app -run TestPhoneDevGateway -v -timeout 0
func TestPhoneDevGateway(t *testing.T) {
	if os.Getenv("DROI_PHONE_DEV") == "" {
		t.Skip("set DROI_PHONE_DEV=1 to serve a Fake Daemon to the Phone App")
	}
	sc := readmeScenario()
	sc.Turn = &fakedaemon.Turn{Kind: "permission", Command: "pnpm test", ToolOutput: "27 passed", DelayMs: 40,
		Reply: "All **27 tests** pass.\n\n| File | Change |\n| --- | --- |\n| `src/auth/login.ts` | awaits the refresh |\n\nNext I would run the whole suite."}
	d := fakedaemon.Start(t, sc)
	gw, err := gateway.Start(gateway.Options{
		Port:         41499,
		DaemonURL:    func() string { return d.URL },
		PairingToken: func() string { return "phone-dev-token" },
		Version:      "1.97.1",
		ComputerName: "Fake Mac",
		ComputerID:   "fake-mac",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	t.Logf("pairing link: %s/#pair=phone-dev-token", gw.URL())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
