package keys

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/contentenc"
)

// A key's plan sets the size of its quota: the billing endpoint counts every
// window in percent of the plan's own allowance. Factory names the plan in
// organization.subscription.factoryTier; the monthly Standard tokens of each
// are droid 1.0's: Pro 20M, Plus 100M, Max 200M, Ultra 2B.
const (
	organizationPath = "/api/organization"
	// planTTL is how long a key's plan stands before it is read again.
	planTTL = 6 * time.Hour
)

// PlanName is how Factory shows tier; "" for a tier Droi does not know.
func PlanName(tier string) string {
	switch tier {
	case "team", "team_annual":
		return "Pro"
	case "pro_plus":
		return "Plus"
	case "max":
		return "Max"
	case "ultra":
		return "Ultra"
	case "teams":
		return "Teams"
	case "business":
		return "Business"
	case "enterprise", "payg":
		return "Enterprise"
	}
	return ""
}

// PlanScale is tier's quota in Pro quotas. A tier whose allowance Droi does
// not know, or none, counts as one Pro.
func PlanScale(tier string) float64 {
	switch tier {
	case "pro_plus":
		return 5
	case "max":
		return 10
	case "ultra":
		return 100
	}
	return 1
}

type planLookup struct {
	tier string
	ok   bool
	at   time.Time
}

// plan is key's tier, read once per planTTL, or after orgRetry when the
// last read failed; a failed read keeps the tier read before.
func (r *KeyRotator) plan(ctx context.Context, key, ua, version, org string) string {
	now := r.nowTime()
	r.orgMu.Lock()
	got, seen := r.plans[key]
	r.orgMu.Unlock()
	if seen && (got.ok && now.Sub(got.at) < planTTL || !got.ok && now.Sub(got.at) < orgRetry) {
		return got.tier
	}
	tier, ok := r.fetchPlan(ctx, key, ua, version, org)
	if !ok {
		tier = got.tier
	}
	r.orgMu.Lock()
	if r.plans == nil {
		r.plans = make(map[string]planLookup)
	}
	r.plans[key] = planLookup{tier: tier, ok: ok, at: now}
	r.orgMu.Unlock()
	return tier
}

// fetchPlan asks the organization on the billing endpoint's host, with the
// headers droid sends.
func (r *KeyRotator) fetchPlan(ctx context.Context, key, ua, version, org string) (string, bool) {
	u, err := url.Parse(r.cfg.UsageAPIURL)
	if err != nil || u.Host == "" {
		return "", false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Scheme+"://"+u.Host+organizationPath, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if version != "" {
		req.Header.Set("X-Client-Version", version)
	}
	req.Header.Set("X-Factory-Client", factoryClientCLI)
	if org != "" {
		req.Header.Set("X-Factory-Org-Id", org)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", contentenc.AcceptEncoding)
	resp, err := r.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	body, closer, ok := contentenc.Decode(resp.Header.Get("Content-Encoding"), resp.Body)
	if closer != nil {
		defer closer.Close()
	}
	if !ok {
		return "", false
	}
	var v struct {
		Organization *struct {
			Subscription *struct {
				FactoryTier string `json:"factoryTier"`
			} `json:"subscription"`
		} `json:"organization"`
	}
	if json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&v) != nil || v.Organization == nil {
		return "", false
	}
	if v.Organization.Subscription == nil {
		return "", true
	}
	return v.Organization.Subscription.FactoryTier, true
}
