package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestTheWindow opens the app's window, as no view test can: it builds the
// app with the automation plugin, starts it on a throwaway home and profile
// without droid, and drives it through the socket as a user would, through
// the menu bar, a key and Quit. It needs a desktop session:
// DROI_E2E=1 go test ./apps/native -run TestTheWindow -v
func TestTheWindow(t *testing.T) {
	if os.Getenv("DROI_E2E") == "" {
		t.Skip("DROI_E2E=1 opens the window (a desktop session)")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("the menu paths are macOS's")
	}
	// Short: a Unix socket's path has room for about 100 bytes.
	dir, err := os.MkdirTemp("/tmp", "droi-window-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	bin := filepath.Join(dir, "droi")
	if out, err := exec.Command("go", "build", "-tags", "e2e", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home, data := filepath.Join(dir, "home"), filepath.Join(dir, "data")
	for _, d := range []string{home, data} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(data, "preferences.json"), []byte(`{"droi.language":"en"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "app.sock")
	app := exec.Command(bin)
	// TMPDIR keeps its single-instance lock apart from an installed Droi's.
	app.Env = []string{"HOME=" + home, "TMPDIR=" + dir, "PATH=/usr/bin:/bin", "DROI_USER_DATA_DIR=" + data,
		"DROI_NO_UPDATE_CHECK=1", "MYGO_AUTOMATION=" + sock}
	logFile, _ := os.Create(filepath.Join(dir, "app.log"))
	app.Stdout, app.Stderr = logFile, logFile
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- app.Wait() }()
	t.Cleanup(func() {
		_ = app.Process.Kill()
		if t.Failed() {
			log, _ := os.ReadFile(filepath.Join(dir, "app.log"))
			t.Logf("the app's log:\n%s", log)
		}
	})

	var conn net.Conn
	until(t, "the automation socket", func() bool {
		conn, err = net.Dial("unix", sock)
		return err == nil
	})
	defer conn.Close()
	r := bufio.NewReader(conn)
	call := func(method string, params any) (json.RawMessage, string) {
		t.Helper()
		line, _ := json.Marshal(map[string]any{"id": 1, "method": method, "params": params})
		if _, err := conn.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
		answer, err := r.ReadBytes('\n')
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		var res struct {
			Result json.RawMessage
			Error  *struct{ Code, Message string }
		}
		_ = json.Unmarshal(answer, &res)
		if res.Error != nil {
			return nil, res.Error.Code + ": " + res.Error.Message
		}
		return res.Result, ""
	}
	must := func(method string, params any) json.RawMessage {
		t.Helper()
		res, failed := call(method, params)
		if failed != "" {
			t.Fatalf("%s: %s", method, failed)
		}
		return res
	}
	labels := func() []string {
		var tree struct{ Nodes []struct{ Label string } }
		res, failed := call("tree", nil)
		if failed != "" {
			return nil
		}
		_ = json.Unmarshal(res, &tree)
		var out []string
		for _, n := range tree.Nodes {
			out = append(out, n.Label)
		}
		return out
	}
	has := func(label string) bool { return slices.Contains(labels(), label) }

	var windows struct {
		Windows []struct {
			Title, Kind string
			Visible     bool
		}
	}
	until(t, "the window", func() bool {
		res, failed := call("windows", nil)
		_ = json.Unmarshal(res, &windows)
		return failed == "" && len(windows.Windows) == 1 && windows.Windows[0].Visible
	})
	if w := windows.Windows[0]; w.Title != "Droi" || w.Kind != "content" {
		t.Fatalf("the window is %+v", w)
	}
	until(t, "the sidebar", func() bool { return has("Settings") })

	must("menu", map[string]any{"path": []string{"Droi", "Settings…"}})
	until(t, "Settings, from the menu bar", func() bool { return has("Session defaults") })

	// The socket's keys reach the window's content, not the menu bar's
	// shortcuts: Escape is the window's own.
	must("input", map[string]any{"events": []map[string]any{{"type": "key", "key": "Escape"}}})
	until(t, "Settings closed with Escape", func() bool { return !has("Session defaults") })

	var shot struct {
		PNG           string
		Width, Height int
	}
	_ = json.Unmarshal(must("capture", nil), &shot)
	b, err := base64.StdEncoding.DecodeString(shot.PNG)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(strings.NewReader(string(b)))
	if err != nil || img.Bounds().Dx() != shot.Width || shot.Width < 720 {
		t.Fatalf("the capture is %dx%d: %v", shot.Width, shot.Height, err)
	}
	if out := os.Getenv("DROI_E2E_SHOT"); out != "" {
		_ = os.WriteFile(out, b, 0o644)
	}

	// Quit answers once its item has run, as the app ends: the answer may
	// not come.
	_, _ = call("menu", map[string]any{"path": []string{"Droi", "Quit Droi"}})
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("the app quit with %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the app did not quit")
	}
}

// until waits up to a minute for done, which only guards against a hang.
func until(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
