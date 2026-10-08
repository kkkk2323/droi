package protocol

import (
	"testing"
	"time"
)

// The SDK passes these overrides across several lines; a generator that
// misses them leaves the method on the Client's 30s default.
func TestMethodTimeoutOverrides(t *testing.T) {
	want := map[string]time.Duration{
		MethodAuthenticateMCPServer: 300 * time.Second,
		MethodCompactSession:        240 * time.Second,
		MethodCleanupWorktree:       180 * time.Second,
		MethodGenerateSemanticDiff:  180 * time.Second,
		MethodPushCwdFileToURL:      900 * time.Second,
		MethodPullURLToCwdFile:      900 * time.Second,
	}
	for method, d := range want {
		if got := Methods[method].Timeout; got != d {
			t.Errorf("%s: timeout %v, want %v", method, got, d)
		}
	}
}
