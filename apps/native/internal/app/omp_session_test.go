package app

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseSelectOptionsAcceptsStringArray(t *testing.T) {
	got := parseSelectOptions(json.RawMessage(`["Approve","Deny"]`))
	want := []selectOption{
		{Value: "Approve", Label: "Approve"},
		{Value: "Deny", Label: "Deny"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestParseSelectOptionsAcceptsValueLabel(t *testing.T) {
	got := parseSelectOptions(json.RawMessage(`[{"value":"allow","label":"Allow for session"}]`))
	want := []selectOption{{Value: "allow", Label: "Allow for session"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}
