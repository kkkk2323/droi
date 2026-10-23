package droidpatch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Build writes the Patched Droid for the official build at src to dst,
// signs it where the system asks for a signature, and verifies that the
// patch runs. dst only appears once all of that passed; src is not changed.
func Build(ctx context.Context, src, dst string, r Recipe) (Report, error) {
	blob, err := os.ReadFile(src)
	if err != nil {
		return Report{}, err
	}
	out, rep, err := Apply(blob, r)
	if err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return Report{}, err
	}
	tmp := dst + ".new"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, out, 0o700); err != nil {
		return Report{}, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := signFile(ctx, tmp); err != nil {
		return Report{}, err
	}
	if err := verifyFile(ctx, tmp); err != nil {
		return Report{}, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return Report{}, err
	}
	ok = true
	if b, err := json.MarshalIndent(rep, "", "  "); err == nil {
		_ = os.WriteFile(dst+".json", b, 0o600)
	}
	return rep, nil
}

// Tests replace these: their made-up executables neither sign nor run.
var (
	signFile   = sign
	verifyFile = Verify
)

// sign gives the patched file an ad-hoc signature on macOS, whose kernel
// does not run an arm64 binary with a broken one.
func sign(ctx context.Context, path string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if out, err := exec.CommandContext(ctx, "codesign", "-f", "-s", "-", path).CombinedOutput(); err != nil {
		return fmt.Errorf("droidpatch: codesign: %v: %s", err, out)
	}
	if out, err := exec.CommandContext(ctx, "codesign", "--verify", path).CombinedOutput(); err != nil {
		return fmt.Errorf("droidpatch: codesign --verify: %v: %s", err, out)
	}
	return nil
}
