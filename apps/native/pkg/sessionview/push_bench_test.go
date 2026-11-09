package sessionview

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/transcript"
)

// What one conversation.view push of a real window costs the window, from the JSON on the wire to the transcript
// rows, as a whole view and as the ops of one streamed piece of text. DROI_VIEW_PUSH names a file holding one
// whole-view push (apps/native/docs/performance.md says how to make one); without it the benchmarks are skipped.

func BenchmarkAWholeViewPush(b *testing.B) {
	params := viewPushParams(b)
	b.SetBytes(int64(len(params)))
	s := NewState(2)
	var bl transcript.Builder
	b.ReportAllocs()
	for b.Loop() {
		var n kernel.ViewNotification
		if err := json.Unmarshal(params, &n); err != nil {
			b.Fatal(err)
		}
		s.Apply(n)
		bl.BuildView(s.Transcript())
	}
}

func BenchmarkADeltaPush(b *testing.B) {
	params := viewPushParams(b)
	var whole kernel.ViewNotification
	if err := json.Unmarshal(params, &whole); err != nil {
		b.Fatal(err)
	}
	s := NewState(2)
	s.Apply(whole)
	var bl transcript.Builder
	bl.BuildView(s.Transcript())
	// pi.live as a turn streams: one generation whose text grows by a few tokens a push.
	s.Apply(kernel.ViewNotification{ID: 2, Kind: kernel.ViewKindValue, Ops: json.RawMessage(
		`[["s",["docs","pi.live"],{"generation":{"message":{"role":"assistant","content":[{"type":"text","text":""}]}},"tools":[]}]]`)})
	push := []byte(`{"id":2,"kind":"value","value":null,"ops":[["a",["docs","pi.live","generation","message","content",0,"text"],"a few streamed tokens "]]}`)
	b.SetBytes(int64(len(push)))
	b.ReportAllocs()
	for b.Loop() {
		var n kernel.ViewNotification
		if err := json.Unmarshal(push, &n); err != nil {
			b.Fatal(err)
		}
		if !s.Apply(n) {
			b.Fatal("the ops did not apply")
		}
		bl.BuildView(s.Transcript())
	}
}

func viewPushParams(b *testing.B) json.RawMessage {
	path := os.Getenv("DROI_VIEW_PUSH")
	if path == "" {
		b.Skip("DROI_VIEW_PUSH is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	var msg struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		b.Fatal(err)
	}
	return msg.Params
}
