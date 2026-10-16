package prefs

import (
	"slices"
	"testing"
)

func TestAListReadsWhatWasStoredLast(t *testing.T) {
	s := Memory()
	p := List{Key: "list"}
	if p.Get(s) != nil || p.Has(s, "a") {
		t.Fatal("an unset list has entries")
	}
	p.Set(s, []string{"a", "b"})
	got := p.Get(s)
	if !slices.Equal(got, []string{"a", "b"}) || !p.Has(s, "b") {
		t.Fatalf("got %q", got)
	}
	got[0] = "changed"
	if p.Get(s)[0] != "a" {
		t.Fatal("changing what Get returned changed the list")
	}
	p.Toggle(s, "a")
	if !slices.Equal(p.Get(s), []string{"b"}) || p.Has(s, "a") {
		t.Fatalf("after Toggle %q", p.Get(s))
	}
	s.Set("list", "not json")
	if p.Get(s) != nil || p.Has(s, "b") {
		t.Fatalf("an unreadable list reads %q", p.Get(s))
	}
	s.Remove("list")
	if p.Get(s) != nil {
		t.Fatal("a removed list has entries")
	}
}
