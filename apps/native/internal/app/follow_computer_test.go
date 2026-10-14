package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/computer"
	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// otherComputer is a second Host for the window to follow: a Gateway in front
// of a Fake Daemon of its own, with its own Scratch folder and System Prompt
// Addition.
type otherComputer struct {
	d       *fakedaemon.Daemon
	gw      *gateway.Gateway
	scratch string
}

const otherToken = "other-pairing-token"

func startOtherComputer(t *testing.T, sc fakedaemon.Scenario, addition string) *otherComputer {
	t.Helper()
	scratch := filepath.Join(t.TempDir(), "chats", "2026-10-14-ffeedd")
	sc.ValidDirectories = append(sc.ValidDirectories, scratch)
	d := fakedaemon.Start(t, sc)
	o := &otherComputer{d: d, scratch: scratch}
	gw, err := gateway.Start(gateway.Options{
		DaemonURL:          func() string { return d.URL },
		PairingToken:       func() string { return otherToken },
		Credential:         func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: "fk-other"}, nil },
		AppendSystemPrompt: func() string { return addition },
		Version:            "9.9.9",
		ComputerName:       "Ubuntu box",
		ComputerID:         "box-1",
		Scratch:            madeFolder(o.scratch),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gw.Close() })
	o.gw = gw
	return o
}

func (o *otherComputer) paired() computer.Paired {
	return computer.Paired{Gateway: o.gw.URL(), Token: otherToken, Name: "Ubuntu box", ID: "box-1"}
}

func (o *otherComputer) link() string { return o.gw.URL() + "/#pair=" + otherToken }

func (o *otherComputer) initializeRequests(t *testing.T) []string {
	t.Helper()
	reqs, err := o.d.Requests()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range reqs {
		if r.Method == "daemon.initialize_session" {
			out = append(out, string(r.Params))
		}
	}
	return out
}

// relaunches counts the window's relaunches, which the tests cannot make.
type relaunches struct{ n int }

func (r *relaunches) relaunch() { r.n++ }

// followHarness is the window of a Droi whose Computer preference names p:
// it connects to that Gateway with its Pairing Token, as main does, while
// the harness's own Host and Gateway are this computer's, still running.
func followHarness(t *testing.T, local fakedaemon.Scenario, p computer.Paired, rl *relaunches, with func(*Config)) *harness {
	t.Helper()
	ctl := controller.New(controller.Config{
		URL:                  p.SocketURL(),
		Credential:           gateway.PlaceholderCredential,
		DefaultMessageLimit:  LoadedMessageLimit,
		MaxConnectAttempts:   1,
		MaxReconnectAttempts: 1000,
	})
	t.Cleanup(func() { ctl.Close() })
	client, err := gateway.NewClient(p.SocketURL())
	if err != nil {
		t.Fatal(err)
	}
	h := startHarness(t, local, "", "This computer's rules", func(cfg *Config) {
		cfg.Computer, cfg.Controller, cfg.Gateway = &p, ctl, client
		cfg.Host = testHost(t)
		cfg.Host.Settings.Update(func(s *host.Settings) { s.APIKey = host.OptString("fk-local") })
		if rl != nil {
			cfg.Relaunch = rl.relaunch
		}
		if with != nil {
			with(cfg)
		}
	})
	go ctl.Connect(context.Background())
	return h
}

func localSessions() fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "On this Mac", Cwd: "/Users/dev/mac-project",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}}}
}

func boxSessions() fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "On the box", Cwd: "/home/clive/box-project",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hello from linux"}}}}}
}

func (h *harness) openSettingsTab(tab string) {
	h.t.Helper()
	h.a.Go(Route{Name: "settings", Tab: tab})
	h.settle()
}

func (h *harness) hasButton(name string) bool {
	_, ok := h.tt.Find(name)
	return ok
}

func TestNothingSavedTheWindowFollowsThisComputer(t *testing.T) {
	h := newHarness(t, oneSession(), "")
	if h.a.following() || h.a.computerID() != "" {
		t.Fatalf("following %v, computer %q", h.a.following(), h.a.computerID())
	}
}

func TestFollowingAComputerListsItsSessionsAndNotThisComputers(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.until("the other computer's Session", func() bool { return h.hasText("On the box") })
	if h.hasText("On this Mac") {
		t.Fatalf("this computer's Session is listed: %q", h.tt.Texts())
	}
	if !h.a.following() || h.a.computerID() != "box-1" {
		t.Fatalf("following %v, computer %q", h.a.following(), h.a.computerID())
	}
	h.click("On the box")
	h.until("its messages", func() bool { return h.hasText("hello from linux") })
}

func TestFollowingAComputerStreamsATurnAndAnswersItsPrompts(t *testing.T) {
	sc := deployScenario(&fakedaemon.Turn{Kind: "permission", Command: "rm -rf build", Reply: "Build directory removed on the box."})
	o := startOtherComputer(t, sc, "")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.openSession("Deploy")
	h.send("Clean the build dir")
	h.until("the permission card", func() bool { return h.hasText("rm -rf build") })
	h.click("Yes, allow")
	h.until("the reply", func() bool { return h.hasText("Build directory removed on the box.") })
}

func TestASessionStartedOnAnotherComputerGetsThatHostsAdditionAndScratchFolder(t *testing.T) {
	o := startOtherComputer(t, draftScenario(), "The box's rules")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	startScratchSession(t, h)
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })

	var scratch []string
	for _, params := range o.initializeRequests(t) {
		if strings.Contains(params, "droi.scratch") {
			scratch = append(scratch, params)
		}
	}
	if len(scratch) != 1 {
		t.Fatalf("%d Scratch Sessions on the other computer: %v", len(scratch), scratch)
	}
	if !strings.Contains(scratch[0], o.scratch) {
		t.Fatalf("the Session does not work in the other computer's Scratch folder %s: %s", o.scratch, scratch[0])
	}
	if !strings.Contains(scratch[0], "The box's rules") || strings.Contains(scratch[0], "This computer's rules") {
		t.Fatalf("the System Prompt Addition is not that Host's: %s", scratch[0])
	}
	if info, err := os.Stat(o.scratch); err != nil || !info.IsDir() {
		t.Fatalf("the folder was not made there: %v", err)
	}
	if got := initializeRequests(t, h); len(got) != 0 {
		t.Fatalf("this computer's Daemon was asked to start Sessions: %v", got)
	}
}

func TestFollowingAnotherComputerHidesWhatBelongsToThisHost(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) { cfg.Remote = &fakeRemote{} })
	h.until("the Session list", func() bool { return h.hasText("On the box") })
	h.openSettingsTab("computer")
	for _, hidden := range []string{"Account", "Memory", "Remote Access", "Advanced"} {
		if h.hasButton(hidden) {
			t.Errorf("the %s tab is shown while following another computer", hidden)
		}
	}
	for _, shown := range []string{"General", "Session defaults", "Worktrees", "Notifications", "Computer", "About"} {
		if !h.hasButton(shown) {
			t.Errorf("no %s tab", shown)
		}
	}
	if !h.hasText("Account, Memory, Remote Access and Advanced are changed on Ubuntu box.") {
		t.Fatalf("no line saying where they are changed: %q", h.tt.Texts())
	}
}

func TestThisComputersSettingsTabsAreThereWhenFollowingNothing(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t); cfg.Remote = &fakeRemote{} })
	h.openSettingsTab("computer")
	for _, shown := range []string{"Account", "Memory", "Remote Access", "Advanced", "Computer"} {
		if !h.hasButton(shown) {
			t.Errorf("no %s tab", shown)
		}
	}
	if h.hasText("are changed on") {
		t.Fatal("the line about another computer is shown for this one")
	}
}

func TestFollowingAnotherComputerDoesNotAskThisHostToSignIn(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) { cfg.Host = testHost(t) })
	h.until("the other computer's Session", func() bool { return h.hasText("On the box") })
	if h.hasText("Sign in to Droi") {
		t.Fatal("this Host's missing login blocks the window")
	}
}

func TestPastingAPairingLinkChecksItSavesTheComputerAndRelaunches(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	rl := &relaunches{}
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t); cfg.Relaunch = rl.relaunch })
	h.openSettingsTab("computer")
	if !h.hasText("This computer") {
		t.Fatalf("the Computer entry does not say this computer: %q", h.tt.Texts())
	}
	h.click("Pairing link")
	h.typeInto("  " + o.link() + "  ")
	h.click("Follow computer")
	h.until("the relaunch", func() bool { return rl.n == 1 })

	saved := computer.Load(h.a.prefs)
	if saved == nil || *saved != o.paired() {
		t.Fatalf("saved %+v, want %+v", saved, o.paired())
	}
}

func TestTheComputerEntryShowsTheNameOfTheComputerFollowed(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.until("the Session list", func() bool { return h.hasText("On the box") })
	h.openSettingsTab("computer")
	if !h.hasText("Ubuntu box") || !h.hasButton("Use this computer") {
		t.Fatalf("%q", h.tt.Texts())
	}
}

func TestALinkThatIsNotAGatewayOrWhoseTokenIsRejectedChangesNothing(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>router</html>")) }))
	t.Cleanup(web.Close)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	for name, tc := range map[string]struct{ link, message string }{
		"not a link":     {"hello world", "That is not a pairing link."},
		"only the token": {"abc123", "The link has no address."},
		"a web page":     {web.URL + "/#pair=t", "does not answer as a Droi Gateway"},
		"a wrong token":  {o.gw.URL() + "/#pair=wrong", "does not accept this Pairing Token"},
		"nothing there":  {deadURL + "/#pair=t", "Could not reach"},
	} {
		t.Run(name, func(t *testing.T) {
			rl := &relaunches{}
			h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t); cfg.Relaunch = rl.relaunch })
			h.openSettingsTab("computer")
			h.click("Pairing link")
			h.typeInto(tc.link)
			h.click("Follow computer")
			h.until("the refusal", func() bool { return h.hasText(tc.message) })
			if rl.n != 0 || computer.Load(h.a.prefs) != nil {
				t.Fatalf("relaunched %d times, saved %+v", rl.n, computer.Load(h.a.prefs))
			}
			if h.a.following() {
				t.Fatal("the window follows another computer")
			}
		})
	}
}

func TestUseThisComputerForgetsTheOtherAndRelaunches(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	rl := &relaunches{}
	h := followHarness(t, localSessions(), o.paired(), rl, nil)
	computer.Save(h.a.prefs, o.paired())
	h.until("the Session list", func() bool { return h.hasText("On the box") })
	h.openSettingsTab("computer")
	h.click("Use this computer")
	h.until("the relaunch", func() bool { return rl.n == 1 })
	if computer.Load(h.a.prefs) != nil {
		t.Fatal("the other computer is still saved")
	}
}

func TestAnUnreachableComputerShowsTheErrorAndUseThisComputerSwitchesBack(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	p := o.paired()
	o.gw.Close()
	rl := &relaunches{}
	h := followHarness(t, localSessions(), p, rl, nil)
	computer.Save(h.a.prefs, p)
	h.until("the error", func() bool { return h.hasText("Could not reach Ubuntu box") && h.hasButton("Use this computer") })
	if h.hasText("On this Mac") {
		t.Fatal("this computer's Sessions are shown in place of the other's")
	}
	h.click("Use this computer")
	h.until("the relaunch", func() bool { return rl.n == 1 })
	if computer.Load(h.a.prefs) != nil {
		t.Fatal("the other computer is still saved")
	}
}

func TestFollowingAnotherComputerLeavesThisComputersGatewayServingPhones(t *testing.T) {
	o := startOtherComputer(t, boxSessions(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.until("the other computer's Session", func() bool { return h.hasText("On the box") })
	if err := gateway.CheckToken(context.Background(), h.gw.URL(), "pairing-token"); err != nil {
		t.Fatalf("a paired phone is refused by this computer's Gateway: %v", err)
	}
	if !h.ctl.Status().Connected {
		t.Fatal("this computer's own connection is gone")
	}
}
