package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// On Kernel a loop is one of the Host's Automations that posts into its conversation (decision Q6).
func TestOnKernelALoopIsAnAutomationOfItsConversation(t *testing.T) {
	fake := newFakeAutomations(t)
	fake.add(gateway.Automation{ID: "elsewhere", Name: "Loop: not mine", Prompt: "Not this conversation's", Schedule: "*/5 * * * *", Timezone: "UTC", Conversation: "999999"})
	h := startHarnessGateway(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}}), "", "", func(o *gateway.Options) { o.Automations = fake }, nil)
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	session := h.d.Sessions[0].SessionID
	conv, ok := h.kernel.id(session)
	if !ok {
		t.Fatalf("no conversation holds %s", session)
	}
	h.openSession("Deploy")
	h.send("/loop 10m check the build")

	var saved gateway.Automation
	h.until("the loop saved", func() bool {
		saved, ok = fake.lastSaved()
		return ok
	})
	if saved.ID != "" || saved.Conversation != conv.String() || saved.Prompt != "check the build" || saved.Name != "Loop: check the build" ||
		saved.Timezone != "UTC" || saved.WorkingDirectory != "/Users/dev/acme-web" || !strings.HasSuffix(saved.Schedule, " * * * *") {
		t.Fatalf("saved %+v", saved)
	}
	id := "loop-check-the-build"
	h.until("the loop run at once", func() bool { return slices.Contains(fake.calls(&fake.ran), id) })
	h.until("the loop above the composer", func() bool { return h.hasText("check the build") && h.hasText("Every 10 minutes") })
	if h.hasText("Not this conversation's") {
		t.Error("another conversation's loop shows")
	}
	if n := h.count(protocol.MethodCreateCron); n != 0 {
		t.Errorf("the Daemon was asked for a cron %d times", n)
	}
	for _, r := range h.kernel.k.Requests() {
		if strings.Contains(r.Prompt(), "/loop") {
			t.Fatalf("the command went to the model: %q", r.Prompt())
		}
	}

	h.click("Pause loop")
	h.until("the loop paused", func() bool { return slices.Contains(fake.calls(&fake.paused), "pause "+id) && h.hasText("· Paused") })
	h.click("Resume loop")
	h.until("the loop resumed", func() bool { return slices.Contains(fake.calls(&fake.paused), "resume "+id) && !h.hasText("· Paused") })

	h.send("/loop stop")
	h.until("the loop deleted", func() bool { return slices.Contains(fake.calls(&fake.deleted), id) && !h.hasText("Every 10 minutes") })
	if slices.Contains(fake.calls(&fake.deleted), "elsewhere") {
		t.Error("/loop stop deleted another conversation's loop")
	}
}

func TestALoopsDailyTimeIsReadInItsZone(t *testing.T) {
	if _, err := time.LoadLocation("Asia/Shanghai"); err != nil {
		t.Skip("no time zone data")
	}
	for _, tc := range []struct{ expr, zone, want string }{
		{"7 10 * * *", "Asia/Shanghai", "7 2 * * *"},
		{"7 10 * * *", "UTC", "7 10 * * *"},
		{"7 10 * * *", "", "7 10 * * *"},
		{"*/10 * * * *", "Asia/Shanghai", "*/10 * * * *"},
	} {
		if got := utcExpression(tc.expr, tc.zone); got != tc.want {
			t.Errorf("%q in %q: %q, want %q", tc.expr, tc.zone, got, tc.want)
		}
	}
}
