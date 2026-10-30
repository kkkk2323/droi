package proxy

import (
	"net/http"
	"testing"

	"github.com/egoist/mygo/netproxy"
)

// The requests the proxy sends on to Factory, its own key openings and
// cache warming go through the proxy set in the system's settings.
func TestUpstreamRequestsGoThroughTheSystemProxy(t *testing.T) {
	if (&Proxy{}).outboundTransport() != http.RoundTripper(netproxy.Transport()) {
		t.Error("upstream requests do not follow the system's proxy")
	}
	if NewCacheWarmer(CacheWarmingConfig{}, nil, nil, nil, nil).client.Transport != netproxy.Transport() {
		t.Error("cache warming does not follow the system's proxy")
	}
}
