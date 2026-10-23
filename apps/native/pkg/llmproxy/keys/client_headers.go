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

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/contentenc"
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

type orgLookup struct {
	id string
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
	r.orgMu.Lock()
	got, ok := r.orgIDs[key]
	r.orgMu.Unlock()
	if ok && (got.id != "" || r.nowTime().Sub(got.at) < orgRetry) {
		return got.id
	}
	id := r.fetchOrgID(ctx, key, ua)
	r.orgMu.Lock()
	if r.orgIDs == nil {
		r.orgIDs = make(map[string]orgLookup)
	}
	r.orgIDs[key] = orgLookup{id: id, at: r.nowTime()}
	r.orgMu.Unlock()
	return id
}

// fetchOrgID asks whoami on the billing endpoint's host, with the headers
// droid sends on its own whoami.
func (r *KeyRotator) fetchOrgID(ctx context.Context, key, ua string) string {
	u, err := url.Parse(r.cfg.UsageAPIURL)
	if err != nil || u.Host == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Scheme+"://"+u.Host+whoamiPath, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Factory-Whoami-Extended", "true")
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", contentenc.AcceptEncoding)
	resp, err := r.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, closer, ok := contentenc.Decode(resp.Header.Get("Content-Encoding"), resp.Body)
	if closer != nil {
		defer closer.Close()
	}
	if !ok {
		return ""
	}
	var who struct {
		OrgID string `json:"orgId"`
	}
	if json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&who) != nil {
		return ""
	}
	return who.OrgID
}
