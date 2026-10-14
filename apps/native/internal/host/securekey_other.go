//go:build !windows

package host

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

func securityKey(account string) string {
	switch runtime.GOOS {
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", keychainService, "-a", account, "-w").Output()
		if err != nil {
			return ""
		}
		return string(out)
	case "linux":
		return secretToolKey(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		}, account)
	}
	return ""
}

// secretToolKey reads a key droid keeps in the Secret Service (GNOME
// Keyring, KWallet) with libsecret's secret-tool. keytar files a password
// under the attributes service and account. A computer without secret-tool,
// a keyring or the key, or a keyring prompt nobody answers, gives "" so the
// next credential source is tried without a word.
func secretToolKey(run func(ctx context.Context, name string, args ...string) ([]byte, error), account string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := run(ctx, "secret-tool", "lookup", "service", keychainService, "account", account)
	if err != nil {
		return ""
	}
	return string(out)
}
