package compaction

import (
	"reflect"
	"testing"
)

func TestPendingFromStartToFinish(t *testing.T) {
	log := NewLog()
	var heard []Finished
	log.OnDone(func(f Finished) { heard = append(heard, f) })

	log.Start("parent")
	if !log.Snapshot().Pending["parent"] {
		t.Fatal("parent should be pending")
	}

	log.Finish("parent", "child", 12, 1000)
	after := log.Snapshot()
	if after.Pending["parent"] {
		t.Error("parent should no longer be pending")
	}
	if got := after.Finished["child"]; got != (Done{RemovedCount: 12, FinishedAt: 1000}) {
		t.Errorf("child = %+v", got)
	}
	if _, ok := after.Finished["parent"]; ok {
		t.Error("done is remembered where the user landed, not on the parent")
	}
	want := []Finished{{SessionID: "parent", ShownIn: "child", Done: Done{RemovedCount: 12, FinishedAt: 1000}}}
	if !reflect.DeepEqual(heard, want) {
		t.Errorf("heard %+v", heard)
	}
}

func TestFailClearsPendingWithoutDone(t *testing.T) {
	log := NewLog()
	called := false
	log.OnDone(func(Finished) { called = true })
	log.Start("s")
	log.Fail("s")
	if len(log.Snapshot().Pending) != 0 || called {
		t.Error("a failure should clear pending and fire no done event")
	}
}

func TestChangesNeverAlterAnEarlierSnapshot(t *testing.T) {
	log := NewLog()
	changes := 0
	off := log.Subscribe(func() { changes++ })
	before := log.Snapshot()
	log.Start("s")
	if len(before.Pending) != 0 || !log.Snapshot().Pending["s"] || changes != 1 {
		t.Errorf("before = %+v, changes = %d", before, changes)
	}
	off()
	log.Fail("s")
	if changes != 1 {
		t.Errorf("changes = %d after unsubscribing", changes)
	}
}

func TestNoticeShown(t *testing.T) {
	d := Done{FinishedAt: 1000}
	if !d.NoticeShown(1000+NoticeMs-1) || d.NoticeShown(1000+NoticeMs) {
		t.Error("the notice should last NoticeMs")
	}
}
