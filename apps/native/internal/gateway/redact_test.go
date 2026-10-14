package gateway

import "testing"

func TestRedactTokenHidesTheTokenInADialError(t *testing.T) {
	in := `droid: dial: failed to send handshake request: Get "http://win.myhome:41417/daemon?token=nfMjzcu8YW6L-x_9": context deadline exceeded`
	want := `droid: dial: failed to send handshake request: Get "http://win.myhome:41417/daemon?token=…": context deadline exceeded`
	if got := RedactToken(in); got != want {
		t.Fatalf("got %q", got)
	}
	if got := RedactToken("a=1&token=abc&b=2"); got != "a=1&token=…&b=2" {
		t.Fatalf("got %q", got)
	}
	if got := RedactToken("no token here"); got != "no token here" {
		t.Fatalf("got %q", got)
	}
}
