package droidpatch

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeStore(t *testing.T, official []byte) (*Store, *int) {
	t.Helper()
	signFile = func(context.Context, string) error { return nil }
	verifyFile = func(context.Context, string) error { return nil }
	t.Cleanup(func() { signFile, verifyFile = sign, Verify })
	sum := sha256.Sum256(official)
	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  droid\n"))
		case strings.HasSuffix(r.URL.Path, "/9.9.9/darwin/x64/droid"):
			downloads++
			w.Write(official)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	s := NewStore(filepath.Join(t.TempDir(), "droid"))
	s.Platform = "darwin/amd64"
	s.DownloadBase = srv.URL
	return s, &downloads
}

func TestPrepareDownloadsChecksAndPatches(t *testing.T) {
	official := fakeDroid()
	s, downloads := fakeStore(t, official)
	r, err := Generate(official, "9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	r.Platforms = []string{"darwin/amd64"}
	path, err := s.Prepare(context.Background(), r, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if *downloads != 1 || path != s.Path("9.9.9") || !s.Ready("9.9.9") {
		t.Fatalf("downloads %d, path %s, ready %v", *downloads, path, s.Ready("9.9.9"))
	}
	if _, err := os.Stat(path + ".official"); !os.IsNotExist(err) {
		t.Fatal("the official download was kept")
	}
	g, _ := Parse(mustRead(t, path))
	if m, _ := g.Target(); !strings.Contains(string(m.Contents), r.Inject) {
		t.Fatal("the stored droid is not patched")
	}
	if got := s.Installed(); len(got) != 1 || got[0] != "9.9.9" {
		t.Fatalf("installed %v", got)
	}
	// Ready: no second download.
	if _, err := s.Prepare(context.Background(), r, "", nil); err != nil || *downloads != 1 {
		t.Fatalf("second Prepare: %v, downloads %d", err, *downloads)
	}
	s.Prune()
	if s.Ready("9.9.9") {
		t.Fatal("Prune kept a version")
	}
}

func TestPrepareUsesTheLocalOfficialBuild(t *testing.T) {
	official := fakeDroid()
	s, downloads := fakeStore(t, official)
	local := filepath.Join(t.TempDir(), "droid")
	os.WriteFile(local, official, 0o700)
	r, _ := Generate(official, "9.9.9")
	r.Platforms = []string{"darwin/amd64"}
	if _, err := s.Prepare(context.Background(), r, local, nil); err != nil {
		t.Fatal(err)
	}
	if *downloads != 0 {
		t.Fatal("downloaded although the local droid is the official build")
	}
	// A local droid that is not Factory's build is not used.
	os.WriteFile(local, append(official, 'x'), 0o700)
	s.Prune()
	if _, err := s.Prepare(context.Background(), r, local, nil); err != nil || *downloads != 1 {
		t.Fatalf("Prepare: %v, downloads %d", err, *downloads)
	}
}

func TestPrepareRefusesAnotherPlatform(t *testing.T) {
	s, _ := fakeStore(t, fakeDroid())
	r, _ := Generate(fakeDroid(), "9.9.9")
	r.Platforms = []string{"windows/amd64"}
	if _, err := s.Prepare(context.Background(), r, "", nil); err == nil {
		t.Fatal("prepared a recipe not verified on this platform")
	}
}

func TestRefreshRecipesNeedsTheSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	body := Marshal([]Recipe{{Version: "9.9.9", Platforms: []string{"darwin/amd64"}}, {Version: "9.10.0", Platforms: []string{"darwin/amd64"}}})
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	served := sig
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			w.Write([]byte(served))
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	s := NewStore(t.TempDir())
	s.Platform = "darwin/amd64"
	s.RecipesURL = srv.URL + "/recipes.json"
	s.PublicKey = pub
	served = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("other")))
	if err := s.RefreshRecipes(context.Background()); err == nil {
		t.Fatal("kept recipes with a wrong signature")
	}
	served = sig
	if err := s.RefreshRecipes(context.Background()); err != nil {
		t.Fatal(err)
	}
	rs := s.Recipes()
	if len(rs) < 2 || rs[0].Version != "9.10.0" || rs[1].Version != "9.9.9" {
		t.Fatalf("recipes = %+v", rs)
	}
}

func TestEmbeddedRecipesParse(t *testing.T) {
	s := NewStore(t.TempDir())
	s.Platform = "darwin/arm64"
	if r, ok := s.Recipe("0.247.0"); !ok || len(r.ModuleSHA256) != 64 {
		t.Fatalf("no embedded recipe for 0.247.0 on darwin/arm64: %+v", r)
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("0.247.0", "0.99.1") <= 0 || CompareVersions("1.0.0", "1.0.0") != 0 || CompareVersions("x", "0.1.0") >= 0 {
		t.Fatal("version order")
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
