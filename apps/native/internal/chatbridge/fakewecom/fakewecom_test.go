package fakewecom_test

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kkkk2323/droi/apps/native/internal/chatbridge/fakewecom"
)

// Like WeCom's server, the Fake WeCom refuses Go's canonical
// Sec-Websocket-Key, so a dialer that sends it fails the tests.
func TestTheFakeWeComRefusesGosCanonicalHeaderNames(t *testing.T) {
	w := fakewecom.Start(t, fakewecom.Robot{BotID: "aib", Secret: "s"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, res, err := websocket.Dial(ctx, w.URL, nil)
	if err == nil {
		c.CloseNow()
		t.Fatal("the canonical spelling was accepted")
	}
	if res == nil || res.StatusCode != 404 || w.Refused() != 1 {
		t.Errorf("response %v, %d refused", res, w.Refused())
	}
}
