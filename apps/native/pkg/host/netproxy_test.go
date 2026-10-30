package host

import (
	"net/http"
	"testing"

	"github.com/egoist/mygo/netproxy"
)

// Sign-in and usage requests to Factory go through the proxy set in the
// system's settings, as the browser the sign-in opens does.
func TestFactoryRequestsGoThroughTheSystemProxy(t *testing.T) {
	for name, c := range map[string]*http.Client{
		"sign-in": (&FactoryAuth{}).client(),
		"usage":   (&Host{adding: &FactoryAuth{}}).http(),
	} {
		if c.Transport != netproxy.Transport() {
			t.Errorf("%s does not follow the system's proxy", name)
		}
	}
}
