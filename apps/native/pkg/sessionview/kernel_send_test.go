package sessionview

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// The composer on the Kernel path, against a Kernel: what a send, a queued message, Stop and taking a message back
// do is Kernel's and Pi's, so these tests drive a View through a real one and read what it did with each call.

// kernelSendPage is a View of a fresh conversation on a Fake Kernel, with an Observer as its Kernel so a test can
// read the calls the page made.
type kernelSendPage struct {
	k  *fakekernel.Kernel
	c  *kernel.Client
	o  *fakekernel.Observer
	st *Store
	id kernel.ID
	v  *View
}

func newKernelSendPage(t *testing.T, sc fakekernel.Scenario) *kernelSendPage {
	t.Helper()
	k := fakekernel.Start(t, sc)
	c := k.Dial(t)
	p := &kernelSendPage{k: k, c: c, o: k.Observe(c), st: NewStore(3)}
	p.id = k.CreateConversation(t, c)
	pumpNotifications(p.st, c)
	p.v = New(strconvID(p.id), Env{Kernel: p.o, Sessions: p.st, Revisions: &Revisions{}})
	p.v.Open()
	await(t, "the page to open", func() bool { s := p.v.Frame(); return s != nil && s.Loaded })
	return p
}

func strconvID(id kernel.ID) string { b, _ := json.Marshal(id); return string(b) }

// queuedTexts are the shelf's messages as text, with their kinds.
func (p *kernelSendPage) queued() []session.QueuedMessage {
	if s := p.v.Frame(); s != nil {
		return s.QueuedMessages()
	}
	return nil
}

func queuedTextOf(q session.QueuedMessage) string {
	var parts []string
	for _, b := range q.Content {
		var t struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(b.Raw, &t) == nil && t.Text != "" {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (p *kernelSendPage) rowTexts() []string {
	p.v.Frame()
	return texts(p.v)
}

func slowAnswer(match string) fakekernel.Step {
	return fakekernel.Step{Match: match, Chunks: slices.Repeat([]string{"working "}, 40), Delay: 100 * time.Millisecond}
}

// TestAKernelPageSendsAMessage: a message typed on the page is conversation.submit with the text as its input, and
// the turn it starts is drawn.
func TestAKernelPageSendsAMessage(t *testing.T) {
	p := newKernelSendPage(t, fakekernel.Scenario{Steps: []fakekernel.Step{{Match: "hello", Text: "hi there"}}})
	p.v.Send("  hello  ", nil, "")
	await(t, "the question and its answer", func() bool {
		rows := p.rowTexts()
		return slices.Contains(rows, "hello") && slices.Contains(rows, "hi there")
	})
	var sent kernel.SubmitParams
	if err := json.Unmarshal(p.o.CallsOf(kernel.MethodConversationSubmit)[0].Params, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Input != "hello" || sent.WhenBusy != "" || sent.RequestID == "" {
		t.Errorf("the page submitted %+v, want the trimmed text, Kernel's default when busy, and a request id", sent)
	}
}

// TestAKernelPageQueuesWhileATurnRuns: Enter while a turn runs queues the message for after the run, ⌘↩ hands it to
// the running turn; the shelf shows the first as queued and the second as next, from pi.inbox.
func TestAKernelPageQueuesWhileATurnRuns(t *testing.T) {
	p := newKernelSendPage(t, fakekernel.Scenario{AllowUnmatched: true, Fallback: "ok", Steps: []fakekernel.Step{slowAnswer("first")}})
	p.v.Send("first", nil, "")
	await(t, "the run", func() bool { s := p.v.Frame(); return s != nil && s.Running })

	p.v.Send("after the run", nil, protocol.QueuePlacementEndOfLoop)
	await(t, "the follow-up on the shelf", func() bool {
		q := p.queued()
		return len(q) == 1 && queuedTextOf(q[0]) == "after the run" && q[0].Kind == session.KindDaemonQueuedEndOfLoop
	})
	p.v.Send("join in", nil, protocol.QueuePlacementEndOfTurn)
	await(t, "the third submission", func() bool { return p.o.Count(kernel.MethodConversationSubmit) == 3 })
	var modes []string
	for _, rpc := range p.o.CallsOf(kernel.MethodConversationSubmit) {
		var sp kernel.SubmitParams
		_ = json.Unmarshal(rpc.Params, &sp)
		modes = append(modes, sp.WhenBusy)
	}
	if !slices.Equal(modes, []string{"", kernel.WhenBusyFollowUp, kernel.WhenBusySteer}) {
		t.Errorf("the page submitted with whenBusy %q, want none, followUp, steer", modes)
	}
}

// TestStopKeepsWhatTheUserQueued: Kernel withdraws the queued inputs when a run is stopped. The page keeps them as
// paused messages, as the Daemon's path did, so Stop does not throw away what the user wrote; removing one then
// drops it on this computer.
func TestStopKeepsWhatTheUserQueued(t *testing.T) {
	p := newKernelSendPage(t, fakekernel.Scenario{AllowUnmatched: true, Fallback: "ok", Steps: []fakekernel.Step{slowAnswer("first")}})
	p.v.Send("first", nil, "")
	await(t, "the run", func() bool { s := p.v.Frame(); return s != nil && s.Running })
	p.v.Send("keep me", nil, protocol.QueuePlacementEndOfLoop)
	await(t, "the queued message", func() bool { return len(p.queued()) == 1 })

	p.v.Interrupt()
	var q []session.QueuedMessage
	await(t, "the run to stop with the message paused", func() bool {
		s := p.v.Frame()
		q = p.queued()
		return s != nil && !s.Running && len(q) == 1 && q[0].Kind == session.KindLocalPausedAfterEsc
	})
	if queuedTextOf(q[0]) != "keep me" {
		t.Errorf("the paused message reads %q", queuedTextOf(q[0]))
	}
	if items := p.st.State(p.id).InboxItems(); len(items) != 0 {
		t.Errorf("Kernel still holds %+v after the stop", items)
	}

	if err := p.v.Withdraw(q[0]); err != nil {
		t.Fatal(err)
	}
	if left := p.queued(); len(left) != 0 {
		t.Errorf("removing the paused message left %+v", left)
	}
	if n := p.o.Count(kernel.MethodSubmissionAbort); n != 0 {
		t.Errorf("removing a message kept on this computer asked Kernel %d times", n)
	}
}

// TestWithdrawingAQueuedMessageKeepsItFromRunning: taking a queued message back aborts its submission on Kernel, and
// the run that follows never sees it.
func TestWithdrawingAQueuedMessageKeepsItFromRunning(t *testing.T) {
	p := newKernelSendPage(t, fakekernel.Scenario{AllowUnmatched: true, Fallback: "ok", Steps: []fakekernel.Step{slowAnswer("first")}})
	p.v.Send("first", nil, "")
	await(t, "the run", func() bool { s := p.v.Frame(); return s != nil && s.Running })
	p.v.Send("never mind", nil, protocol.QueuePlacementEndOfLoop)
	var q []session.QueuedMessage
	await(t, "the queued message", func() bool { q = p.queued(); return len(q) == 1 })

	if err := p.v.Withdraw(q[0]); err != nil {
		t.Fatalf("withdraw: %v\n%s", err, p.k.Logs())
	}
	await(t, "the run to end", func() bool { s := p.v.Frame(); return s != nil && !s.Running })
	if slices.Contains(p.rowTexts(), "never mind") {
		t.Error("the withdrawn message went out")
	}
	for _, r := range p.k.Requests() {
		if strings.Contains(r.FullPrompt(), "never mind") {
			t.Error("the model was asked with the withdrawn message")
		}
	}
}

// TestStopKeepsOnlyWhatKernelWithdrew: between the moment Stop reads the queue and the abort landing, a queued input
// can enter the transcript. That one went out; kept as a paused message, editing it would send it twice. Only the
// inputs Kernel reports withdrawn — unanswered, with no entry — come back.
func TestStopKeepsOnlyWhatKernelWithdrew(t *testing.T) {
	view := kernel.View{Conversation: kernel.Conversation{ID: 7}, Entries: []kernel.Entry{numbered(t, 1)}, Docs: map[string]json.RawMessage{
		"pi.live":  json.RawMessage(`{"run":{"taskId":3,"inputs":[1],"turns":1}}`),
		"pi.inbox": json.RawMessage(`{"items":[{"id":5,"mode":"followUp","content":"withdrawn"},{"id":6,"mode":"steer","content":"placed first"},{"id":8,"mode":"write","entry":{}}]}`),
	}}
	entry := kernel.ID(9)
	f := &fakeKernel{views: map[kernel.ID]kernel.View{7: view},
		abort: func(kernel.AbortParams) error { return nil },
		submissions: map[kernel.ID]kernel.SubmissionGet{
			5: {SubmissionID: 5, Status: "unanswered", Reason: "aborted"},
			6: {SubmissionID: 6, Status: "unanswered", Entry: &entry, Reason: "aborted"},
		}}
	st := NewStore(3)
	v := New("7", Env{Kernel: f, Sessions: st, Revisions: &Revisions{}})
	v.Open()
	await(t, "the page to open", func() bool { s := v.Frame(); return s != nil && s.Loaded && len(s.QueuedMessages()) == 2 })

	v.Interrupt()
	await(t, "the paused message", func() bool {
		for _, q := range v.Frame().QueuedMessages() {
			if q.Kind == session.KindLocalPausedAfterEsc {
				return true
			}
		}
		return false
	})
	var paused []string
	for _, q := range v.Frame().QueuedMessages() {
		if q.Kind == session.KindLocalPausedAfterEsc {
			paused = append(paused, queuedTextOf(q))
		}
	}
	if !slices.Equal(paused, []string{"withdrawn"}) {
		t.Errorf("Stop kept %q, want only the input Kernel withdrew", paused)
	}
}
