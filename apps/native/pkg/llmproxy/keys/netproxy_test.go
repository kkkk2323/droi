package keys

import (
	"testing"

	"github.com/egoist/mygo/netproxy"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

// The Key Pool reads its keys' usage through the proxy set in the system's
// settings.
func TestUsageGoesThroughTheSystemProxy(t *testing.T) {
	if NewKeyRotator(&config.Config{}, nil).client.Transport != netproxy.Transport() {
		t.Fatal("usage does not follow the system's proxy")
	}
}
