package drafts

import (
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/attachments"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

func TestDraftTextIsStoredAndImagesStayInMemory(t *testing.T) {
	store := prefs.Memory()
	d := New(store)
	pic := []attachments.Image{{ID: "1", Name: "a.png", MediaType: "image/png", Data: "AAAA"}}

	d.Save("s1", Draft{Text: "half a thought", Images: pic})
	if v, _ := store.Get("droi.draft.s1"); v != "half a thought" {
		t.Errorf("stored %q", v)
	}
	if got := d.Load("s1"); got.Text != "half a thought" || !reflect.DeepEqual(got.Images, pic) {
		t.Errorf("Load = %+v", got)
	}

	// A new run of the app reads the text, not the images.
	if got := New(store).Load("s1"); got.Text != "half a thought" || len(got.Images) != 0 {
		t.Errorf("reloaded = %+v", got)
	}

	d.Save("s1", Draft{})
	if _, ok := store.Get("droi.draft.s1"); ok {
		t.Error("an empty draft should leave nothing stored")
	}
	if got := d.Load("s1"); got.Text != "" || len(got.Images) != 0 {
		t.Errorf("cleared = %+v", got)
	}
}

func TestPendingPromptIsTakenOnce(t *testing.T) {
	var p Pending
	if _, ok := p.Take("s"); ok {
		t.Error("nothing queued yet")
	}
	p.Set("s", PendingPrompt{Text: "hello"})
	if got, ok := p.Take("s"); !ok || got.Text != "hello" {
		t.Errorf("Take = %+v, %v", got, ok)
	}
	if _, ok := p.Take("s"); ok {
		t.Error("the message should be handed out once")
	}
}
