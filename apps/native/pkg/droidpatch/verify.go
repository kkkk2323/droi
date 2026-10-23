package droidpatch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VerifyModel is a model droid sends requests for without reading feature
// flags first, which a local receiver cannot serve.
const VerifyModel = "claude-haiku-4-5-20251001"

const (
	verifyLogin = "fk-droi-verify-login"
	verifyLLM   = "fk-droi-verify-llm"
)

// Verify runs exe, a patched droid, against a local receiver with made-up
// keys and checks that the patch runs: public requests carry the login,
// LLM requests the key file's key, and without a key file an LLM request
// fails at once instead of falling back to the login. No request leaves
// this computer.
func Verify(ctx context.Context, exe string) error {
	var mu sync.Mutex
	type seen struct{ path, cred string }
	var got []seen
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred := "other"
		switch r.Header.Get("Authorization") {
		case "Bearer " + verifyLogin:
			cred = "login"
		case "Bearer " + verifyLLM:
			cred = "llm"
		}
		mu.Lock()
		got = append(got, seen{r.URL.Path, cred})
		mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("{}"))
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	run := func(withKey bool, timeout time.Duration) ([]seen, error) {
		home, err := os.MkdirTemp("", "droi-verify-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(home)
		keyFile := filepath.Join(home, "llm-key")
		if withKey {
			if err := os.WriteFile(keyFile, []byte(verifyLLM), 0o600); err != nil {
				return nil, err
			}
		}
		mu.Lock()
		got = nil
		mu.Unlock()
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(cctx, exe, "exec", "-m", VerifyModel, "--cwd", home, "Reply DONE")
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home, "TMPDIR=" + home,
			"FACTORY_HOME_OVERRIDE=" + home, "FACTORY_API_KEY=" + verifyLogin,
			KeyFileEnv + "=" + keyFile, "FACTORY_DISABLE_KEYRING=1",
			"FACTORY_DROID_AUTO_UPDATE_ENABLED=false", "FACTORY_API_BASE_URL=" + base,
		}
		hideConsole(cmd)
		_ = cmd.Run()
		if cctx.Err() != nil && ctx.Err() == nil {
			return nil, errTimedOut
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]seen(nil), got...), ctx.Err()
	}

	check := func(rows []seen, wantLLM bool) error {
		llm := 0
		for _, s := range rows {
			isLLM := strings.Contains(s.path, "/api/llm/")
			switch {
			case isLLM && s.cred != "llm":
				return fmt.Errorf("droidpatch: an LLM request carried the %s credential", s.cred)
			case !isLLM && s.cred != "login":
				return fmt.Errorf("droidpatch: %s carried the %s credential", s.path, s.cred)
			case isLLM:
				llm++
			}
		}
		if wantLLM && llm == 0 {
			return errors.New("droidpatch: droid sent no LLM request")
		}
		if !wantLLM && llm > 0 {
			return errors.New("droidpatch: droid sent an LLM request without a key file")
		}
		return nil
	}

	rows, err := run(true, 60*time.Second)
	if err != nil {
		return err
	}
	if err := check(rows, true); err != nil {
		return err
	}
	rows, err = run(false, 30*time.Second)
	if errors.Is(err, errTimedOut) {
		return errors.New("droidpatch: without a key file droid kept retrying instead of failing")
	}
	if err != nil {
		return err
	}
	return check(rows, false)
}

var errTimedOut = errors.New("timed out")
