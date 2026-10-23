// Package config holds the settings of Droi's LLM proxy (ADR 0027), the
// observing proxy ported from droid-proxy. The Host fills a Config with
// paths in Droi's own data folder; nothing here reads droid-proxy's files.
package config

const (
	DefaultUpstreamBaseURL  = "https://api.factory.ai"
	DefaultUsageAPIURL      = "https://api.factory.ai/api/billing/limits"
	DefaultChatUsageAPIURL  = "https://app.factory.ai/api/organization/members/chat-usage"
	UsageFetchConcurrency   = 6
	UsageFetchTimeoutSec    = 8
	UsageRefreshIntervalSec = 60
	// UsageActiveWindowSec is how long after the last LLM request the usage
	// refresh keeps running; after that the proxy is idle and stops polling.
	UsageActiveWindowSec = 300
	// UsageFullRefreshSec is how often an active refresh loop reads every key;
	// the rounds between read only the keys that can have changed.
	UsageFullRefreshSec = 1800
	// UsageRecheckMinSec spaces the single-key re-reads after a 402/429.
	UsageRecheckMinSec = 30
	SpilloverThreshold = 0.98

	// DailyKeyOpens is how many keys' Standard months the rotator starts per
	// local day before it would rather wait, so the months end on different
	// days.
	DailyKeyOpens = 2

	// DefaultLLMErrorCooldownSec is how long a key that received an HTTP 4xx
	// (401/403) from the upstream LLM endpoint is kept out of rotation. The
	// billing/limits refresh can answer 200 for a key the LLM endpoint still
	// rejects; without a cooldown the key would be chosen again at once.
	DefaultLLMErrorCooldownSec = 120
)

// Config is what the rotator and the proxy read.
type Config struct {
	UpstreamBaseURL     string
	UsageAPIURL         string
	ChatUsageAPIURL     string
	LLMErrorCooldownSec int

	CacheWarmingMode        string
	CacheWarmingMinTokens   int
	CacheWarmingIdleMaxMin  int
	CacheWarmingMaxSessions int
	CacheWarmingMaxTokens   int
	// CacheWarmingModeFile keeps the mode chosen while Droi runs.
	CacheWarmingModeFile string
}

// New is a Config with the defaults; the caller sets the files.
func New() *Config {
	return &Config{
		UpstreamBaseURL:     DefaultUpstreamBaseURL,
		UsageAPIURL:         DefaultUsageAPIURL,
		ChatUsageAPIURL:     DefaultChatUsageAPIURL,
		LLMErrorCooldownSec: DefaultLLMErrorCooldownSec,
		// Cache warming replays a Session's last Anthropic request before its
		// prompt cache expires.
		CacheWarmingMode:        "off",
		CacheWarmingMinTokens:   50000,
		CacheWarmingIdleMaxMin:  30,
		CacheWarmingMaxSessions: 4,
		CacheWarmingMaxTokens:   1,
	}
}
