package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

// A Session's speed from an earlier run shows under its composer.
func TestSpeedMeterShowsSavedStats(t *testing.T) {
	file := filepath.Join(t.TempDir(), "session-stats.json")
	saved := `{"s-speed":{"calls":4,"modelMs":192000,"toolMs":45000,"ttftMs":5600,"ttftCalls":4,"decodeMs":10000,"decodeTokens":720,` +
		`"last":{"ttftMs":1200,"decodeMs":4000,"outputTokens":340},"updatedAt":1}}`
	if err := os.WriteFile(file, []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Fast", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}, Extra: map[string]any{"sessionId": "s-speed"}}},
	}, "", func(c *Config) { c.StatsFile = file })
	h.openSession("Fast")
	h.until("the last call's speed under the composer", func() bool { return h.hasText("85 tok/s") })
	if h.hasText("Time in tools") {
		t.Fatal("the details stay closed until clicked")
	}
	h.click("Model speed")
	for _, want := range []string{"1.2s", "340", "4", "1.4s", "72 tok/s", "3m 12s", "45s"} {
		if !h.hasText(want) {
			t.Fatalf("details lack %q; texts %q", want, h.tt.Texts())
		}
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		h.settle()
		_ = os.MkdirAll(dir, 0o755)
		writePNG(filepath.Join(dir, "speed-details.png"), h.tt.Image())
	}
}

func TestSessionStatsSurviveARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "session-stats.json")
	st := openSessionStats(file)
	st.put("a", session.Stats{Calls: 2, ModelMs: 3000})
	st.save()

	got, ok := openSessionStats(file).get("a")
	if !ok || got.Calls != 2 || got.ModelMs != 3000 {
		t.Fatalf("reloaded %+v (%t), want 2 calls and 3000 ms", got, ok)
	}
	if _, ok := openSessionStats(file).get("b"); ok {
		t.Fatal("an unknown Session has no saved stats")
	}
}

func TestFormatSpeedFigures(t *testing.T) {
	for in, want := range map[float64]string{0: "–", 850: "0.8s", 1240: "1.2s", 12_400: "12s"} {
		if got := formatSeconds(in); got != want {
			t.Errorf("formatSeconds(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{45_000: "45s", 192_000: "3m 12s", 3_900_000: "1h 5m"} {
		if got := formatSpan(in); got != want {
			t.Errorf("formatSpan(%v) = %q, want %q", in, got, want)
		}
	}
}
