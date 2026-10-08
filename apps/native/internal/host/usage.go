package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/l10n"
)

// usageTTL is how long an account's usage stands before it is fetched
// again; the Account tab asks on every frame it draws.
const usageTTL = time.Minute

// UsageWindow is one of a pool's rate-limit windows. Ends is zero while the
// window has not started, and in the past once it is over.
type UsageWindow struct {
	Percent float64
	Ends    time.Time
}

// Active reports that the window is running, as droid counts it.
func (w UsageWindow) Active(now time.Time) bool { return !w.Ends.IsZero() && !w.Ends.Before(now) }

// UsagePool is a billing pool's windows: Standard or Core.
type UsagePool struct {
	FiveHour, Weekly, Monthly UsageWindow
}

// Usage is an account's Factory usage, from /api/billing/limits.
type Usage struct {
	Standard, Core UsagePool
	// Overage is what runs once Standard is used up: "droidCore",
	// "extraUsage", or "" for nothing.
	Overage    string
	ExtraCents int
}

// UsageState is where an account's usage stands: the last answer, why the
// last fetch failed, and whether one is under way.
type UsageState struct {
	Usage    *Usage
	Err      string
	Fetching bool
	Checked  time.Time
}

type usageEntry struct {
	UsageState
	tried time.Time
}

// AccountUsage is an account's usage as last fetched.
func (h *Host) AccountUsage(id string) UsageState {
	h.usageMu.Lock()
	defer h.usageMu.Unlock()
	if e := h.usage[id]; e != nil {
		return e.UsageState
	}
	return UsageState{}
}

// RefreshUsage fetches an account's usage in the background, unless it was
// fetched in the last minute or is being fetched; listeners hear the answer.
func (h *Host) RefreshUsage(id string) {
	h.usageMu.Lock()
	e := h.usage[id]
	if e == nil {
		e = &usageEntry{}
		h.usage[id] = e
	}
	if e.Fetching || time.Since(e.tried) < usageTTL {
		h.usageMu.Unlock()
		return
	}
	e.Fetching, e.tried = true, time.Now()
	h.usageMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		u, err := h.fetchUsage(ctx, id)
		h.usageMu.Lock()
		e.Fetching = false
		if err != nil {
			e.Err = err.Error()
		} else {
			e.Usage, e.Err, e.Checked = u, "", time.Now()
		}
		h.usageMu.Unlock()
		h.changed()
	}()
}

// fetchUsage asks Factory, through the Factory API base URL the Daemon uses
// (a local droid-proxy, say), the way droid asks: the account's own token
// and droid's client headers.
func (h *Host) fetchUsage(ctx context.Context, id string) (*Usage, error) {
	home := h.homeOf(id)
	login := h.cli(home).Read()
	if login == nil {
		return nil, hostError(l10n.L("Signed out."))
	}
	token := login.AccessToken
	if exp := JWTExpiry(token); !exp.IsZero() && time.Until(exp) < time.Minute {
		// The droid CLI and the Daemon refresh their own logins; doing it here
		// as well would race their rotation. An added account no one runs is
		// Droi's alone.
		if id == "" || id == h.ActiveAccount() {
			return nil, hostError(l10n.L("The login is being renewed."))
		}
		var err error
		if token, err = h.renewLogin(ctx, home, login); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.FactoryAPIBaseURL(), "/")+"/api/billing/limits", nil)
	if err != nil {
		return nil, err
	}
	version, agent := h.droidClient()
	req.Header["Authorization"] = []string{"Bearer " + token}
	req.Header["X-Factory-Client"] = []string{"cli"}
	if version != "" {
		req.Header["X-Client-Version"] = []string{version}
	}
	if login.Account.OrgID != nil {
		req.Header["X-Factory-Org-Id"] = []string{*login.Account.OrgID}
	}
	if agent != "" {
		req.Header["User-Agent"] = []string{agent}
	}
	req.Header["Accept"] = []string{"*/*"}
	res, err := h.http().Do(req)
	if err != nil {
		return nil, hostError(l10n.L("Factory could not be reached."))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, hostError(l10n.L("Factory answered %d.", res.StatusCode))
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseUsage(b)
}

func parseUsage(b []byte) (*Usage, error) {
	type window struct {
		UsedPercent float64 `json:"usedPercent"`
		WindowEnd   *string `json:"windowEnd"`
	}
	type pool struct {
		FiveHour, Weekly, Monthly window
	}
	var v struct {
		Limits *struct {
			Standard pool `json:"standard"`
			Core     pool `json:"core"`
		} `json:"limits"`
		OveragePreference      *string `json:"overagePreference"`
		ExtraUsageBalanceCents int     `json:"extraUsageBalanceCents"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Limits == nil {
		return nil, hostError(l10n.L("Factory sent no usage."))
	}
	w := func(x window) UsageWindow {
		out := UsageWindow{Percent: x.UsedPercent}
		if x.WindowEnd != nil {
			out.Ends, _ = time.Parse(time.RFC3339, *x.WindowEnd)
		}
		return out
	}
	p := func(x pool) UsagePool { return UsagePool{w(x.FiveHour), w(x.Weekly), w(x.Monthly)} }
	return &Usage{Standard: p(v.Limits.Standard), Core: p(v.Limits.Core), Overage: deref(v.OveragePreference), ExtraCents: v.ExtraUsageBalanceCents}, nil
}

// renewLogin refreshes an added account's login, as droid would, and gives
// the new one back to its folder.
func (h *Host) renewLogin(ctx context.Context, home string, login *CliLogin) (string, error) {
	if login.RefreshToken == "" {
		return "", hostError(l10n.L("Sign in again to see usage."))
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {login.RefreshToken}}
	if org, ok := DecodeJWT(login.AccessToken)["org_id"].(string); ok && org != "" {
		form.Set("organization_id", org)
	}
	res, b, err := h.adding.post(ctx, "/authenticate", form)
	if err != nil {
		return "", hostError(l10n.L("Factory could not be reached."))
	}
	var t struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if res.StatusCode/100 != 2 || json.Unmarshal(b, &t) != nil || t.AccessToken == "" {
		return "", hostError(l10n.L("Sign in again to see usage."))
	}
	if err := WriteCliLogin(home, t.AccessToken, t.RefreshToken); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

func (h *Host) http() *http.Client {
	if h.adding.HTTP != nil {
		return h.adding.HTTP
	}
	return http.DefaultClient
}

// droidClient is the X-Client-Version and User-Agent droid's own requests
// carry: its version, and the Bun it was compiled with. Read once per droid
// build.
func (h *Host) droidClient() (version, agent string) {
	path := h.DroidPath()
	build := BuildOf(path)
	if build == nil {
		return "", ""
	}
	h.usageMu.Lock()
	c := h.client
	h.usageMu.Unlock()
	if c.build != nil && *c.build == *build {
		return c.version, c.agent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	hideConsole(cmd)
	if out, err := cmd.Output(); err == nil {
		version = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	}
	agent = bunAgent(path)
	h.usageMu.Lock()
	h.client = droidClientInfo{build: build, version: version, agent: agent}
	h.usageMu.Unlock()
	return version, agent
}

type droidClientInfo struct {
	build          *DroidBuild
	version, agent string
}

var bunAgentPattern = regexp.MustCompile(`Bun/\d+\.\d+\.\d+`)

// bunAgent is the "Bun/x.y.z" a Bun-compiled executable names itself with
// in its requests; "" when the file has none.
func bunAgent(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	// The last bytes of each read are kept, so a name across two reads is found.
	const keep = 32
	buf := make([]byte, keep+1<<20)
	kept := 0
	for {
		n, err := io.ReadFull(f, buf[kept:])
		chunk := buf[:kept+n]
		if m := bunAgentPattern.Find(chunk); m != nil {
			return string(m)
		}
		if err != nil {
			return ""
		}
		kept = copy(buf, chunk[len(chunk)-keep:])
	}
}

// Highest is the fullest window of the pool still running; ok is false when
// none is.
func (p UsagePool) Highest(now time.Time) (w UsageWindow, ok bool) {
	for _, x := range []UsageWindow{p.FiveHour, p.Weekly, p.Monthly} {
		if x.Active(now) && (!ok || x.Percent > w.Percent) {
			w, ok = x, true
		}
	}
	return w, ok
}
