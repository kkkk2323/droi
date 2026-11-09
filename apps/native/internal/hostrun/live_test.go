//go:build !windows

package hostrun_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automationwork"
	"github.com/kkkk2323/droi/apps/native/v2/internal/hostrun"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
)

// liveHost is the Host as the app runs it, with its Services and its
// Gateway, on a real Daemon in a throwaway HOME. Only with FACTORY_API_KEY
// set; DROID_PATH picks the droid (Droi's 1.4.1 by default),
// DROI_LIVE_MODEL the model and FACTORY_API_BASE_URL the proxy.
type liveHost struct {
	t                   *testing.T
	key, model, baseURL string
	droidPath, bin      string
	root, home, data    string
	logFile             string

	h   *host.Host
	svc *hostrun.Services
	gw  *gateway.Gateway
	// api is a Client's side of the Gateway's endpoints; socket is the
	// Gateway's Daemon socket URL.
	api    *gateway.Client
	socket string
	// daemon is a connection straight to the Daemon, for checks.
	daemon *droid.Client
}

func newLiveHost(t *testing.T) *liveHost {
	t.Helper()
	key := os.Getenv("FACTORY_API_KEY")
	if key == "" {
		t.Skip("FACTORY_API_KEY is not set")
	}
	lh := &liveHost{t: t, key: key, model: os.Getenv("DROI_LIVE_MODEL"), baseURL: os.Getenv("FACTORY_API_BASE_URL"), droidPath: os.Getenv("DROID_PATH")}
	if lh.model == "" {
		lh.model = "glm-5.3-flash"
	}
	realHome, _ := os.UserHomeDir()
	if lh.droidPath == "" {
		lh.droidPath = filepath.Join(realHome, "Library", "Application Support", "Droi", "droid", "1.4.1", "droid")
	}
	if _, err := os.Stat(lh.droidPath); err != nil {
		t.Skipf("no droid at %s (set DROID_PATH): %v", lh.droidPath, err)
	}
	lh.root, _ = filepath.EvalSymlinks(t.TempDir())
	// The Daemon runs `<this binary> host-tools <server>` for the Host's tool
	// servers. Built before HOME moves, which holds Go's cache.
	lh.bin = filepath.Join(lh.root, "Droi")
	if out, err := exec.Command("go", "build", "-o", lh.bin, "github.com/kkkk2323/droi/apps/native/v2").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	lh.home, lh.data = filepath.Join(lh.root, "home"), filepath.Join(lh.root, "data")
	for _, d := range []string{lh.home, lh.data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The agents commit in a HOME of their own.
	gitconfig := "[user]\n\tname = Droi Live Test\n\temail = live-test@droi.invalid\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(filepath.Join(lh.home, ".gitconfig"), []byte(gitconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	// The Daemon takes the Host's environment, HOME included.
	t.Setenv("HOME", lh.home)
	lh.logFile = filepath.Join(lh.root, "host.log")
	f, err := os.Create(lh.logFile)
	if err != nil {
		t.Fatal(err)
	}
	prev := log.Writer()
	log.SetOutput(f)
	t.Cleanup(func() {
		lh.stop()
		log.SetOutput(prev)
		_ = f.Close()
		if t.Failed() {
			lh.dumpLogs()
		}
	})
	lh.start()
	return lh
}

func (lh *liveHost) env(k string) string {
	switch k {
	case "HOME":
		return lh.home
	case "FACTORY_API_KEY":
		return lh.key
	case "FACTORY_API_BASE_URL":
		return lh.baseURL
	}
	return os.Getenv(k)
}

// start starts the Host, its Services and its Gateway on loopback, as the
// app wires them, and waits for the Daemon.
func (lh *liveHost) start() {
	t := lh.t
	t.Helper()
	var svc *hostrun.Services
	h := host.New(host.Config{
		UserData:          lh.data,
		Home:              lh.home,
		Version:           "live",
		Env:               lh.env,
		Executable:        lh.bin,
		Overlay:           func() host.OverlayOptions { return svc.Overlay(host.OverlayOptions{}) },
		ClientHiddenTools: func() []string { return svc.ClientHiddenTools() },
	})
	path := lh.droidPath
	if err := h.Settings.Update(func(s *host.Settings) { s.DroidPath = &path }); err != nil {
		t.Fatal(err)
	}
	svc = hostrun.StartServices(h)
	opts := h.GatewayOptions("live", func(string) error { return nil })
	opts.Port = freePort(t)
	opts.Automations = svc.GatewayAutomations()
	opts.Workstreams = svc.GatewayWorkstreams()
	gw, err := gateway.Start(opts)
	if err != nil {
		svc.Stop()
		t.Fatal(err)
	}
	h.Start()
	lh.h, lh.svc, lh.gw = h, svc, gw
	lh.socket = strings.Replace(gw.URL(), "http", "ws", 1) + gateway.DaemonPath + "?" + gateway.TokenQuery + "=" + h.Settings.Get().PairingToken
	if lh.api, err = gateway.NewClient(lh.socket); err != nil {
		t.Fatal(err)
	}
	lh.dialDaemon()
}

func (lh *liveHost) dialDaemon() {
	t := lh.t
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url, err := lh.h.WaitForDaemonURL(ctx)
	if err != nil {
		t.Fatalf("the Daemon did not start: %v (%s)", err, lh.h.Daemon.State().Reason)
	}
	for lh.daemon == nil || lh.daemon.Err() != nil {
		lh.daemon, err = droid.Dial(ctx, droid.Options{URL: url, Credential: &droid.Credential{APIKey: lh.key}, RequestTimeout: 3 * time.Minute})
		if err != nil {
			if ctx.Err() != nil {
				t.Fatalf("no connection to the Daemon: %v", err)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	// The Host's own Sessions go through its Runner, which connects on its
	// own a moment after the Daemon answers.
	for {
		if _, err := lh.svc.Sessions.Client(); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("the Host's Runner did not connect to the Daemon")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (lh *liveHost) stop() {
	if lh.daemon != nil {
		_ = lh.daemon.Close()
		lh.daemon = nil
	}
	if lh.gw != nil {
		_ = lh.gw.Close()
		lh.gw = nil
	}
	if lh.svc != nil {
		lh.svc.Stop()
		lh.svc = nil
	}
	if lh.h != nil {
		lh.h.Stop()
		lh.h = nil
	}
}

// dumpLogs logs what the Host and the Daemon said that may tell why a check
// failed.
func (lh *liveHost) dumpLogs() {
	b, _ := os.ReadFile(lh.logFile)
	lines := strings.Split(string(b), "\n")
	lh.t.Logf("host log (last 80 lines):\n%s", strings.Join(lines[max(0, len(lines)-80):], "\n"))
	_ = filepath.Walk(filepath.Join(lh.home, ".factory"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".log") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, line := range strings.Split(string(b), "\n") {
			l := strings.ToLower(line)
			if strings.Contains(l, "droi-") || strings.Contains(l, "error") && (strings.Contains(l, "mcp") || strings.Contains(l, "tool")) {
				lh.t.Logf("%s: %.600s", filepath.Base(p), line)
			}
		}
		return nil
	})
	if b, err := os.ReadFile(filepath.Join(lh.data, "logs", "daemon.log")); err == nil {
		lines := strings.Split(string(b), "\n")
		lh.t.Logf("daemon.log (last 40 lines):\n%s", strings.Join(lines[max(0, len(lines)-40):], "\n"))
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitFor polls ok until it holds or timeout passes.
func waitFor(t *testing.T, timeout time.Duration, what string, ok func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Errorf("timed out after %s waiting for %s", timeout, what)
			return false
		}
		time.Sleep(time.Second)
	}
	return true
}

// liveSession is a Session as the Daemon lists it.
type liveSession struct {
	SessionID  string                `json:"sessionId"`
	Title      string                `json:"title"`
	Cwd        string                `json:"cwd"`
	ArchivedAt any                   `json:"archivedAt"`
	Tags       []protocol.SessionTag `json:"tags"`
}

func (s liveSession) tag(name string) (protocol.SessionTag, bool) {
	for _, t := range s.Tags {
		if t.Name == name {
			return t, true
		}
	}
	return protocol.SessionTag{}, false
}

func (lh *liveHost) session(t *testing.T, ctx context.Context, id string) (liveSession, bool) {
	yes := true
	res, err := lh.daemon.ListAvailableSessions(ctx, protocol.ListAvailableSessionsParams{IncludeArchived: &yes})
	if err != nil {
		t.Errorf("list_available_sessions: %v", err)
		return liveSession{}, false
	}
	b, _ := json.Marshal(res.Sessions)
	var all []liveSession
	_ = json.Unmarshal(b, &all)
	for _, s := range all {
		if s.SessionID == id {
			return s, true
		}
	}
	return liveSession{}, false
}

func (lh *liveHost) opened(t *testing.T, ctx context.Context, id string) bool {
	res, err := lh.daemon.ListOpenedSessions(ctx, protocol.ListOpenedSessionsParams{})
	if err != nil {
		t.Errorf("list_opened_sessions: %v", err)
		return false
	}
	b, _ := json.Marshal(res.Sessions)
	return strings.Contains(string(b), `"`+id+`"`)
}

// settingsFile is a Session's settings file, as the Daemon wrote it.
func (lh *liveHost) settingsFile(id string) (map[string]any, string) {
	matches, _ := filepath.Glob(filepath.Join(lh.home, ".factory", "sessions", "*", id+".settings.json"))
	direct, _ := filepath.Glob(filepath.Join(lh.home, ".factory", "sessions", id+".settings.json"))
	matches = append(matches, direct...)
	if len(matches) == 0 {
		return nil, ""
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, ""
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, string(b)
}

func disabledIDs(settings map[string]any) []string {
	var out []string
	if list, ok := settings["disabledToolIds"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// checkHidden checks that a Session's settings file holds every id of want
// in its disabledToolIds.
func (lh *liveHost) checkHidden(t *testing.T, what, id string, want []string) {
	t.Helper()
	var raw string
	var got []string
	ok := waitFor(t, 20*time.Second, what+"'s settings file", func() bool {
		var m map[string]any
		m, raw = lh.settingsFile(id)
		got = disabledIDs(m)
		for _, w := range want {
			if !slices.Contains(got, w) {
				return false
			}
		}
		return true
	})
	if !ok {
		t.Errorf("%s %s: disabledToolIds %q, want %q in them; settings file: %.1500s", what, id, got, want, raw)
		return
	}
	t.Logf("%s %s: disabledToolIds %q", what, id, got)
}

// liveMessage is a Session message as get_session_messages answers it:
// the newest 100, oldest first.
type liveMessage struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Name string `json:"name"`
	} `json:"content"`
}

func (m liveMessage) text() string {
	var b strings.Builder
	for _, c := range m.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func (m liveMessage) tools() []string {
	var out []string
	for _, c := range m.Content {
		if c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out
}

func (lh *liveHost) messages(ctx context.Context, id string) []liveMessage {
	limit := float64(100)
	page, err := lh.daemon.GetSessionMessages(ctx, protocol.GetSessionMessagesParams{SessionID: id, Limit: &limit})
	if err != nil {
		lh.t.Logf("get_session_messages %s: %v", id, err)
		return nil
	}
	b, _ := json.Marshal(page.Messages)
	var out []liveMessage
	_ = json.Unmarshal(b, &out)
	// The Daemon answers the newest first.
	slices.Reverse(out)
	return out
}

// mcpTools counts the MCP tools, by server, that a Session's model is
// given (daemon.get_context_breakdown): the ones its disabledToolIds leave.
func mcpTools(t *testing.T, ctx context.Context, c *droid.Client, id string) map[string]int {
	t.Helper()
	res, err := c.GetContextBreakdown(ctx, protocol.GetContextBreakdownParams{SessionID: id})
	if err != nil {
		t.Errorf("get_context_breakdown %s: %v", id, err)
		return nil
	}
	out := map[string]int{}
	for _, s := range res.MCPServers {
		out[s.Name] = int(s.ToolCount)
	}
	return out
}

func transcript(msgs []liveMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %.400s", m.Role, m.text())
		if tools := m.tools(); len(tools) > 0 {
			fmt.Fprintf(&b, " [tools %s]", strings.Join(tools, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func runOf(runs []gateway.AutomationRun, id string) (gateway.AutomationRun, bool) {
	for _, r := range runs {
		if r.RunID == id {
			return r, true
		}
	}
	return gateway.AutomationRun{}, false
}

// TestLiveAutomations runs Droi's own Automations (ADR 0035) on a real
// Daemon through the Host's Gateway.
func TestLiveAutomations(t *testing.T) {
	lh := newLiveHost(t)
	began := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	workspace := filepath.Join(lh.root, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	wantHidden := automationwork.RunHiddenTools()

	// 2, set up first: it runs on the next minutes while the rest goes on.
	tick, err := lh.api.SaveAutomation(ctx, gateway.Automation{Name: "Live tick", Prompt: "Reply with the word TICK and nothing else.", Schedule: "* * * * *", Model: lh.model, Autonomy: "high", WorkingDirectory: workspace})
	if err != nil {
		t.Fatalf("saving the minute automation: %v", err)
	}
	t.Logf("2: saved %s, next run %s", tick.ID, tick.NextRunAt)

	t.Run("4 client session", func(t *testing.T) {
		c, err := droid.Dial(ctx, droid.Options{URL: lh.socket, Credential: &droid.Credential{APIKey: gateway.APIKeyPlaceholder}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		init, err := c.InitializeSession(ctx, protocol.InitializeSessionParams{MachineID: "local", Cwd: workspace, ModelID: lh.model})
		if err != nil {
			t.Fatal(err)
		}
		lh.checkHidden(t, "4: the Client Session", init.SessionID, append(slices.Clone(automationwork.FactoryAutomationTools), "mcp:droi-workstream"))
		var servers []protocol.MCPServerStatusInfo
		waitFor(t, 60*time.Second, "the Host's tool servers to connect", func() bool {
			res, err := c.ListMCPServers(ctx, protocol.ListMCPServersParams{SessionID: init.SessionID})
			if err != nil {
				t.Logf("list_mcp_servers: %v", err)
				return false
			}
			servers = res.Servers
			n := 0
			for _, s := range servers {
				if (s.Name == automationwork.ServerName || s.Name == "droi-workstream") && s.Status == "connected" {
					n++
				}
			}
			return n == 2
		})
		for _, s := range servers {
			t.Logf("4: MCP server %s: %s %s", s.Name, s.Status, s.Error)
		}
		res, err := c.ListMCPTools(ctx, protocol.ListMCPToolsParams{SessionID: init.SessionID})
		if err != nil {
			t.Fatal(err)
		}
		enabled := map[string]bool{}
		for _, tool := range res.Tools {
			if strings.HasPrefix(tool.ServerName, "droi-") {
				enabled[tool.ServerName+"/"+tool.Name] = tool.IsEnabled
			}
		}
		// isEnabled is the tool's own switch, not the Session's disabledToolIds.
		t.Logf("4: Droi's MCP tools in the Client Session (name: enabled): %v", enabled)
		given := mcpTools(t, ctx, c, init.SessionID)
		t.Logf("4: MCP tools the Client Session's model is given, by server: %v", given)
		if given[automationwork.ServerName] != 5 || given["droi-workstream"] != 0 {
			t.Errorf("4: the Client Session's model is given %v, want 5 of %s and none of droi-workstream", given, automationwork.ServerName)
		}
		_, _ = c.CloseSession(ctx, protocol.CloseSessionParams{SessionID: init.SessionID})
	})

	t.Run("5 factory move", func(t *testing.T) {
		yes := true
		created, err := lh.daemon.CreateAutomation(ctx, protocol.CreateAutomationParams{
			ID: "live-factory", Name: "Live factory", Instructions: "Reply with the word FACTORY.", Schedule: "0 9 * * *",
			Model: lh.model, Paused: &yes, SkipFirstRun: &yes,
		})
		if err != nil {
			t.Fatalf("daemon.create_automation: %v", err)
		}
		b, _ := json.Marshal(created)
		t.Logf("5: create_automation answered %s", b)
		list, err := lh.daemon.ListAutomations(ctx, protocol.ListAutomationsParams{})
		if err != nil {
			t.Fatal(err)
		}
		var entry *protocol.AutomationEntry
		for i := range list.Automations {
			if list.Automations[i].ID == "live-factory" || list.Automations[i].ID == created.AutomationID {
				entry = &list.Automations[i]
			}
		}
		if entry == nil {
			b, _ := json.Marshal(list)
			t.Fatalf("5: the Daemon does not list it: %.1500s", b)
		}
		t.Logf("5: Factory automation %s uuid %q at %s, schedule %q", entry.ID, entry.Uuid, entry.Path, entry.Schedule)
		if entry.Uuid == "" {
			t.Error("5: the Factory automation has no uuid")
		}
		if err := os.MkdirAll(filepath.Join(entry.Path, "memory"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(entry.Path, "memory", "notes.md"), []byte("# Notes\nkept\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(entry.Path, ".memory-edit.lock"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(entry.Path, ".memory-edit.lock", "owner.json"), []byte(`{"pid":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		factory, err := lh.api.FactoryAutomations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		listed := false
		for _, f := range factory {
			if f.ID == entry.ID {
				listed = true
				t.Logf("5: /automations/factory lists %s: schedule %q, local %q %q, not movable %q", f.ID, f.Schedule, f.LocalSchedule, f.LocalTimezone, f.NotMovable)
			}
		}
		if !listed {
			t.Fatalf("5: /automations/factory does not list %s: %+v", entry.ID, factory)
		}
		moved, err := lh.api.MoveFactoryAutomation(ctx, entry.ID)
		if err != nil {
			t.Fatalf("5: move: %v", err)
		}
		t.Logf("5: moved to %s (folder %s), forkedFrom %q, forkedFromName %q, schedule %q %q, deleteError %q", moved.Automation.ID, moved.Automation.Folder, moved.Automation.ForkedFrom, moved.Automation.ForkedFromName, moved.Automation.Schedule, moved.Automation.Timezone, moved.DeleteError)
		if moved.DeleteError != "" {
			t.Errorf("5: delete failed: %s", moved.DeleteError)
		}
		if entry.Uuid != "" && moved.Automation.ForkedFrom != entry.Uuid {
			t.Errorf("5: forkedFrom %q, want the uuid %q", moved.Automation.ForkedFrom, entry.Uuid)
		}
		if b, err := os.ReadFile(filepath.Join(moved.Automation.Folder, "memory", "notes.md")); err != nil || !strings.Contains(string(b), "kept") {
			t.Errorf("5: the copy's memory/notes.md: %q, %v", b, err)
		}
		if _, err := os.Stat(filepath.Join(moved.Automation.Folder, ".memory-edit.lock")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("5: the copy has the lock folder: %v", err)
		}
		if _, err := os.Stat(entry.Path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("5: the Factory folder %s is still there: %v", entry.Path, err)
		}
	})

	var pong gateway.AutomationRun
	t.Run("1 run now", func(t *testing.T) {
		a, err := lh.api.SaveAutomation(ctx, gateway.Automation{Name: "Live pong", Prompt: "Reply with the word PONG and nothing else.", Schedule: "0 3 1 1 *", Model: lh.model, Autonomy: "high"})
		if err != nil {
			t.Fatal(err)
		}
		run, err := lh.api.RunAutomation(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("1: run %s of %s in Session %s", run.RunID, a.ID, run.SessionID)
		waitFor(t, 4*time.Minute, "the run to end", func() bool {
			runs, err := lh.api.AutomationRuns(ctx, a.ID)
			if err != nil {
				t.Logf("runs: %v", err)
				return false
			}
			pong, _ = runOf(runs, run.RunID)
			return pong.Status != "" && pong.Status != "running"
		})
		t.Logf("1: run ended %s %q, Session %s", pong.Status, pong.Error, pong.SessionID)
		if pong.Status != "succeeded" {
			t.Fatalf("1: the run ended %s: %s", pong.Status, pong.Error)
		}
		s, ok := lh.session(t, ctx, pong.SessionID)
		tag, tagged := s.tag("droi.automation")
		switch {
		case !ok:
			t.Errorf("1: the Daemon does not list the run's Session")
		case !tagged:
			t.Errorf("1: the run's Session has tags %+v", s.Tags)
		case tag.Metadata["runId"] != run.RunID || tag.Metadata["automationId"] != a.ID || tag.Metadata["trigger"] != "manual":
			t.Errorf("1: the run's tag %+v", tag)
		case s.ArchivedAt != nil:
			t.Errorf("1: the run's Session is archived (%v)", s.ArchivedAt)
		}
		t.Logf("1: tag %+v", tag)
		waitFor(t, 30*time.Second, "the run's Session to close", func() bool { return !lh.opened(t, ctx, pong.SessionID) })
		lh.checkHidden(t, "1: the run's Session", pong.SessionID, wantHidden)
	})

	t.Run("3 turn outcome", func(t *testing.T) {
		if pong.SessionID == "" {
			t.Skip("no run")
		}
		msgs := lh.messages(ctx, pong.SessionID)
		turnID := ""
		for _, m := range msgs {
			if m.Role == "user" && strings.Contains(m.text(), "PONG") {
				turnID = m.ID
				break
			}
		}
		if turnID == "" {
			t.Fatalf("3: no prompt in the run's Session:\n%s", transcript(msgs))
		}
		res, err := lh.daemon.GetSessionTurnOutcome(ctx, protocol.GetSessionTurnOutcomeParams{SessionID: pong.SessionID, TurnID: turnID, TimeoutMs: 5000})
		if err != nil {
			t.Fatalf("3: get_session_turn_outcome: %v", err)
		}
		t.Logf("3: get_session_turn_outcome(%s) answered %s", turnID, res.Raw)
		v, err := res.Value()
		got, ok := v.(*protocol.DaemonGetSessionTurnOutcomeResultAvailable)
		if err != nil || !ok {
			t.Fatalf("3: not available: %s", res.Raw)
		}
		var o struct {
			TurnID string `json:"turnId"`
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(got.Outcome.Raw, &o)
		if o.TurnID != turnID || o.Reason != string(protocol.AgentTurnCompletionReasonCompleted) || !strings.Contains(got.FinalOutput, "PONG") {
			t.Errorf("3: outcome %s, final output %q", got.Outcome.Raw, got.FinalOutput)
		}
	})

	t.Run("2 scheduled run and pause", func(t *testing.T) {
		var sched gateway.AutomationRun
		deadline := began.Add(3 * time.Minute)
		waitFor(t, time.Until(deadline), "a scheduled run", func() bool {
			runs, _ := lh.api.AutomationRuns(ctx, tick.ID)
			for _, r := range runs {
				if r.Trigger == "schedule" {
					sched = r
					return true
				}
			}
			return false
		})
		if sched.RunID == "" {
			t.FailNow()
		}
		t.Logf("2: scheduled run %s started %s (saved %s)", sched.RunID, sched.StartedAt, began.Format(time.RFC3339))
		if err := lh.api.PauseAutomation(ctx, tick.ID, true); err != nil {
			t.Fatal(err)
		}
		paused := time.Now()
		waitFor(t, 3*time.Minute, "the runs to end", func() bool {
			runs, _ := lh.api.AutomationRuns(ctx, tick.ID)
			for _, r := range runs {
				if r.Status == "running" {
					return false
				}
			}
			return true
		})
		if d := time.Until(paused.Add(75 * time.Second)); d > 0 {
			time.Sleep(d)
		}
		runs, err := lh.api.AutomationRuns(ctx, tick.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			started, _ := time.Parse(time.RFC3339, r.StartedAt)
			t.Logf("2: run %s %s %s started %s ended %s %s", r.RunID, r.Trigger, r.Status, r.StartedAt, r.EndedAt, r.Error)
			if started.After(paused) {
				t.Errorf("2: run %s started after the pause", r.RunID)
			}
			if r.Status != "succeeded" {
				t.Errorf("2: run %s ended %s: %s", r.RunID, r.Status, r.Error)
			}
		}
	})
	t.Logf("model %s, %s", lh.model, time.Since(began).Round(time.Second))
}

// TestLiveWorkstreams runs one of Droi's own Workstreams (ADR 0036) on a
// real Daemon through the Host's Gateway: an orchestrator that starts one
// agent, which commits and reports.
func TestLiveWorkstreams(t *testing.T) {
	lh := newLiveHost(t)
	began := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	repo := filepath.Join(lh.root, "repo")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+lh.home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Live\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", "NOTES.md"), []byte("# Sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(lh.root, "init", "-b", "main", repo)
	git(repo, "add", ".")
	git(repo, "commit", "-m", "first")
	// Agents edit and commit without asking.
	if _, err := lh.daemon.UpdateSessionDefaults(ctx, protocol.UpdateSessionDefaultsParams{AutonomyLevel: protocol.AutonomyLevelHigh, ModelID: lh.model}); err != nil {
		t.Fatalf("daemon.update_session_defaults: %v", err)
	}

	created, err := lh.api.CreateWorkstream(ctx, gateway.NewWorkstream{
		Title: "Live", Workspace: repo, Model: lh.model, AgentModel: lh.model,
		Goal: `Start exactly one agent titled "Readme" whose task is to append the line hello to README.md in its worktree and commit it, then report done. Then wait for its report. When it arrives, reply with the word FINISHED.`,
	})
	if err != nil {
		t.Fatalf("6: create: %v", err)
	}
	orch := created.OrchestratorID
	t.Logf("6: workstream %s, orchestrator %s", created.WorkstreamID, orch)
	find := func() (gateway.Workstream, *gateway.WorkstreamAgent) {
		all, err := lh.api.Workstreams(ctx)
		if err != nil {
			t.Logf("workstreams: %v", err)
			return gateway.Workstream{}, nil
		}
		for _, w := range all {
			if w.ID == created.WorkstreamID {
				if len(w.Agents) > 0 {
					return w, &w.Agents[0]
				}
				return w, nil
			}
		}
		return gateway.Workstream{}, nil
	}

	// 6: the agent reports, and the orchestrator answers it.
	var msgs []liveMessage
	finished := func() bool {
		msgs = lh.messages(ctx, orch)
		heard := false
		for _, m := range msgs {
			if m.Role == "user" && strings.Contains(m.text(), `[From agent "Readme" (`) {
				heard = true
			}
			if heard && m.Role == "assistant" && strings.Contains(m.text(), "FINISHED") {
				return true
			}
		}
		return false
	}
	if !waitFor(t, 10*time.Minute, "the orchestrator to hear the report and answer FINISHED", finished) {
		t.Logf("6: the orchestrator's transcript:\n%s", transcript(msgs))
		if _, a := find(); a != nil {
			t.Logf("6: agent %+v", *a)
			t.Logf("6: the agent's transcript:\n%s", transcript(lh.messages(ctx, a.SessionID)))
		}
		t.FailNow()
	}
	t.Logf("6: the orchestrator's transcript:\n%s", transcript(msgs))
	w, a := find()
	if a == nil {
		t.Fatalf("6: no agent recorded: %+v", w)
	}
	agent := *a
	t.Logf("6: agent %s %q on %s (base %s) at %s, model %s/%s, awaiting %v, last report %+v", agent.SessionID, agent.Title, agent.Branch, agent.BaseBranch, agent.WorktreePath, agent.Model, agent.ReasoningEffort, agent.Awaiting, agent.LastReport)
	if len(w.Agents) != 1 {
		t.Errorf("6: %d agents", len(w.Agents))
	}
	if agent.Branch != "droid/readme" {
		t.Errorf("6: the agent's branch is %q", agent.Branch)
	}
	if info, err := os.Stat(agent.WorktreePath); err != nil || !info.IsDir() {
		t.Errorf("6: the agent's worktree %q: %v", agent.WorktreePath, err)
	}
	if agent.LastReport == nil || agent.LastReport.Kind != "done" {
		t.Errorf("6: the agent's last report %+v", agent.LastReport)
	}
	if files := git(repo, "log", "main..droid/readme", "--name-only", "--format="); !strings.Contains(files, "README.md") {
		t.Errorf("6: the branch's commits touch %q", files)
	} else {
		t.Logf("6: %s", git(repo, "log", "main..droid/readme", "--oneline", "--name-only"))
	}
	if s, ok := lh.session(t, ctx, orch); !ok {
		t.Error("6: the orchestrator is not listed")
	} else if tag, ok := s.tag("droi.workstream"); !ok || tag.Metadata["role"] != "orchestrator" || tag.Metadata["workstreamId"] != created.WorkstreamID {
		t.Errorf("6: the orchestrator's tags %+v", s.Tags)
	} else {
		t.Logf("6: orchestrator tag %+v", tag)
	}
	if s, ok := lh.session(t, ctx, agent.SessionID); ok {
		tag, _ := s.tag("droi.workstream")
		t.Logf("6: agent tag %+v", tag)
		if tag.Metadata["role"] != "agent" || tag.Metadata["orchestratorId"] != orch {
			t.Errorf("6: the agent's tags %+v", s.Tags)
		}
	}
	if given := mcpTools(t, ctx, lh.daemon, orch); given["droi-workstream"] != 5 || given[automationwork.ServerName] != 5 {
		t.Errorf("6: the orchestrator's model is given %v, want 5 tools of droi-workstream and of %s", given, automationwork.ServerName)
	} else {
		t.Logf("6: MCP tools the orchestrator's model is given: %v", given)
	}
	if given := mcpTools(t, ctx, lh.daemon, agent.SessionID); given["droi-workstream"] != 1 {
		t.Errorf("6: the agent's model is given %v, want only report of droi-workstream", given)
	} else {
		t.Logf("6: MCP tools the agent's model is given: %v", given)
	}
	lh.checkHidden(t, "6: the orchestrator", orch, append(slices.Clone(automationwork.FactoryAutomationTools), "mcp:droi-workstream/report"))
	lh.checkHidden(t, "6: the agent", agent.SessionID, append(slices.Clone(automationwork.FactoryAutomationTools), "mcp:droi-workstream/spawn_agent", "mcp:droi-workstream/send_message"))

	t.Run("7 loop tool", func(t *testing.T) {
		c, err := droid.Dial(ctx, droid.Options{URL: lh.socket, Credential: &droid.Credential{APIKey: gateway.APIKeyPlaceholder}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := c.LoadSession(ctx, protocol.LoadSessionParams{SessionID: orch}); err != nil {
			t.Fatal(err)
		}
		before := len(lh.messages(ctx, orch))
		if _, err := c.AddUserMessage(ctx, protocol.AddUserMessageParams{SessionID: orch, Text: `Call your Loop tool once with action "list" (load it with ToolSearch first if you need to), then reply with the word LISTED and what it answered.`}); err != nil {
			t.Fatal(err)
		}
		var used []string
		waitFor(t, 4*time.Minute, "the orchestrator to call Loop", func() bool {
			msgs = lh.messages(ctx, orch)
			used = nil
			done := false
			for _, m := range msgs[min(before, len(msgs)):] {
				used = append(used, m.tools()...)
				if m.Role == "assistant" && strings.Contains(m.text(), "LISTED") {
					done = true
				}
			}
			return done && slices.Contains(used, "Loop")
		})
		t.Logf("7: tools the orchestrator called: %q", used)
		if !slices.Contains(used, "Loop") {
			t.Logf("7: the orchestrator's transcript:\n%s", transcript(msgs[min(before, len(msgs)):]))
		}
	})

	t.Run("8 agent settings", func(t *testing.T) {
		res, err := lh.daemon.LoadSession(ctx, protocol.LoadSessionParams{SessionID: agent.SessionID})
		if err != nil {
			t.Fatal(err)
		}
		var efforts []protocol.ReasoningEffort
		for _, m := range res.AvailableModels {
			if m.ID == res.Settings.ModelID {
				efforts = m.SupportedReasoningEfforts
			}
		}
		current := res.Settings.ReasoningEffort
		var other protocol.ReasoningEffort
		for _, e := range efforts {
			if e != current {
				other = e
			}
		}
		t.Logf("8: the agent runs %s at %q; the model takes %q", res.Settings.ModelID, current, efforts)
		if other == "" {
			t.Skipf("8: %s has no other reasoning effort", res.Settings.ModelID)
		}
		if err := lh.api.SetWorkstreamAgentSettings(ctx, gateway.WorkstreamSettings{ID: created.WorkstreamID, AgentModel: lh.model, AgentReasoningEffort: string(other)}); err != nil {
			t.Fatal(err)
		}
		var got gateway.WorkstreamAgent
		waitFor(t, 30*time.Second, "the agent to take the settings", func() bool {
			_, a := find()
			if a != nil {
				got = *a
			}
			return a != nil && a.ReasoningEffort == string(other)
		})
		m, raw := lh.settingsFile(agent.SessionID)
		t.Logf("8: record %s/%s; settings file reasoningEffort %v", got.Model, got.ReasoningEffort, m["reasoningEffort"])
		if m["reasoningEffort"] != string(other) {
			t.Errorf("8: the agent's settings file: %.1500s", raw)
		}
		if s, ok := lh.session(t, ctx, agent.SessionID); ok {
			tag, _ := s.tag("droi.workstream")
			if tag.Metadata["agentReasoningEffort"] != string(other) {
				t.Errorf("8: the agent's tag %+v", tag)
			}
		}
	})

	handedFrom := ""
	t.Run("9 handoff", func(t *testing.T) {
		res, err := lh.daemon.CompactSession(ctx, protocol.CompactSessionParams{SessionID: agent.SessionID, Mode: "handoff"})
		if err != nil {
			t.Fatalf("9: compact_session handoff: %v", err)
		}
		next := res.NewSessionID
		t.Logf("9: handed %s off to %s", agent.SessionID, next)
		_, listed := lh.session(t, ctx, next)
		t.Logf("9: list_available_sessions lists the new Session: %v", listed)
		waitFor(t, 30*time.Second, "the record to follow the handoff", func() bool {
			_, a := find()
			return a != nil && a.SessionID == next
		})
		hidden := append(slices.Clone(automationwork.FactoryAutomationTools), "mcp:droi-workstream/spawn_agent", "mcp:droi-workstream/send_message")
		lh.checkHidden(t, "9: the handed-off agent", next, hidden)
		if given := mcpTools(t, ctx, lh.daemon, next); given["droi-workstream"] != 1 {
			t.Errorf("9: the handed-off agent's model is given %v, want only report of droi-workstream", given)
		} else {
			t.Logf("9: MCP tools the handed-off agent's model is given: %v", given)
		}
		_, before := lh.settingsFile(next)
		lh.h.RestartDaemon()
		_ = lh.daemon.Close()
		lh.daemon = nil
		lh.dialDaemon()
		if _, err := lh.daemon.LoadSession(ctx, protocol.LoadSessionParams{SessionID: next}); err != nil {
			t.Fatalf("9: loading the handed-off agent after a restart: %v", err)
		}
		lh.checkHidden(t, "9: the handed-off agent after a Daemon restart", next, hidden)
		if given := mcpTools(t, ctx, lh.daemon, next); given["droi-workstream"] != 1 {
			t.Errorf("9: after a Daemon restart the handed-off agent's model is given %v, want only report of droi-workstream", given)
		} else {
			t.Logf("9: after a Daemon restart, MCP tools the handed-off agent's model is given: %v", given)
		}
		if _, after := lh.settingsFile(next); after != before {
			t.Logf("9: its settings file changed on the load:\nbefore %s\nafter %s", before, after)
		}
		handedFrom, agent.SessionID = agent.SessionID, next
	})

	t.Run("10 archive", func(t *testing.T) {
		kept, err := lh.api.ArchiveWorkstream(ctx, created.WorkstreamID)
		if err != nil {
			t.Fatalf("10: archive: %v", err)
		}
		t.Logf("10: kept worktrees %q", kept)
		if !slices.Contains(kept, agent.WorktreePath) {
			t.Errorf("10: kept %q, want %s", kept, agent.WorktreePath)
		}
		if _, err := os.Stat(agent.WorktreePath); err != nil {
			t.Errorf("10: the kept worktree: %v", err)
		}
		w, _ := find()
		if w.ArchivedAt == "" {
			t.Error("10: the workstream is not archived")
		}
		for _, a := range w.Agents {
			if a.ArchivedAt == "" {
				t.Errorf("10: agent %s is not archived", a.SessionID)
			}
		}
		for _, id := range slices.DeleteFunc([]string{orch, agent.SessionID, handedFrom}, func(s string) bool { return s == "" }) {
			var rpc *droid.RPCError
			if _, err := lh.daemon.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id}); !errors.As(err, &rpc) || rpc.Code != -32004 {
				t.Errorf("10: loading archived %s: %v", id, err)
			}
			s, ok := lh.session(t, ctx, id)
			t.Logf("10: %s listed %v, archivedAt %v", id, ok, s.ArchivedAt)
			if ok && s.ArchivedAt == nil {
				t.Errorf("10: %s is listed but not archived", id)
			}
		}
	})

	t.Run("11 subfolder workspace", func(t *testing.T) {
		sub := filepath.Join(repo, "sub")
		c2, err := lh.api.CreateWorkstream(ctx, gateway.NewWorkstream{
			Title: "Live sub", Workspace: sub, Model: lh.model, AgentModel: lh.model,
			Goal: `Start exactly one agent titled "Where" whose task is to run pwd and ls in a shell and report done with what they printed. Then wait for its report. When it arrives, reply with the word FINISHED.`,
		})
		if err != nil {
			t.Fatal(err)
		}
		var ag *gateway.WorkstreamAgent
		waitFor(t, 8*time.Minute, "the sub agent to report", func() bool {
			all, _ := lh.api.Workstreams(ctx)
			for _, w := range all {
				if w.ID == c2.WorkstreamID && len(w.Agents) > 0 {
					ag = &w.Agents[0]
					return ag.LastReport != nil
				}
			}
			return false
		})
		if ag == nil {
			t.FailNow()
		}
		res, err := lh.daemon.LoadSession(ctx, protocol.LoadSessionParams{SessionID: ag.SessionID})
		cwd := ""
		if err == nil {
			cwd = res.Cwd
		}
		t.Logf("11: agent %s, worktree %s, branch %s, session cwd %q, report %+v", ag.SessionID, ag.WorktreePath, ag.Branch, cwd, ag.LastReport)
		if m, _ := lh.settingsFile(ag.SessionID); m != nil {
			if sp, ok := m["systemPrompt"].(map[string]any); ok {
				appended, _ := sp["append"].(string)
				t.Logf("11: the agent's system prompt addition:\n%s", appended)
				if !strings.Contains(appended, "sub") {
					t.Errorf("11: the agent's prompt does not name the subfolder")
				}
			}
		}
		if kept, err := lh.api.ArchiveWorkstream(ctx, c2.WorkstreamID); err != nil {
			t.Errorf("11: archive: %v", err)
		} else {
			t.Logf("11: archived; kept %q", kept)
		}
	})
	t.Logf("model %s, %s", lh.model, time.Since(began).Round(time.Second))
}
