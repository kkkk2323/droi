package sessionfile

import "testing"

func TestDetails(t *testing.T) {
	got := Details("a1b2c3", "Fix the login bug", "/Users/dev/acme-web", "/Users/dev/.factory/sessions/-Users-dev-acme-web/a1b2c3.jsonl")
	want := "Title: Fix the login bug\nSession ID: a1b2c3\nWorkspace: /Users/dev/acme-web\nTranscript: /Users/dev/.factory/sessions/-Users-dev-acme-web/a1b2c3.jsonl"
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestDetailsLeavesOutWhatIsUnknown(t *testing.T) {
	if got, want := Details("a1b2c3", "Fix the login bug", "", ""), "Title: Fix the login bug\nSession ID: a1b2c3"; got != want {
		t.Errorf("got %q", got)
	}
}
