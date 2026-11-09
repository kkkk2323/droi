package keys

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/llmproxy/contentenc"
)

// Billing polls carry the headers droid sends on its own
// GET /api/billing/limits (captured from droid 0.247.0 on Bun 1.4.3), so they
// look like droid's.
const (
	defaultClientUserAgent = "Bun/1.4.3"
	factoryClientCLI       = "cli"
	whoamiPath             = "/api/cli/whoami"
	// orgRetry is how long a key whose whoami failed waits before another try.
	orgRetry = 10 * time.Minute
)

// Identity is what whoami says of a key: its organization, the region that organization is served from, and the
// organization's own host, when it has one.
type Identity struct {
	OrgID        string
	Region       string
	PremBaseHost string
}

type orgLookup struct {
	id Identity
	at time.Time
}

// readDroidVersion reads the installed droid's version until droid's own
// traffic shows it. Tests replace it.
var readDroidVersion = installedDroidVersion

var droidVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+\S*$`)

// NoteClientHeaders records droid's version and runtime from a request it sent
// through the proxy, for the billing polls to reuse.
func (r *KeyRotator) NoteClientHeaders(h http.Header) {
	if v := strings.TrimSpace(h.Get("X-Client-Version")); v != "" {
		r.clientVersion.Store(v)
	}
	if ua := h.Get("User-Agent"); strings.HasPrefix(ua, "Bun/") {
		r.clientUA.Store(ua)
	}
}

func (r *KeyRotator) clientHeaders() (ua, version string) {
	ua, _ = r.clientUA.Load().(string)
	if ua == "" {
		ua = defaultClientUserAgent
	}
	version, _ = r.clientVersion.Load().(string)
	if version == "" {
		r.versionOnce.Do(func() {
			if v := readDroidVersion(); v != "" {
				r.clientVersion.CompareAndSwap(nil, v)
			}
		})
		version, _ = r.clientVersion.Load().(string)
	}
	return ua, version
}

// installedDroidVersion runs `droid --version`. A GUI app's PATH often lacks
// ~/.local/bin, where droid installs itself, so that is tried as well.
func installedDroidVersion() string {
	var paths []string
	if p, err := exec.LookPath("droid"); err == nil {
		paths = append(paths, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".local", "bin", "droid"))
	}
	for _, p := range paths {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := exec.CommandContext(ctx, p, "--version").Output()
		cancel()
		if err != nil {
			continue
		}
		line, _, _ := strings.Cut(string(out), "\n")
		if v := strings.TrimSpace(line); droidVersionPattern.MatchString(v) {
			return v
		}
	}
	return ""
}

// orgID returns key's own organization, looked up through whoami once per
// run: a pool key belongs to another org than the droid account, so the
// account's X-Factory-Org-Id would not match the key.
func (r *KeyRotator) orgID(ctx context.Context, key, ua string) string {
	return r.identity(ctx, key, ua).OrgID
}

// Identity is key's own organization, region and host, from whoami once per run (a failed whoami is asked again
// after orgRetry). A model request the proxy sends for a client that holds no key is addressed by it.
func (r *KeyRotator) Identity(ctx context.Context, key string) Identity {
	ua, _ := r.clientHeaders()
	return r.identity(ctx, key, ua)
}

func (r *KeyRotator) identity(ctx context.Context, key, ua string) Identity {
	r.orgMu.Lock()
	got, ok := r.orgIDs[key]
	r.orgMu.Unlock()
	if ok && (got.id.OrgID != "" || r.nowTime().Sub(got.at) < orgRetry) {
		return got.id
	}
	id := r.fetchIdentity(ctx, key, ua)
	r.orgMu.Lock()
	if r.orgIDs == nil {
		r.orgIDs = make(map[string]orgLookup)
	}
	r.orgIDs[key] = orgLookup{id: id, at: r.nowTime()}
	r.orgMu.Unlock()
	return id
}

// fetchIdentity asks whoami on the billing endpoint's host, with the headers
// droid sends on its own whoami.
func (r *KeyRotator) fetchIdentity(ctx context.Context, key, ua string) Identity {
	u, err := url.Parse(r.cfg.UsageAPIURL)
	if err != nil || u.Host == "" {
		return Identity{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Scheme+"://"+u.Host+whoamiPath, nil)
	if err != nil {
		return Identity{}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Factory-Whoami-Extended", "true")
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", contentenc.AcceptEncoding)
	resp, err := r.client.Do(req)
	if err != nil {
		return Identity{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Identity{}
	}
	body, closer, ok := contentenc.Decode(resp.Header.Get("Content-Encoding"), resp.Body)
	if closer != nil {
		defer closer.Close()
	}
	if !ok {
		return Identity{}
	}
	var who struct {
		OrgID          string `json:"orgId"`
		Region         string `json:"region"`
		PremBaseHostV2 string `json:"premBaseHostV2"`
	}
	if json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&who) != nil {
		return Identity{}
	}
	return Identity{OrgID: who.OrgID, Region: who.Region, PremBaseHost: who.PremBaseHostV2}
}
