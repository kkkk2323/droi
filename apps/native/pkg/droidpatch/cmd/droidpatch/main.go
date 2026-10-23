// Command droidpatch makes and publishes the recipes of the Patched Droid
// (ADR 0027). CI runs `droidpatch update` on a schedule; a developer can run
// it by hand for a droid the recipes lack:
//
//	go run ./apps/native/pkg/droidpatch/cmd/droidpatch update -recipes old.json -out dist
//
// It patches the official build for this computer, verifies the patch against
// a local receiver, and writes dist/recipes.json, signed into
// dist/recipes.json.sig when DROI_RECIPE_SIGNING_KEY holds the key.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidpatch"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "update" {
		fmt.Fprintln(os.Stderr, "usage: droidpatch update [-version V] [-binary path] [-recipes file] -out dir")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	version := fs.String("version", "", "droid version; the latest when empty")
	binary := fs.String("binary", "", "an official droid of that version, instead of downloading it")
	recipes := fs.String("recipes", "", "the recipes published so far")
	out := fs.String("out", "", "folder for recipes.json and recipes.json.sig")
	_ = fs.Parse(os.Args[2:])
	if *out == "" {
		fs.Usage()
		os.Exit(2)
	}
	if err := update(*version, *binary, *recipes, *out); err != nil {
		fmt.Fprintln(os.Stderr, "droidpatch:", err)
		os.Exit(1)
	}
}

func update(version, binary, recipesFile, out string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if version == "" {
		v, err := latest(ctx)
		if err != nil {
			return err
		}
		version = v
	}
	var have []droidpatch.Recipe
	if recipesFile != "" {
		b, err := os.ReadFile(recipesFile)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			have = parse(b)
		}
	}
	platform := droidpatch.Platform()
	for _, r := range have {
		if r.Version == version && r.Supports(platform) {
			fmt.Printf("droid %s on %s already has a recipe\n", version, platform)
			return write(out, have)
		}
	}

	work, err := os.MkdirTemp("", "droidpatch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	store := droidpatch.NewStore(work)
	if binary == "" {
		binary = filepath.Join(work, "official")
		if err := download(ctx, store, version, binary); err != nil {
			return err
		}
	}
	blob, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	r, err := droidpatch.Generate(blob, version)
	if err != nil {
		return err
	}
	r.Platforms = []string{platform}
	if _, err := droidpatch.Build(ctx, binary, filepath.Join(work, "patched"), r); err != nil {
		return fmt.Errorf("droid %s: %w", version, err)
	}
	merged := false
	for i, old := range have {
		if old.Version != version {
			continue
		}
		if old.ModuleSHA256 == r.ModuleSHA256 && old.Anchor == r.Anchor && old.Inject == r.Inject {
			if !slices.Contains(old.Platforms, platform) {
				have[i].Platforms = append(old.Platforms, platform)
			}
		} else {
			// Another platform's build differs; this one's recipe replaces it
			// only for this platform.
			have = append(have, r)
		}
		merged = true
		break
	}
	if !merged {
		have = append(have, r)
	}
	fmt.Printf("droid %s on %s: patched and verified (%s)\n", version, platform, r.ModuleSHA256[:12])
	return write(out, have)
}

func parse(b []byte) []droidpatch.Recipe {
	var f droidpatch.Recipes
	if err := jsonUnmarshal(b, &f); err != nil {
		return nil
	}
	return f.Recipes
}

func write(out string, rs []droidpatch.Recipe) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	b := droidpatch.Marshal(rs)
	if err := os.WriteFile(filepath.Join(out, "recipes.json"), b, 0o644); err != nil {
		return err
	}
	seed := os.Getenv("DROI_RECIPE_SIGNING_KEY")
	if seed == "" {
		fmt.Println("DROI_RECIPE_SIGNING_KEY is not set; recipes.json is not signed")
		return nil
	}
	sig, err := droidpatch.Sign(b, seed)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "recipes.json.sig"), []byte(sig+"\n"), 0o644)
}

func latest(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://downloads.factory.ai/factory-cli/LATEST", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LATEST answered %d", resp.StatusCode)
	}
	v := strings.TrimSpace(string(b))
	if droidpatch.CompareVersions(v, "0.0.0") <= 0 {
		return "", fmt.Errorf("LATEST is %q", v)
	}
	return v, nil
}
