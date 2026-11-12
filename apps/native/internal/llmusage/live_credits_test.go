package llmusage_test

import (
	"cmp"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
)

// The request log prices each call from what its response reported, at
// droid's prices. This checks that against Factory's own billing counter:
// one droid exec per model through the Key Pool's proxy, with a key nothing
// else uses while it runs. It spends real credits, so it runs only with
// FACTORY_API_KEY set; DROI_LIVE_CREDIT_MODELS picks the models and
// DROID_PATH the droid (default: the newest of Droi's).
func TestLiveRequestLogCreditsMatchFactoryBilling(t *testing.T) {
	key := os.Getenv("FACTORY_API_KEY")
	if key == "" {
		t.Skip("FACTORY_API_KEY is not set")
	}
	droid := liveDroid(t)
	models := strings.Split(cmp.Or(os.Getenv("DROI_LIVE_CREDIT_MODELS"), "gemini-4-carbon,gpt-6.1-sol,claude-haiku-5-5"), ",")
	ms, err := droidhook.ExportModels(context.Background(), droid, filepath.Join(t.TempDir(), "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	llmusage.UseDroidModels(ms)

	dir := t.TempDir()
	pool := keypool.Open(keypool.Options{Dir: dir, Upstream: "https://api.factory.ai"})
	if err := pool.Add(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	base, err := pool.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Stop)
	log := llmusage.NewRequestLog(pool.RequestLog())

	src, _ := filepath.Abs(".")
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			work := t.TempDir()
			for _, f := range []string{"observe.go", "quota.go", "spend.go", "runway.go", "cycle.go", "format.go"} {
				b, err := os.ReadFile(filepath.Join(src, f))
				if err != nil {
					t.Fatal(err)
				}
				_ = os.WriteFile(filepath.Join(work, f), b, 0o600)
			}
			before := stableCounter(t, key)
			start := time.Now()
			cmd := exec.Command(droid, "exec", "-m", model, "-r", "medium", "-o", "json", "--cwd", work,
				"Use the Read tool to read each .go file in the current directory, one file per tool call, never in parallel. Then describe what they do in 2 sentences.")
			cmd.Env = liveEnv(t.TempDir(), key, base)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("droid exec: %v\n%s", err, tail(out))
			}
			var res struct {
				SessionID string `json:"session_id"`
				Usage     struct {
					FactoryCredits float64 `json:"factory_credits"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(lastJSON(out), &res); err != nil || res.SessionID == "" {
				t.Fatalf("no result: %v\n%s", err, tail(out))
			}
			billed := float64(stableCounter(t, key) - before)
			if err := log.Refresh(); err != nil {
				t.Fatal(err)
			}
			logged := log.SessionCredits(map[string]bool{res.SessionID: true}, start, time.Time{})
			// The key served nothing else meanwhile: every call the log holds
			// since start is this run's, the Session's or droid's own.
			all := log.Observe(llmusage.ObserveQuery{Period: llmusage.PeriodAll}, time.Now())
			var total float64
			for _, r := range all.Listed {
				if r.Time.Before(start) || r.Usage == nil {
					continue
				}
				c, ok := llmusage.Credits(r.Model, *r.Usage, r.Time)
				total += c
				t.Logf("  %s %s session=%q kind=%q usage=%+v credits=%.0f priced=%v", r.Time.Format("15:04:05"), r.Model, r.SessionID, r.Kind, *r.Usage, c, ok)
			}
			t.Logf("%s: Factory billed %.0f; the request log prices the Session %.0f (%d calls), every call %.0f; droid reported %.0f",
				model, billed, logged.Credits, logged.Calls, total, res.Usage.FactoryCredits)
			if logged.Calls == 0 || math.Abs(total-billed) > 0.01*billed+5 {
				t.Errorf("the request log prices %.0f, Factory billed %.0f", total, billed)
			}
		})
	}
}

func liveDroid(t *testing.T) string {
	if p := os.Getenv("DROID_PATH"); p != "" {
		return p
	}
	dir, _ := os.UserConfigDir()
	found, _ := filepath.Glob(filepath.Join(dir, "Droi", "droid", "*", "droid"))
	if len(found) == 0 {
		t.Skip("no droid on this computer; DROID_PATH picks one")
	}
	return found[len(found)-1]
}

// liveEnv runs droid in a throwaway HOME, through the proxy, with the key:
// without Droi's key hook, which would swap in a key of Droi's own pool.
func liveEnv(home, key, base string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "HOME" || strings.HasPrefix(name, "FACTORY_") || strings.HasPrefix(name, "DROI_") || strings.HasPrefix(name, "BUN_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "HOME="+home, "FACTORY_API_KEY="+key, "FACTORY_API_BASE_URL="+base)
}

// stableCounter is the key's billed Standard tokens once two reads 15
// seconds apart agree: Factory adds a call some seconds after it ends.
func stableCounter(t *testing.T, key string) int64 {
	t.Helper()
	read := func() int64 {
		req, _ := http.NewRequest("GET", "https://app.factory.ai/api/organization/members/chat-usage", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var d struct {
			Usage struct {
				Standard struct {
					UserTokens int64 `json:"userTokens"`
				} `json:"standard"`
			} `json:"usage"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
			t.Fatal(err)
		}
		return d.Usage.Standard.UserTokens
	}
	last := read()
	for range 12 {
		time.Sleep(15 * time.Second)
		cur := read()
		if cur == last {
			return cur
		}
		last = cur
	}
	t.Fatal("the billing counter did not settle")
	return 0
}

func lastJSON(out []byte) []byte {
	s := strings.TrimSpace(string(out))
	if i := strings.LastIndex(s, "\n{"); i >= 0 {
		s = s[i+1:]
	}
	return []byte(s)
}

func tail(b []byte) string {
	if len(b) > 2000 {
		b = b[len(b)-2000:]
	}
	return string(b)
}
