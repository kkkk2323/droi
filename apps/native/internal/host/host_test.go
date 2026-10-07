package host

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSettingsCreatesTokenAndKeepsComputerID(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	s := OpenSettings(file, func(string) string { return "" })
	first := s.Get()
	if first.PairingToken == "" || first.ComputerID == "" {
		t.Fatalf("new settings lack ids: %+v", first)
	}
	st, err := os.Stat(file)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("settings file mode: %v %v", st, err)
	}
	if err := s.Update(func(x *Settings) { x.ScratchFolder = OptString("/tmp/chats") }); err != nil {
		t.Fatal(err)
	}
	again := OpenSettings(file, func(string) string { return "" }).Get()
	if again.ComputerID != first.ComputerID || deref(again.ScratchFolder) != "/tmp/chats" {
		t.Fatalf("reload: %+v", again)
	}
}

func TestSettingsReadsDesktopShellFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(file, []byte(`{"remoteAccess":false,"droidPath":null,"factoryApiBaseUrl":"http://127.0.0.1:9","appendSystemPrompt":"Be brief","pairingHost":null,"scratchFolder":null,"memoryEnabled":true,"memoryModel":null,"pairingToken":"tok","computerId":"cid","apiKey":"fk-x","login":null}`), 0o600)
	s := OpenSettings(file, func(string) string { return "" })
	g := s.Get()
	if g.PairingToken != "tok" || g.ComputerID != "cid" || deref(g.AppendSystemPrompt) != "Be brief" || !g.MemoryEnabled || s.APIKey() != "fk-x" {
		t.Fatalf("%+v", g)
	}
	if g.MemoryModelOrDefault() != DefaultMemoryModel {
		t.Fatal(g.MemoryModelOrDefault())
	}
	env := OpenSettings(file, func(k string) string {
		if k == "FACTORY_API_KEY" {
			return "fk-env"
		}
		return ""
	})
	if env.APIKey() != "fk-env" {
		t.Fatal("env key does not win")
	}
}

func TestPairingHostAndScratchFolderParsing(t *testing.T) {
	for in, want := range map[string]string{"laptop.myhome": "laptop.myhome", "laptop.myhome:41417": "laptop.myhome", "http://a.b:1/x": "a.b", "  ": "", "bad host!": ""} {
		if got := deref(PairingHostOf(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"~": "/h", "~/x/../y": "/h/y", "/abs/": "/abs", "rel": "", "": ""} {
		if got := deref(ScratchFolderOf(in, "/h")); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestLocateDroidOrder(t *testing.T) {
	dir := t.TempDir()
	mk := func(p string) string {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
		return p
	}
	home := filepath.Join(dir, "home")
	local := mk(filepath.Join(home, ".local", "bin", droidBinary()))
	bin := filepath.Join(dir, "bin")
	if got := LocateDroid("", bin, home); got != local {
		t.Fatalf("fallback: %q", got)
	}
	onPath := mk(filepath.Join(bin, droidBinary()))
	if got := LocateDroid("", bin, home); got != onPath {
		t.Fatalf("PATH: %q", got)
	}
	over := mk(filepath.Join(dir, "custom"))
	if got := LocateDroid(over, bin, home); got != over {
		t.Fatalf("override: %q", got)
	}
	if got := LocateDroid(filepath.Join(dir, "missing"), "", filepath.Join(dir, "none")); got != "" {
		t.Fatalf("none: %q", got)
	}
}

func TestDroidBuildReplaced(t *testing.T) {
	p := filepath.Join(t.TempDir(), "droid")
	os.WriteFile(p, []byte("a"), 0o755)
	b := BuildOf(p)
	if b.Replaced() {
		t.Fatal("same file counts as replaced")
	}
	next := p + ".new"
	os.WriteFile(next, []byte("b"), 0o755)
	os.Chtimes(next, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	os.Rename(next, p)
	if !b.Replaced() {
		t.Fatal("replacement not noticed")
	}
	os.Remove(p)
	if b.Replaced() {
		t.Fatal("a missing file is not newer")
	}
}

func TestDaemonArgs(t *testing.T) {
	got := strings.Join(DaemonArgs(4242, []string{"--liveness-fd", "3"}, ""), " ")
	if got != "daemon --host 127.0.0.1 --port 4242 --liveness-fd 3" {
		t.Fatal(got)
	}
}

func encrypt(t *testing.T, key []byte, plain string) string {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCMWithNonceSize(block, 16)
	iv := []byte("0123456789abcdef")
	sealed := gcm.Seal(nil, iv, []byte(plain), nil)
	ct, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	enc := base64.StdEncoding.EncodeToString
	return enc(iv) + ":" + enc(tag) + ":" + enc(ct)
}

func jwt(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
}

func TestCliLoginFromFileStore(t *testing.T) {
	home := t.TempDir()
	key := []byte("0123456789abcdef0123456789abcdef")
	os.WriteFile(filepath.Join(home, "auth.v2.key"), []byte(base64.StdEncoding.EncodeToString(key)), 0o600)
	token := jwt(map[string]any{"sub": "workos_user", "email": "dev@example.com"})
	creds, _ := json.Marshal(map[string]any{"access_token": token, "refresh_token": "r", "active_organization_id": "org_1"})
	os.WriteFile(filepath.Join(home, "auth.v2.file"), []byte(encrypt(t, key, string(creds))), 0o600)
	os.WriteFile(filepath.Join(home, "host.json"), []byte(`{"computerRegistration":{"userId":"factory_user","firestoreOrgId":"org_f"}}`), 0o600)

	r := &CliLoginReader{FactoryHome: home, ReadSecureKey: func(string) string { return "" }}
	login := r.Read()
	if login == nil || login.AccessToken != token || login.Account.UserID != "factory_user" || deref(login.Account.OrgID) != "org_1" || deref(login.Account.Email) != "dev@example.com" {
		t.Fatalf("%+v", login)
	}
	os.Remove(filepath.Join(home, "auth.v2.file"))
	if r.Read() != nil {
		t.Fatal("logged-out CLI still reads as a login")
	}
}

func TestScratchCreateTrashRestore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chats")
	var trashed []string
	s := &Scratch{Root: func() string { return root }, MoveToTrash: func(p string) error { trashed = append(trashed, p); return os.RemoveAll(p) }}
	p, err := s.Create()
	if err != nil || !scratchName.MatchString(filepath.Base(p)) {
		t.Fatalf("%q %v", p, err)
	}
	if err := s.Trash(p); err != nil || len(trashed) != 0 {
		t.Fatalf("empty folder: %v %v", err, trashed)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("empty folder not removed")
	}
	if err := s.Restore(p); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(p, "f"), []byte("x"), 0o644)
	if err := s.Trash(p); err != nil || len(trashed) != 1 {
		t.Fatalf("folder with files: %v %v", err, trashed)
	}
	for _, bad := range []string{"/etc", filepath.Join(root, "..", "2026-01-01-abcdef"), filepath.Join(root, "notscratch")} {
		if err := s.Trash(bad); !errors.Is(err, ErrNotScratch) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestSystemPromptAddition(t *testing.T) {
	h := New(Config{UserData: t.TempDir(), Home: t.TempDir(), Env: func(string) string { return "" }})
	if h.SystemPrompt() != nil {
		t.Fatal("no addition still sends a system prompt")
	}
	h.Settings.Update(func(s *Settings) { s.AppendSystemPrompt = OptString(" Be brief ") })
	if got := string(h.SystemPrompt()); got != `{"append":"Be brief","preset":"droid","type":"preset"}` {
		t.Fatal(got)
	}
}

func TestSupervisorRestartsAfterCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	// A Daemon that exits at once: never healthy, so it is restarted.
	var mu sync.Mutex
	var states []DaemonStatus
	spawns := 0
	s := &Supervisor{
		Backoff:      []time.Duration{10 * time.Millisecond},
		ReadyTimeout: 300 * time.Millisecond,
		Spawn: func(port int) (*exec.Cmd, error) {
			mu.Lock()
			spawns++
			mu.Unlock()
			cmd := exec.Command("/bin/sh", "-c", "exit 3")
			return cmd, cmd.Start()
		},
		OnState: func(st DaemonState) {
			mu.Lock()
			states = append(states, st.Status)
			mu.Unlock()
		},
	}
	s.Start()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := spawns
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Stop()
	mu.Lock()
	defer mu.Unlock()
	if spawns < 3 {
		t.Fatalf("spawned %d times, states %v", spawns, states)
	}
	if s.State().Status != DaemonStopped {
		t.Fatalf("state after Stop: %v", s.State())
	}
}
