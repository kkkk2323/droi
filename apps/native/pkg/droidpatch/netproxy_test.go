package droidpatch

import (
	"testing"

	"github.com/egoist/mygo/netproxy"
)

// The droid builds and the recipes download through the proxy set in the
// system's settings, as a browser's downloads do, not only HTTP_PROXY's.
func TestTheStoreDownloadsThroughTheSystemProxy(t *testing.T) {
	if NewStore(t.TempDir()).HTTP.Transport != netproxy.Transport() {
		t.Fatal("the store does not follow the system's proxy")
	}
}
