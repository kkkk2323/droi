package droidpatch

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
)

// Recipes is the file of recipes, embedded and published.
type Recipes struct {
	Recipes []Recipe `json:"recipes"`
}

//go:embed recipes.json
var embeddedRecipes []byte

// PublicKey verifies the published recipes.json; CI signs it with the
// private half (DROI_RECIPE_SIGNING_KEY). A recipe injects code into droid,
// so an unsigned one is never used.
var PublicKey = mustKey("3R7As7yszZZeC0FNkfoeRWG6D0Y3ft3irv8lSRV4WjM=")

func mustKey(s string) ed25519.PublicKey {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("droidpatch: bad public key")
	}
	return b
}

const (
	// RecipesURL is where CI publishes recipes for new droid versions.
	RecipesURL = "https://github.com/kkkk2323/droi/releases/download/droid-patch/recipes.json"
	// DownloadBase is Factory's versioned release path.
	DownloadBase = "https://downloads.factory.ai/factory-cli/releases"
)

// Platform is GOOS/GOARCH, as recipes name it.
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// Store keeps the Patched Droids in Dir, one folder per version.
type Store struct {
	Dir          string
	Platform     string
	HTTP         *http.Client
	RecipesURL   string
	DownloadBase string
	PublicKey    ed25519.PublicKey
}

// NewStore is a Store for this computer.
func NewStore(dir string) *Store {
	return &Store{
		Dir:          dir,
		Platform:     Platform(),
		HTTP:         &http.Client{Timeout: 30 * time.Minute},
		RecipesURL:   RecipesURL,
		DownloadBase: DownloadBase,
		PublicKey:    PublicKey,
	}
}

func (s *Store) recipesCache() string { return filepath.Join(s.Dir, "recipes.json") }

func parseRecipes(b []byte) []Recipe {
	var f Recipes
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return f.Recipes
}

// Recipes are the recipes for this platform, the newest droid first: the
// embedded ones and those last fetched.
func (s *Store) Recipes() []Recipe {
	// A version has a recipe per platform when the platforms' builds differ.
	byVersion := map[string]Recipe{}
	add := func(rs []Recipe) {
		for _, r := range rs {
			if r.Supports(s.Platform) {
				byVersion[r.Version] = r
			}
		}
	}
	add(parseRecipes(embeddedRecipes))
	if b, err := os.ReadFile(s.recipesCache()); err == nil {
		add(parseRecipes(b))
	}
	var out []Recipe
	for _, r := range byVersion {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Recipe) int { return CompareVersions(b.Version, a.Version) })
	return out
}

// Recipe is the recipe for version on this platform.
func (s *Store) Recipe(version string) (Recipe, bool) {
	for _, r := range s.Recipes() {
		if r.Version == version {
			return r, true
		}
	}
	return Recipe{}, false
}

// RefreshRecipes fetches the published recipes and keeps them when their
// signature verifies.
func (s *Store) RefreshRecipes(ctx context.Context) error {
	body, err := s.get(ctx, s.RecipesURL, 1<<20)
	if err != nil {
		return err
	}
	sig, err := s.get(ctx, s.RecipesURL+".sig", 1<<10)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(s.PublicKey, body, raw) {
		return errors.New("droidpatch: the published recipes are not signed by Droi")
	}
	if parseRecipes(body) == nil {
		return errors.New("droidpatch: the published recipes do not parse")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp := s.recipesCache() + ".new"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.recipesCache())
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
		return nil, fmt.Errorf("droidpatch: %s answered %d", url, resp.StatusCode)
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

// Path is where the Patched Droid of version is.
func (s *Store) Path(version string) string {
	return filepath.Join(s.Dir, version, binaryName(s.goos()))
}

// Ready reports whether the Patched Droid of version was built and verified.
func (s *Store) Ready(version string) bool {
	_, err := os.Stat(s.Path(version) + ".json")
	if err != nil {
		return false
	}
	_, err = os.Stat(s.Path(version))
	return err == nil
}

// Installed are the versions with a Patched Droid, the newest first.
func (s *Store) Installed() []string {
	entries, _ := os.ReadDir(s.Dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() && s.Ready(e.Name()) {
			out = append(out, e.Name())
		}
	}
	slices.SortFunc(out, func(a, b string) int { return CompareVersions(b, a) })
	return out
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

// Prepare makes the Patched Droid for r: from local when that is the very
// official build, else from Factory's download. It answers the patched file.
func (s *Store) Prepare(ctx context.Context, r Recipe, local string, progress func(Progress)) (string, error) {
	if s.Ready(r.Version) {
		return s.Path(r.Version), nil
	}
	if !r.Supports(s.Platform) {
		return "", fmt.Errorf("droidpatch: the %s recipe was not verified on %s", r.Version, s.Platform)
	}
	want, err := s.officialSHA256(ctx, r.Version)
	if err != nil {
		return "", err
	}
	dst := s.Path(r.Version)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	src := ""
	if local != "" && fileSHA256(local) == want {
		src = local
	} else {
		src = dst + ".official"
		if err := s.download(ctx, s.officialURL(r.Version), src, want, progress); err != nil {
			return "", err
		}
		defer os.Remove(src)
	}
	if _, err := Build(ctx, src, dst, r); err != nil {
		return "", err
	}
	return dst, nil
}

func (s *Store) officialSHA256(ctx context.Context, version string) (string, error) {
	sum, err := s.get(ctx, s.officialURL(version)+".sha256", 1<<10)
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.Fields(string(sum) + " ")[0])
	if len(want) != 64 {
		return "", errors.New("droidpatch: Factory's checksum does not read")
	}
	return want, nil
}

// DownloadOfficial fetches Factory's build of version for the Store's
// platform into path, checked against Factory's checksum.
func (s *Store) DownloadOfficial(ctx context.Context, version, path string, progress func(Progress)) error {
	want, err := s.officialSHA256(ctx, version)
	if err != nil {
		return err
	}
	return s.download(ctx, s.officialURL(version), path, want, progress)
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
		return fmt.Errorf("droidpatch: %s answered %d", url, resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
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
		err = errors.New("droidpatch: the downloaded droid does not match Factory's checksum")
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

// Prune removes the Patched Droids of every version but keep.
func (s *Store) Prune(keep ...string) {
	entries, _ := os.ReadDir(s.Dir)
	for _, e := range entries {
		if e.IsDir() && !slices.Contains(keep, e.Name()) && validVersion(e.Name()) {
			_ = os.RemoveAll(filepath.Join(s.Dir, e.Name()))
		}
	}
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

// Sign signs a recipes file with the base64 seed of the private key.
func Sign(recipes []byte, seed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seed))
	if err != nil || len(raw) != ed25519.SeedSize {
		return "", errors.New("droidpatch: the signing key does not read")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(raw), recipes)), nil
}

// Marshal writes recipes the way they are published.
func Marshal(rs []Recipe) []byte {
	slices.SortFunc(rs, func(a, b Recipe) int { return CompareVersions(b.Version, a.Version) })
	b, _ := json.MarshalIndent(Recipes{Recipes: rs}, "", "  ")
	return append(bytes.TrimSpace(b), '\n')
}
