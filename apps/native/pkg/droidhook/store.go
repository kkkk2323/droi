package droidhook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/netproxy"
)

// DownloadBase is Factory's versioned release path. An app built on Droi may
// point it at a mirror of its own before a Store is made (droiapp.Options);
// each build is still checked against the checksum next to it.
var DownloadBase = "https://downloads.factory.ai/factory-cli/releases"

// LatestURLFor is where the newest version is named for downloads under
// base: Factory keeps LATEST next to its releases folder.
func LatestURLFor(base string) string {
	base = strings.TrimRight(base, "/")
	if b, ok := strings.CutSuffix(base, "/releases"); ok {
		return b + "/LATEST"
	}
	return base + "/LATEST"
}

// Platform is GOOS/GOARCH.
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// Supported reports whether Factory builds droid for platform.
func Supported(platform string) bool {
	switch platform {
	case "darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64", "windows/amd64":
		return true
	}
	return false
}

// Store keeps Factory's droid builds in Dir, one folder per version, and
// the hook next to them.
type Store struct {
	Dir          string
	Platform     string
	HTTP         *http.Client
	DownloadBase string
	LatestURL    string
}

// NewStore is a Store for this computer.
func NewStore(dir string) *Store {
	return &Store{
		Dir:          dir,
		Platform:     Platform(),
		HTTP:         &http.Client{Transport: netproxy.Transport(), Timeout: 30 * time.Minute},
		DownloadBase: DownloadBase,
		LatestURL:    LatestURLFor(DownloadBase),
	}
}

// Hook writes the hook into the Store's folder and answers its path.
func (s *Store) Hook() (string, error) { return WriteHook(s.Dir) }

// Latest is the newest droid version Factory offers.
func (s *Store) Latest(ctx context.Context) (string, error) {
	b, err := s.get(ctx, s.LatestURL, 64)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if !validVersion(v) {
		return "", fmt.Errorf("droidhook: %s names no version", s.LatestURL)
	}
	return v, nil
}

func (s *Store) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("droidhook: %s answered %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func binaryName(goos string) string {
	if goos == "windows" {
		return "droid.exe"
	}
	return "droid"
}

func (s *Store) goos() string {
	goos, _, _ := strings.Cut(s.Platform, "/")
	return goos
}

// Path is where droid of version is.
func (s *Store) Path(version string) string {
	return filepath.Join(s.Dir, version, binaryName(s.goos()))
}

// verified marks a build that ran the hook of the HookVersion it holds.
func (s *Store) verified(version string) string { return s.Path(version) + ".verified" }

// Ready reports whether droid of version is here and ran this hook.
func (s *Store) Ready(version string) bool {
	b, err := os.ReadFile(s.verified(version))
	if err != nil || strings.TrimSpace(string(b)) != HookVersion() {
		return false
	}
	_, err = os.Stat(s.Path(version))
	return err == nil
}

func (s *Store) officialURL(version string) string {
	goos, arch, _ := strings.Cut(s.Platform, "/")
	if arch == "amd64" {
		arch = "x64"
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(s.DownloadBase, "/"), version, goos, arch, binaryName(goos))
}

// Progress is how far a download got.
type Progress struct {
	Done, Total int64
}

// Prepare makes droid of version ready: Factory's build, copied from local
// when that is the very build, else downloaded, then verified with the hook.
// It answers the path.
func (s *Store) Prepare(ctx context.Context, version, local string, progress func(Progress)) (string, error) {
	if s.Ready(version) {
		return s.Path(version), nil
	}
	if !Supported(s.Platform) {
		return "", fmt.Errorf("droidhook: Factory builds no droid for %s", s.Platform)
	}
	hook, err := s.Hook()
	if err != nil {
		return "", err
	}
	want, err := s.officialSHA256(ctx, version)
	if err != nil {
		return "", err
	}
	dst := s.Path(version)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	// A build verified with an older hook only needs verifying again.
	if fileSHA256(dst) != want {
		tmp := dst + ".new"
		if local != "" && fileSHA256(local) == want {
			err = copyFile(local, tmp)
		} else {
			err = s.download(ctx, s.officialURL(version), tmp, want, progress)
		}
		if err == nil {
			err = os.Rename(tmp, dst)
		}
		if err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
	}
	_ = os.Remove(s.verified(version))
	if err := verifyFile(ctx, dst, hook); err != nil {
		return "", err
	}
	if err := os.WriteFile(s.verified(version), []byte(HookVersion()+"\n"), 0o600); err != nil {
		return "", err
	}
	return dst, nil
}

// Tests replace it: their made-up executables do not run.
var verifyFile = Verify

// SetVerifyForTest replaces the check Prepare runs, for the tests of a
// package that prepares made-up builds; restore puts Verify back.
func SetVerifyForTest(verify func(ctx context.Context, exe, hook string) error) (restore func()) {
	verifyFile = verify
	return func() { verifyFile = Verify }
}

func (s *Store) officialSHA256(ctx context.Context, version string) (string, error) {
	sum, err := s.get(ctx, s.officialURL(version)+".sha256", 1<<10)
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.Fields(string(sum) + " ")[0])
	if len(want) != 64 {
		return "", errors.New("droidhook: Factory's checksum does not read")
	}
	return want, nil
}

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func (s *Store) download(ctx context.Context, url, path, want string, progress func(Progress)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("droidhook: %s answered %d", url, resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, report: progress}
	_, err = io.Copy(io.MultiWriter(f, h, pw), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = errors.New("droidhook: the downloaded droid does not match Factory's checksum")
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

type progressWriter struct {
	done, total int64
	last        time.Time
	report      func(Progress)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.report != nil && (time.Since(p.last) > 200*time.Millisecond || p.done == p.total) {
		p.last = time.Now()
		p.report(Progress{Done: p.done, Total: p.total})
	}
	return len(b), nil
}

// Prune removes the builds of every version but keep, and what the Patched
// Droid (ADR 0027) left in the same folder.
func (s *Store) Prune(keep ...string) {
	entries, _ := os.ReadDir(s.Dir)
	for _, e := range entries {
		if e.IsDir() && !slices.Contains(keep, e.Name()) && validVersion(e.Name()) {
			_ = os.RemoveAll(filepath.Join(s.Dir, e.Name()))
		}
	}
	_ = os.Remove(filepath.Join(s.Dir, "recipes.json"))
}

func validVersion(v string) bool {
	_, ok := semver(v)
	return ok
}

func semver(s string) ([3]int, bool) {
	var v [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(s), "v"), "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// CompareVersions orders droid versions like 0.247.0; unreadable ones sort
// first.
func CompareVersions(a, b string) int {
	x, okA := semver(a)
	y, okB := semver(b)
	switch {
	case !okA && !okB:
		return strings.Compare(a, b)
	case !okA:
		return -1
	case !okB:
		return 1
	}
	for i := range x {
		if x[i] != y[i] {
			if x[i] < y[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
