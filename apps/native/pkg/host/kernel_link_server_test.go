package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// linkKernel is a socket that accepts a Client and holds it open until the test
// ends: enough of a Kernel for what the link does, which is dial and watch.
type linkKernel struct {
	url string
}

func startLinkKernel(t *testing.T) *linkKernel {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return &linkKernel{url: "ws" + strings.TrimPrefix(srv.URL, "http") + "/rpc"}
}

func (k *linkKernel) dial(ctx context.Context) (*kernel.Client, error) {
	return kernel.Dial(ctx, kernel.Options{URL: k.url, Token: "t"})
}
