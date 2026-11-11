package droidhook

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"
)

// ModelsEnv names the file the model export writes to.
const ModelsEnv = "DROI_MODELS_OUT"

//go:embed model-export.mjs
var modelExport []byte

// exportVersion names the embedded export: an export written by another
// one is made again.
func exportVersion() string {
	sum := sha256.Sum256(modelExport)
	return hex.EncodeToString(sum[:6])
}

// Models is what droid's own model registry says of the models it offers
// (ModelsEnv's export): Factory's credit multipliers, promotions and billing
// pools, as the build that runs bills them.
type Models struct {
	Export string `json:"export"`
	// OutputByProvider and OutputDefault are the output multipliers of a
	// model whose cost leaves its own out; empty when the export did not
	// find them.
	OutputByProvider map[string]float64 `json:"outputByProvider,omitempty"`
	OutputDefault    float64            `json:"outputDefault,omitempty"`
	Models           []Model            `json:"models"`
	Error            string             `json:"error,omitempty"`
}

// Model is one model of the registry. Credits are, as droid counts them,
// multiplier × (input + 1.25×cache write + cache-read multiplier × cache
// read + output multiplier × output), the multiplier less the discount of
// the promotion running at the time.
type Model struct {
	ID                       string      `json:"id"`
	Name                     string      `json:"name,omitempty"`
	Provider                 string      `json:"provider,omitempty"`
	BillingPool              string      `json:"billingPool,omitempty"`
	TokenMultiplier          float64     `json:"tokenMultiplier"`
	OutputTokenMultiplier    *float64    `json:"outputTokenMultiplier,omitempty"`
	CacheReadTokenMultiplier *float64    `json:"cacheReadTokenMultiplier,omitempty"`
	Promotions               []Promotion `json:"promotions,omitempty"`
}

// Promotion takes Discount off a model's multiplier from StartsAt until
// ExpiresAt; a zero time leaves that end open.
type Promotion struct {
	Discount  float64   `json:"discount"`
	StartsAt  time.Time `json:"startsAt,omitzero"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// The CLI's defaults as of droid 1.7.1, for an export that did not find its
// own.
var (
	defaultOutputByProvider = map[string]float64{"anthropic": 5, "openai": 8, "google": 6, "factory": 4, "xai": 5}
	defaultOutput           = 4.0
)

// CacheReadDefault is the cache-read multiplier of a model whose cost leaves
// its own out.
const CacheReadDefault = 0.1

// minModels is fewer models than any droid offers: an export with fewer
// found something else than the registry.
const minModels = 30

// OutputMultiplier is m's output multiplier, as droid falls back to it.
func (ms *Models) OutputMultiplier(m Model) float64 {
	if m.OutputTokenMultiplier != nil {
		return *m.OutputTokenMultiplier
	}
	if len(ms.OutputByProvider) > 0 {
		if v, ok := ms.OutputByProvider[m.Provider]; ok {
			return v
		}
		return ms.OutputDefault
	}
	if v, ok := defaultOutputByProvider[m.Provider]; ok {
		return v
	}
	return defaultOutput
}

// CacheReadMultiplier is m's cache-read multiplier.
func (m Model) CacheReadMultiplier() float64 {
	if m.CacheReadTokenMultiplier != nil {
		return *m.CacheReadTokenMultiplier
	}
	return CacheReadDefault
}

// MultiplierAt is m's multiplier at t, less the promotion running then,
// rounded as droid rounds it.
func (m Model) MultiplierAt(t time.Time) float64 {
	for _, p := range m.Promotions {
		if p.Discount == 0 || t.Before(p.StartsAt) || !p.ExpiresAt.IsZero() && !t.Before(p.ExpiresAt) {
			continue
		}
		return math.Round(m.TokenMultiplier*(1-p.Discount)*1e4) / 1e4
	}
	return m.TokenMultiplier
}

func (ms *Models) check() error {
	if ms.Error != "" {
		return fmt.Errorf("droidhook: the model export failed: %s", ms.Error)
	}
	if len(ms.Models) < minModels {
		return fmt.Errorf("droidhook: the model export found %d models", len(ms.Models))
	}
	for _, m := range ms.Models {
		if m.ID == "" || m.TokenMultiplier < 0 {
			return fmt.Errorf("droidhook: the model export holds a model %q at %v", m.ID, m.TokenMultiplier)
		}
	}
	return nil
}

// ReadModels reads an export ExportModels wrote.
func ReadModels(path string) (*Models, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ms Models
	if err := json.Unmarshal(b, &ms); err != nil {
		return nil, err
	}
	return &ms, ms.check()
}

// ExportModels runs exe, Factory's droid, as `droid --version` with the
// model export preloaded, and writes what it finds to out. No request
// leaves this computer: droid only prints its version.
func ExportModels(ctx context.Context, exe, out string) (*Models, error) {
	home, err := os.MkdirTemp("", "droi-models-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	script, tmp := filepath.Join(home, "model-export.mjs"), filepath.Join(home, "models.json")
	if err := os.WriteFile(script, modelExport, 0o600); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, exe, "--version")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home, "TMPDIR=" + home,
		"FACTORY_HOME_OVERRIDE=" + home, "FACTORY_DISABLE_KEYRING=1", "FACTORY_DROID_AUTO_UPDATE_ENABLED=false",
		"BUN_OPTIONS=" + BunOptions(script, ""), ModelsEnv + "=" + tmp,
	}
	// Windows' droid needs these to start; they hold no credential.
	for _, name := range []string{"SystemRoot", "SYSTEMROOT", "windir", "TEMP", "TMP", "LOCALAPPDATA", "APPDATA"} {
		if v, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+v)
		}
	}
	hideConsole(cmd)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("droidhook: droid --version: %w: %s", err, b)
	}
	b, err := os.ReadFile(tmp)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("droidhook: the model export did not run in droid")
	}
	if err != nil {
		return nil, err
	}
	var ms Models
	if err := json.Unmarshal(b, &ms); err != nil {
		return nil, err
	}
	if err := ms.check(); err != nil {
		return nil, err
	}
	ms.Export = exportVersion()
	b, err = json.Marshal(&ms)
	if err != nil {
		return nil, err
	}
	return &ms, WritePrivate(out, b)
}

// ModelsPath is where the model export of droid of version is kept.
func (s *Store) ModelsPath(version string) string {
	return filepath.Join(s.Dir, version, "models.json")
}

// Models is what droid of version says of its models: the kept export when
// this export wrote it, else a new one.
func (s *Store) Models(ctx context.Context, version string) (*Models, error) {
	path := s.ModelsPath(version)
	if ms, err := ReadModels(path); err == nil && ms.Export == exportVersion() {
		return ms, nil
	}
	return (*exportModels.Load())(ctx, s.Path(version), path)
}

type exportFunc = func(ctx context.Context, exe, out string) (*Models, error)

// Tests replace it, while a Host of an earlier test may still read its
// made-up build's models.
var exportModels atomic.Pointer[exportFunc]

func init() {
	f := exportFunc(ExportModels)
	exportModels.Store(&f)
}

// SetExportForTest replaces the export Store.Models runs, for the tests of
// a package whose made-up builds would run it; restore puts ExportModels
// back.
func SetExportForTest(export func(ctx context.Context, exe, out string) (*Models, error)) (restore func()) {
	f := exportFunc(export)
	exportModels.Store(&f)
	return func() {
		f := exportFunc(ExportModels)
		exportModels.Store(&f)
	}
}
