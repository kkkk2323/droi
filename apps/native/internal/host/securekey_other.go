//go:build !windows

package host

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

func securityKey(account string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", keychainService, "-a", account, "-w").Output()
	if err != nil {
		return ""
	}
	return string(out)
}
