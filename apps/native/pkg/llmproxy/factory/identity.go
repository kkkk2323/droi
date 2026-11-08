// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: API/API_EU 13-14, base 329, llmBase 333-339, orgRefused 405.

package factory

import (
	"net/http"
	"strings"
)

const (
	// API is Factory's API, where an account outside the EU region is served.
	API = "https://api.factory.ai"
	// APIEU is Factory's API for an organization in its EU region.
	APIEU = "https://api.eu.factory.ai"
)

// Identity is what whoami says of a key: its organization, the region that organization is served from, and the
// organization's own host when it has one (premBaseHostV2).
type Identity struct {
	OrgID        string
	Region       string
	PremBaseHost string
}

// LLMBase is where a model request goes for a key of identity id, given the base the proxy was configured with. An
// organization's own host comes first, then its region's API. A base other than Factory's own API (a test server, or
// a proxy in front of Factory) is kept: the person who configured it chose where requests go.
func LLMBase(configured string, id Identity) string {
	if strings.TrimRight(configured, "/") != API {
		return configured
	}
	if host := strings.TrimRight(id.PremBaseHost, "/"); host != "" {
		if strings.Contains(host, "://") {
			return host
		}
		return "https://" + host
	}
	if id.Region == "eu" {
		return APIEU
	}
	return API
}

// OrgRefused reports Factory's 403 for an X-Factory-Org-Id the key cannot reach ("Requested active organization is
// not accessible by this user"): the identity is stale, and whoami is asked again.
func OrgRefused(status int, body []byte) bool {
	return status == http.StatusForbidden && strings.Contains(strings.ToLower(string(body)), "active organization is not accessible")
}
