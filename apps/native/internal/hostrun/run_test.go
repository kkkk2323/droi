//go:build !windows

package hostrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

// TestStandInDroid is the droid the tests run: standInDroid re-executes the
// test binary into it, so it needs no other program.
func TestStandInDroid(t *testing.T) {
	if os.Getenv("DROI_STAND_IN") == "" {
		return
	}
	args := flag.Args()
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println(os.Getenv("DROI_STAND_IN_VERSION"))
		return
	}
	port := ""
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port = args[i+1]
		}
	}
	if err := os.WriteFile(os.Getenv("DROI_STAND_IN_PID"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	go func() { <-term; os.Exit(0) }()
	err := http.ListenAndServe("127.0.0.1:"+port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		for {
			typ, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			_ = c.Write(r.Context(), typ, append([]byte("echo:"), data...))
		}
	}))
	t.Fatal(err)
}

type box struct {
	t       *testing.T
	home    string
	env     map[string]string
	pidFile string
	port    int
}

// newBox is a computer: a home folder, XDG folders, and a droid at version
// on its PATH, all temporary.
func newBox(t *testing.T, version string) *box {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs a shell")
	}
	root := t.TempDir()
	b := &box{t: t, home: filepath.Join(root, "home"), pidFile: filepath.Join(root, "daemon.pid")}
	bin := filepath.Join(root, "bin")
	for _, d := range []string{b.home, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec \"" + exe + "\" -test.run='^TestStandInDroid$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "droid"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DROI_STAND_IN", "1")
	t.Setenv("DROI_STAND_IN_VERSION", version)
	t.Setenv("DROI_STAND_IN_PID", b.pidFile)
	if b.port, err = host.PickFreePort(); err != nil {
		t.Fatal(err)
	}
	b.env = map[string]string{
		"PATH":            bin,
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"FACTORY_API_KEY": "fk-test",
	}
	return b
}

func (b *box) settingsFile() string {
	return filepath.Join(b.env["XDG_CONFIG_HOME"], "droi", "settings.json")
}

func (b *box) token() string {
	b.t.Helper()
	raw, err := os.ReadFile(b.settingsFile())
	if err != nil {
		b.t.Fatal(err)
	}
	var s host.Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		b.t.Fatal(err)
	}
	return s.PairingToken
}

// syncBuffer is an io.Writer a test reads while Run writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type running struct {
	b      *box
	out    *syncBuffer
	cancel context.CancelFunc
	done   chan error
}

func (b *box) options(out io.Writer) Options {
	return Options{
		Home:    b.home,
		Env:     func(k string) string { return b.env[k] },
		Version: "9.9.9",
		Port:    b.port,
		Out:     out,
	}
}

func (b *box) start(mod func(*Options)) *running {
	b.t.Helper()
	out := &syncBuffer{}
	o := b.options(out)
	if mod != nil {
		mod(&o)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{b: b, out: out, cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- Run(ctx, o) }()
	b.t.Cleanup(func() { cancel(); <-r.done })
	return r
}

// stop is what Ctrl-C does.
func (r *running) stop() error {
	r.cancel()
	select {
	case err := <-r.done:
		r.done <- err
		return err
	case <-time.After(20 * time.Second):
		r.b.t.Fatal("Run did not return after the stop signal")
		return nil
	}
}

func (r *running) waitForOutput(sub string) {
	r.b.t.Helper()
	deadline := time.After(30 * time.Second)
	for !strings.Contains(r.out.String(), sub) {
		select {
		case err := <-r.done:
			r.b.t.Fatalf("Run ended (%v) before printing %q:\n%s", err, sub, r.out.String())
		case <-deadline:
			r.b.t.Fatalf("no %q in the output:\n%s", sub, r.out.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (r *running) base() string { return "http://127.0.0.1:" + strconv.Itoa(r.b.port) }

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestHostPrintsThePairingLinkServesMetaAndTakesAClient(t *testing.T) {
	b := newBox(t, "0.240.0")
	r := b.start(func(o *Options) { o.Terminal = true })
	r.waitForOutput("Pairing link: ")
	token := b.token()

	out := r.out.String()
	var link string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "Pairing link: "); ok {
			link = strings.TrimSpace(rest)
		}
	}
	if !strings.HasPrefix(link, "http://") || !strings.HasSuffix(link, ":"+strconv.Itoa(b.port)+"/#pair="+token) {
		t.Fatalf("pairing link %q for token %q and port %d", link, token, b.port)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("no QR code on a terminal:\n%s", out)
	}

	res, err := http.Get(r.base() + gateway.MetaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta gateway.Meta
	err = json.NewDecoder(res.Body).Decode(&meta)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(b.settingsFile())
	var s host.Settings
	_ = json.Unmarshal(raw, &s)
	if meta.App != "Droi" || meta.Name != gateway.ComputerName() || meta.ComputerID != s.ComputerID || meta.ComputerID == "" || meta.Version != "9.9.9" {
		t.Fatalf("meta %+v, settings %+v", meta, s)
	}
	if len(gateway.LANInterfaceAddresses()) > 0 && !meta.RemoteAccess {
		t.Fatal("Remote Access is off")
	}

	// A paired Client reaches the Daemon through the Gateway.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(b.port)+gateway.DaemonPath+"?token="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := c.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_, got, err := c.Read(ctx)
	if err != nil || string(got) != "echo:ping" {
		t.Fatalf("through the Gateway: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(b.env["XDG_CONFIG_HOME"], "droi", "logs", "daemon.log")); err != nil {
		t.Fatalf("the Daemon's log is not under the data folder: %v", err)
	}
}

func TestStopEndsTheGatewayAndTheDaemon(t *testing.T) {
	b := newBox(t, "0.240.0")
	r := b.start(nil)
	r.waitForOutput("Pairing link: ")
	raw, err := os.ReadFile(b.pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(raw))
	if pid == 0 || !alive(pid) {
		t.Fatalf("no Daemon running: %q", raw)
	}
	if err := r.stop(); err != nil {
		t.Fatal(err)
	}
	if alive(pid) {
		t.Fatalf("the Daemon (pid %d) is still running", pid)
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(b.port), time.Second); err == nil {
		conn.Close()
		t.Fatal("the Gateway still listens")
	}
}

func TestDeletingThePairingTokenIssuesANewOne(t *testing.T) {
	b := newBox(t, "0.240.0")
	r := b.start(nil)
	r.waitForOutput("Pairing link: ")
	first := b.token()
	if err := r.stop(); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(b.settingsFile())
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	delete(settings, "pairingToken")
	raw, _ = json.Marshal(settings)
	if err := os.WriteFile(b.settingsFile(), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	again := b.start(nil)
	again.waitForOutput("Pairing link: ")
	if second := b.token(); second == "" || second == first {
		t.Fatalf("token %q after deleting %q", second, first)
	}
	if !strings.Contains(again.out.String(), "#pair="+b.token()) {
		t.Fatalf("the link carries another token:\n%s", again.out.String())
	}
}

func TestSecondHostRefusesToStart(t *testing.T) {
	b := newBox(t, "0.240.0")
	b.start(nil).waitForOutput("Pairing link: ")

	out := &syncBuffer{}
	err := Run(context.Background(), b.options(out))
	if !errors.Is(err, ErrHostRunning) {
		t.Fatalf("got %v, want ErrHostRunning", err)
	}
	if strings.Contains(out.String(), "Pairing link") {
		t.Fatalf("a refused Host printed a link:\n%s", out.String())
	}
}

func TestHostRefusesToStartBesideADroidApp(t *testing.T) {
	b := newBox(t, "0.240.0")
	// What the app runs: a Gateway on the usual port.
	app, err := gateway.Start(gateway.Options{Port: b.port, Version: "1.0.0", ComputerName: "app", ComputerID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := Run(context.Background(), b.options(io.Discard)); !errors.Is(err, ErrHostRunning) {
		t.Fatalf("got %v, want ErrHostRunning", err)
	}
	if _, err := os.Stat(b.pidFile); err == nil {
		t.Fatal("started a Daemon beside the app")
	}
}

func TestHostRefusesAnOlderDroidAndNamesTheVersionNeeded(t *testing.T) {
	b := newBox(t, "0.200.0")
	err := Run(context.Background(), b.options(io.Discard))
	if err == nil || !strings.Contains(err.Error(), host.MinDroid) || !strings.Contains(err.Error(), "0.200.0") {
		t.Fatalf("got %v, want a refusal naming 0.200.0 and %s", err, host.MinDroid)
	}
	if _, err := os.Stat(b.pidFile); err == nil {
		t.Fatal("started a Daemon from an old droid")
	}
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(b.port), time.Second); err == nil {
		t.Fatal("the Gateway listens although the Host refused to start")
	}
}

func TestHostSaysWhenThereIsNoDroid(t *testing.T) {
	b := newBox(t, "0.240.0")
	b.env["PATH"] = t.TempDir()
	err := Run(context.Background(), b.options(io.Discard))
	if err == nil || !strings.Contains(err.Error(), "droid") {
		t.Fatalf("got %v", err)
	}
}

func TestScratchWorkspacesAClientTrashesGoToTheFreedesktopTrash(t *testing.T) {
	b := newBox(t, "0.240.0")
	r := b.start(nil)
	r.waitForOutput("Pairing link: ")
	token := b.token()

	post := func(path string) (int, []byte) {
		t.Helper()
		res, err := http.Post(r.base()+path, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, body
	}
	status, body := post(gateway.ScratchPath + "?token=" + token)
	var made struct{ Path string }
	if status != http.StatusCreated || json.Unmarshal(body, &made) != nil || made.Path == "" {
		t.Fatalf("create: %d %s", status, body)
	}
	if want := filepath.Join(b.home, ".droi", "chats"); filepath.Dir(made.Path) != want {
		t.Fatalf("scratch folder %s, want it under %s", made.Path, want)
	}
	if err := os.WriteFile(filepath.Join(made.Path, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, body = post(gateway.ScratchTrashPath + "?token=" + token + "&path=" + made.Path)
	if status != http.StatusNoContent {
		t.Fatalf("trash: %d %s", status, body)
	}
	trash := filepath.Join(b.env["XDG_DATA_HOME"], "Trash")
	name := filepath.Base(made.Path)
	if _, err := os.Stat(filepath.Join(trash, "files", name, "a.txt")); err != nil {
		t.Fatalf("not in the Trash: %v", err)
	}
	info, err := os.ReadFile(filepath.Join(trash, "info", name+".trashinfo"))
	if err != nil || !strings.Contains(string(info), "Path="+made.Path+"\n") {
		t.Fatalf("trash info %q: %v", info, err)
	}
	if _, err := os.Stat(made.Path); !os.IsNotExist(err) {
		t.Fatal("the folder is still in place")
	}
}

func TestDataFoldersFollowXDG(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := ConfigDir(env(map[string]string{"XDG_CONFIG_HOME": "/x/config"}), "/h"); got != "/x/config/droi" {
		t.Errorf("config: %q", got)
	}
	if got := ConfigDir(env(nil), "/h"); got != "/h/.config/droi" {
		t.Errorf("default config: %q", got)
	}
	// A relative XDG path is not valid by the specification.
	if got := ConfigDir(env(map[string]string{"XDG_CONFIG_HOME": "rel"}), "/h"); got != "/h/.config/droi" {
		t.Errorf("relative config: %q", got)
	}
	if got := TrashDir(env(map[string]string{"XDG_DATA_HOME": "/x/data"}), "/h"); got != "/x/data/Trash" {
		t.Errorf("trash: %q", got)
	}
	if got := TrashDir(env(nil), "/h"); got != "/h/.local/share/Trash" {
		t.Errorf("default trash: %q", got)
	}
}
