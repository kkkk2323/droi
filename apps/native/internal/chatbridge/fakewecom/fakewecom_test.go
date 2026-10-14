package fakewecom_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
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

func TestTheFakeWeComChecksTheGetTokenSignature(t *testing.T) {
	r := fakewecom.Robot{BotID: "aib", Secret: "s3cret"}
	w := fakewecom.Start(t, r)
	now := time.Unix(1789545600, 0)
	w.SetClock(func() time.Time { return now })
	getToken := func(secret string, at int64) (errcode int, token string) {
		sum := sha256.Sum256([]byte(secret + r.BotID + strconv.FormatInt(at, 10) + "n0nce"))
		body, _ := json.Marshal(map[string]any{"bot_id": r.BotID, "time": at, "nonce": "n0nce", "signature": hex.EncodeToString(sum[:])})
		res, err := http.Post(w.APIURL+"/get_token", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct {
			Errcode     int    `json:"errcode"`
			AccessToken string `json:"access_token"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		return out.Errcode, out.AccessToken
	}
	if code, _ := getToken("wrong", now.Unix()); code != 853000 {
		t.Errorf("a wrong Secret got %d", code)
	}
	if code, _ := getToken(r.Secret, now.Unix()-120); code != 853000 {
		t.Errorf("a time 2 minutes off got %d", code)
	}
	if code, token := getToken(r.Secret, now.Unix()); code != 0 || token == "" {
		t.Errorf("the right signature got %d %q", code, token)
	}
}
