package keys

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/netproxy"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/contentenc"
)

type KeyUsage struct {
	Key           string
	Used          *float64 // effective used percent for rotation
	Total         *float64 // always 100 (percentage-based)
	Expires       *string  // nearest standard windowEnd for display
	ExpiresTsMs   *float64 // nearest standard windowEnd in ms for sorting
	LastCheckedAt time.Time
	Error         string

	// LLMHTTPError and LLMErrorAt record a runtime error observed on the LLM
	// endpoint (set by MarkKeyHTTPError), as opposed to Error which records a
	// billing/limits probe error. These are preserved across the usage refresh
	// loop so that a key the LLM endpoint rejects (e.g. a transient 403) is kept
	// out of rotation for a cooldown even when the billing endpoint still
	// returns 200 for it.
	LLMHTTPError string    `json:"-"`
	LLMErrorAt   time.Time `json:"-"`

	// Droid Core and overage fields for display
	StandardPercent   *float64
	StandardWindowEnd *string
	CorePercent       *float64
	CoreWindowEnd     *string
	OveragePreference string
	ExtraUsageBalance *float64
	LegacyBilling     bool // true when falling back to chat-usage endpoint

	// StandardLimits/CoreLimits keep the per-window (5h / weekly / monthly)
	// breakdown behind StandardPercent/CorePercent so the UI can show which
	// window is limiting and when each one resets.
	StandardLimits *PoolLimits
	CoreLimits     *PoolLimits

	// StandardResetMs/CoreResetMs are the real reset timestamps (ms) of the
	// exhausted buckets in each pool. Used to cool a (key, pool) pair until the
	// upstream quota actually resets instead of for a fixed window.
	StandardResetMs float64 `json:"-"`
	CoreResetMs     float64 `json:"-"`

	// StandardPoolCooldownUntil/CorePoolCooldownUntil record a runtime capacity
	// error (402/429) scoped to one billing pool. A Standard-only model must
	// skip the key until the Standard cooldown elapses even if the key's Core
	// pool is still healthy (and vice versa). Unlike Error these are preserved
	// across the billing refresh loop.
	StandardPoolCooldownUntil time.Time `json:"-"`
	CorePoolCooldownUntil     time.Time `json:"-"`

	// Absolute token values (legacy billing only, for display)
	StandardUsedTokens  *float64
	StandardTotalTokens *float64

	// Internal: raw response body (not serialized, not for display)
	RawBody []byte `json:"-"`
}

func (u *KeyUsage) UsedRatio() float64 {
	if u.Total == nil || u.Used == nil || *u.Total <= 0 {
		return 1
	}
	return *u.Used / *u.Total
}

func (u *KeyUsage) Remaining() float64 {
	if u.Total == nil || u.Used == nil {
		return 0
	}
	return *u.Total - *u.Used
}

// StandardAvailable reports whether the key can still serve Standard-pool
// traffic. A key whose Standard pool is exhausted must not take Standard-only
// models even when its Core pool is healthy, otherwise the request 402s and the
// client (droid) reacts by switching the session to a Droid Core model.
func (u *KeyUsage) StandardAvailable() bool {
	if u.StandardPercent != nil {
		return *u.StandardPercent < 100
	}
	// No pool breakdown (legacy chat-usage billing): fall back to the effective
	// usage so pre-existing callers keep working.
	if u.Used != nil && u.Total != nil {
		return *u.Used < 100
	}
	return false
}

// CoreAvailable reports whether the key can still serve Core-pool traffic.
func (u *KeyUsage) CoreAvailable() bool {
	if u.CorePercent != nil {
		return *u.CorePercent < 100
	}
	return false
}

// usablePools returns, in preference order, the billing pools this key can
// serve for a request aimed at requestPool. Core-capable requests list a
// Standard-healthy pool first with Core as a same-key fallback, so a
// Standard-only cooldown does not remove the key from Core rotation; the
// cross-key preference between Core-billing and Standard-billing keys lives
// in poolTier. A Standard-only request never lists Core.
func (u *KeyUsage) usablePools(requestPool string) []string {
	stdOK := u.StandardAvailable()
	coreOK := u.CoreAvailable()
	switch requestPool {
	case PoolStandard:
		if stdOK {
			return []string{PoolStandard}
		}
		return nil
	case PoolCore, PoolAny:
		var pools []string
		if stdOK {
			pools = append(pools, PoolStandard)
		}
		if coreOK {
			pools = append(pools, PoolCore)
		}
		return pools
	default:
		return nil
	}
}

// usablePool returns the preferred pool this key can serve, or "" when none.
func (u *KeyUsage) usablePool(requestPool string) string {
	pools := u.usablePools(requestPool)
	if len(pools) == 0 {
		return ""
	}
	return pools[0]
}

// usablePoolFor returns the first pool the key can serve for requestPool that is
// not under a whole-key or pool-scoped cooldown, or "" when none.
func (r *KeyRotator) usablePoolFor(usage *KeyUsage, requestPool string) string {
	for _, pool := range usage.usablePools(requestPool) {
		if !r.isPoolCooledDown(usage, pool) {
			return pool
		}
	}
	return ""
}

// poolPercent returns the used percentage of the given pool and whether the
// value is known.
func (u *KeyUsage) poolPercent(pool string) (float64, bool) {
	if pool == PoolCore {
		if u.CorePercent != nil {
			return *u.CorePercent, true
		}
		return 0, false
	}
	if u.StandardPercent != nil {
		return *u.StandardPercent, true
	}
	if u.Used != nil {
		return *u.Used, true
	}
	return 0, false
}

func (u *KeyUsage) poolRatio(pool string) float64 {
	percent, ok := u.poolPercent(pool)
	if !ok {
		return 1
	}
	return percent / 100
}

func (u *KeyUsage) poolRemaining(pool string) float64 {
	percent, ok := u.poolPercent(pool)
	if !ok {
		return 0
	}
	return 100 - percent
}

// poolLimitsFor returns the per-window breakdown of the given pool, or nil
// when the billing probe did not report one.
func (u *KeyUsage) poolLimitsFor(pool string) *PoolLimits {
	if pool == PoolCore {
		return u.CoreLimits
	}
	return u.StandardLimits
}

// bucketBurn is the bucket's consumption rate relative to the even pace that
// would land on 100% exactly at window close: 1.0 is on pace, 2.0 would
// exhaust the bucket halfway through. Buckets without a live window burn 0.
// Mirrors dproxy-apps' bucketBurn.
func bucketBurn(bucket *BucketLimit, nominalHours float64, now time.Time) float64 {
	if bucket == nil || bucket.WindowEnd == "" {
		return 0
	}
	end, err := time.Parse(time.RFC3339, bucket.WindowEnd)
	if err != nil {
		return 0
	}
	hoursLeft := end.Sub(now).Hours()
	if hoursLeft <= 0 {
		return 0
	}
	elapsed := nominalHours - hoursLeft
	if elapsed < minBurnElapsedHours {
		elapsed = minBurnElapsedHours
	}
	return (bucket.UsedPercent / 100) / (elapsed / nominalHours)
}

// weeklyMonthlyShare is how much of the monthly window one full weekly
// window lets through. Fitted from the request log in 2026-10: the same
// traffic moves the weekly window twice as far as the monthly one.
const weeklyMonthlyShare = 0.5

// quotaDeadlineMs is when this pool's leftover quota is lost. That is the end
// of the monthly window: an unused week only loses quota when the weeks left
// after it cannot carry the monthly remainder, so the weekly end wins only
// then. With no monthly window the weekly end is used. The 5-hour window is
// left out: it ends within hours of any use. Ends already past are skipped,
// so stale usage data never pins a key to the front. 0 means no deadline is
// known.
func quotaDeadlineMs(limits *PoolLimits, now time.Time) float64 {
	if limits == nil {
		return 0
	}
	end := func(b *BucketLimit) (time.Time, bool) {
		if b == nil || b.WindowEnd == "" || b.UsedPercent >= 100 {
			return time.Time{}, false
		}
		t, err := time.Parse(time.RFC3339, b.WindowEnd)
		return t, err == nil && t.After(now)
	}
	weekly, wok := end(limits.Weekly)
	monthly, mok := end(limits.Monthly)
	var deadline time.Time
	switch {
	case mok && wok && weekly.Before(monthly) && monthlyAtRisk(limits.Monthly.UsedPercent, weekly, monthly):
		deadline = weekly
	case mok:
		deadline = monthly
	case wok:
		deadline = weekly
	default:
		return 0
	}
	return float64(deadline.UnixMilli())
}

// monthlyAtRisk reports whether the weekly windows that can open between
// weekEnd and monthEnd hold less than the monthly remainder.
func monthlyAtRisk(monthlyUsed float64, weekEnd, monthEnd time.Time) bool {
	weeks := math.Ceil(monthEnd.Sub(weekEnd).Hours() / weeklyWindowHours)
	return 100-monthlyUsed > weeks*weeklyMonthlyShare*100
}

// poolResetMs returns the real reset timestamp (ms) of the given pool when it
// is exhausted, or 0 if unknown.
func (u *KeyUsage) poolResetMs(pool string) float64 {
	if pool == PoolCore {
		return u.CoreResetMs
	}
	return u.StandardResetMs
}

// poolCooldown returns the runtime capacity cooldown for the given pool. Pools
// other than Standard/Core have no scoped cooldown.
func (u *KeyUsage) poolCooldown(pool string) time.Time {
	switch pool {
	case PoolCore:
		return u.CorePoolCooldownUntil
	case PoolStandard:
		return u.StandardPoolCooldownUntil
	default:
		return time.Time{}
	}
}

func (u *KeyUsage) setPoolCooldown(pool string, until time.Time) {
	switch pool {
	case PoolCore:
		u.CorePoolCooldownUntil = until
	case PoolStandard:
		u.StandardPoolCooldownUntil = until
	}
}

const (
	// fiveHourSpillRatio: a pool whose 5-hour bucket is past this ratio is hot
	// and spills new traffic to cooler keys, even across the Core/Standard
	// tier preference. Mirrors dproxy-apps' FiveHourSpillRatio.
	fiveHourSpillRatio = 0.9
	// coreConcentrationPaceCap bounds how fast a key may burn its weekly
	// Standard bucket (relative to the even pace, see bucketBurn) while
	// Core-capable traffic is being concentrated on it; a key burning faster
	// yields to the rest of the pool. Mirrors dproxy-apps'
	// CoreConcentrationPaceCap.
	coreConcentrationPaceCap = 1.6
	weeklyWindowHours        = 7 * 24
	// minBurnElapsedHours floors the elapsed window time so the first hours of
	// a fresh window do not report a huge burn rate off a tiny sample.
	minBurnElapsedHours = 12
)

type candidate struct {
	key         string
	expiresTsMs float64 // raw milliseconds for precise comparison
	usedRatio   float64
	remaining   float64
	total       float64
	pool        string  // billing pool this key would serve (standard or core)
	tier        int     // preference rank of pool for this request (0 = preferred)
	fiveHour    float64 // used ratio of the pool's 5-hour bucket
	weeklyBurn  float64 // burn rate of the pool's weekly bucket relative to even pace
	// concentrate marks a Core-capable request landing on Standard quota, the
	// only case where draining one key early unlocks extra (Core) quota.
	concentrate bool
	// unopened: serving this request would start the key's Standard month.
	unopened     bool
	waitingSince time.Time
	order        int // index in configuration order
}

func (c candidate) hot() bool { return c.fiveHour >= fiveHourSpillRatio }

// paced reports whether this candidate is a concentration target whose weekly
// burn is still under the pace cap. Candidates over the cap fall back to
// deadline balancing.
func (c candidate) paced() bool { return c.concentrate && c.weeklyBurn <= coreConcentrationPaceCap }

type KeyRotator struct {
	cfg                  *config.Config
	keys                 []string
	usages               map[string]*KeyUsage
	mu                   sync.RWMutex
	activeKey            atomic.Pointer[string]
	usageRefreshDisabled atomic.Bool
	// usageProbed is set once a probe round has stored usage. After that an
	// all-401/403 round is treated as an upstream blip, not as keys that
	// cannot read billing.
	usageProbed atomic.Bool
	client      *http.Client
	clock       func() time.Time
	stopCh      chan struct{}
	usageLog    UsageLineWriter
	// openMu guards openedAt and serializes month openings.
	openMu sync.Mutex
	// openedAt is when a request was last routed to a key whose month was
	// not running.
	openedAt map[string]time.Time
	// scheduledOpening: a scheduled opener starts the planned months.
	scheduledOpening atomic.Bool
	// clientVersion and clientUA are droid's X-Client-Version and User-Agent
	// as last seen in its own traffic (see client_headers.go).
	clientVersion atomic.Value
	clientUA      atomic.Value
	versionOnce   sync.Once
	// orgMu guards orgIDs, each key's own org from whoami.
	orgMu  sync.Mutex
	orgIDs map[string]orgLookup
	// lastActivity and lastRound are UnixNano times of the last LLM request
	// and the last full usage round; wake and recheck feed the refresh loop.
	lastActivity atomic.Int64
	lastRound    atomic.Int64
	wake         chan struct{}
	recheck      chan string
	recheckMu    sync.Mutex
	recheckAt    map[string]time.Time
	// lastFullRound is when every key was last read; usedKeys are the keys
	// handed out for requests since the last round (see refresh.go).
	lastFullRound atomic.Int64
	usedMu        sync.Mutex
	usedKeys      map[string]struct{}
}

func NewKeyRotator(cfg *config.Config, keys []string) *KeyRotator {
	r := &KeyRotator{
		cfg:      cfg,
		keys:     keys,
		usages:   make(map[string]*KeyUsage),
		client:   &http.Client{Transport: netproxy.Transport(), Timeout: time.Duration(config.UsageFetchTimeoutSec) * time.Second},
		stopCh:   make(chan struct{}),
		openedAt: make(map[string]time.Time),
		wake:     make(chan struct{}, 1),
		recheck:  make(chan string, 64),
	}
	return r
}

func (r *KeyRotator) FetchAllUsages(ctx context.Context) {
	if r.usageRefreshDisabled.Load() {
		r.reselectActiveKey()
		return
	}
	r.lastFullRound.Store(r.nowTime().UnixNano())
	r.takeUsedKeys()

	fetched := r.ProbeAllUsages(ctx)
	blocked := 0
	allInvalidAuth := len(fetched) > 0
	for _, usage := range fetched {
		if usage.Error == ErrUpstreamBlocked {
			blocked++
			continue
		}
		if !IsInvalidKeyError(usage.Error) {
			allInvalidAuth = false
		}
	}

	if allInvalidAuth {
		if r.usageProbed.Load() || blocked > 0 {
			log.Printf("usage_probe_all_invalid keys=%d blocked=%d; keeping the last usage and retrying", len(fetched), blocked)
			// The 403s can stick to a pooled connection; redial next round.
			r.client.CloseIdleConnections()
		} else {
			r.usageRefreshDisabled.Store(true)
		}
		r.reselectActiveKey()
		return
	}

	r.mu.Lock()
	snapshots := r.changedSnapshotsLocked(fetched)
	for _, usage := range fetched {
		if usage.Error == ErrUpstreamBlocked {
			continue
		}
		r.storeUsageLocked(usage)
	}
	logTo := r.usageLog
	r.mu.Unlock()
	r.usageProbed.Store(true)
	r.lastRound.Store(r.nowTime().UnixNano())
	writeSnapshots(logTo, snapshots)
	r.reselectActiveKey()
}

// storeUsageLocked replaces a key's usage with a fresh probe. It keeps the
// runtime LLM error mark and pool cooldowns: the billing/limits endpoint can
// return 200 for a key the LLM endpoint still rejects, so a fresh probe must
// not clear a cooldown set by MarkKeyHTTPError or MarkKeyPoolError.
func (r *KeyRotator) storeUsageLocked(usage *KeyUsage) {
	if existing, ok := r.usages[usage.Key]; ok {
		usage.LLMHTTPError = existing.LLMHTTPError
		usage.LLMErrorAt = existing.LLMErrorAt
		usage.StandardPoolCooldownUntil = existing.StandardPoolCooldownUntil
		usage.CorePoolCooldownUntil = existing.CorePoolCooldownUntil
	}
	r.usages[usage.Key] = usage
}

func (r *KeyRotator) ProbeAllUsages(ctx context.Context) []*KeyUsage {
	r.mu.RLock()
	keyList := append([]string(nil), r.keys...)
	r.mu.RUnlock()
	return r.probeKeys(ctx, keyList)
}

func (r *KeyRotator) probeKeys(ctx context.Context, keyList []string) []*KeyUsage {
	var wg sync.WaitGroup
	sem := make(chan struct{}, config.UsageFetchConcurrency)
	results := make(chan *KeyUsage, len(keyList))

	for _, key := range keyList {
		wg.Add(1)
		sem <- struct{}{}
		go func(k string) {
			defer wg.Done()
			defer func() { <-sem }()
			results <- r.fetchKeyUsage(ctx, k)
		}(key)
	}
	wg.Wait()
	close(results)

	byKey := make(map[string]*KeyUsage, len(keyList))
	for usage := range results {
		byKey[usage.Key] = usage
	}
	ordered := make([]*KeyUsage, 0, len(keyList))
	for _, key := range keyList {
		if usage, ok := byKey[key]; ok {
			ordered = append(ordered, usage)
		}
	}
	return ordered
}

// StartRefreshLoop re-reads usage the way droid does: about once a minute
// while LLM requests flow, and while idle only the keys whose check failed. Each round reads only the
// keys that can have changed (see refresh.go). Activity after an idle spell
// gets a round at once, and a key that hits a capacity error is re-read on its
// own (see NoteLLMActivity and RecheckKey).
func (r *KeyRotator) StartRefreshLoop(ctx context.Context) {
	if r.usageRefreshDisabled.Load() {
		return
	}
	interval := time.Duration(config.UsageRefreshIntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if r.recentlyActive() {
					r.refreshRound(ctx)
				} else {
					// Idle, nothing else can change; a failed check is still
					// worth another try.
					r.refreshKeys(ctx, r.failedKeys())
				}
			case <-r.wake:
				r.refreshRound(ctx)
				ticker.Reset(interval)
			case key := <-r.recheck:
				r.RefreshKeyUsage(ctx, key)
			case <-r.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// NoteLLMActivity records an LLM request. When the last full round is older
// than two intervals (the proxy was idle), it asks the refresh loop for one now, so
// the request after an idle spell is not routed on hours-old usage.
func (r *KeyRotator) NoteLLMActivity(t time.Time) {
	r.lastActivity.Store(t.UnixNano())
	stale := 2 * time.Duration(config.UsageRefreshIntervalSec) * time.Second
	if t.Sub(time.Unix(0, r.lastRound.Load())) <= stale {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *KeyRotator) recentlyActive() bool {
	last := r.lastActivity.Load()
	return last != 0 && r.nowTime().Sub(time.Unix(0, last)) <= time.Duration(config.UsageActiveWindowSec)*time.Second
}

// RecheckKey asks the refresh loop to re-read one key, at most once per
// UsageRecheckMinSec per key; without a running loop it does nothing.
func (r *KeyRotator) RecheckKey(key string) {
	if key == "" {
		return
	}
	now := r.nowTime()
	r.recheckMu.Lock()
	if last, ok := r.recheckAt[key]; ok && now.Sub(last) < time.Duration(config.UsageRecheckMinSec)*time.Second {
		r.recheckMu.Unlock()
		return
	}
	if r.recheckAt == nil {
		r.recheckAt = make(map[string]time.Time)
	}
	r.recheckAt[key] = now
	r.recheckMu.Unlock()
	select {
	case r.recheck <- key:
	default:
	}
}

// RefreshKeyUsage re-reads one key's usage and stores it like a full round.
func (r *KeyRotator) RefreshKeyUsage(ctx context.Context, key string) {
	r.refreshKeys(ctx, []string{key})
}

func (r *KeyRotator) Stop() {
	close(r.stopCh)
}

func (r *KeyRotator) UsageRefreshDisabled() bool {
	return r.usageRefreshDisabled.Load()
}

func (r *KeyRotator) GetActiveKey() string {
	if p := r.activeKey.Load(); p != nil {
		return *p
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.keys) > 0 {
		return r.keys[0]
	}
	return ""
}

// AddKey probes key against the billing endpoint and, when the probe
// succeeds, appends it to the rotation. It returns an error when the key is
// already configured or the upstream rejects it.
func (r *KeyRotator) AddKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("empty key")
	}
	r.mu.RLock()
	for _, k := range r.keys {
		if k == key {
			r.mu.RUnlock()
			return fmt.Errorf("key already configured")
		}
	}
	r.mu.RUnlock()

	usage := r.fetchKeyUsage(ctx, key)
	if usage.Error != "" {
		return fmt.Errorf("key probe failed: %s", usage.Error)
	}

	r.mu.Lock()
	r.keys = append(r.keys, key)
	r.usages[key] = usage
	r.mu.Unlock()
	r.reselectActiveKey()
	return nil
}

// RemoveKey takes key out of the rotation.
func (r *KeyRotator) RemoveKey(key string) {
	r.mu.Lock()
	kept := r.keys[:0:0]
	for _, k := range r.keys {
		if k != key {
			kept = append(kept, k)
		}
	}
	r.keys = kept
	delete(r.usages, key)
	r.mu.Unlock()
	if p := r.activeKey.Load(); p != nil && *p == key {
		r.activeKey.Store(nil)
	}
	r.reselectActiveKey()
}

// Keys are the keys in the rotation, in the order they were added.
func (r *KeyRotator) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.keys...)
}

// SetActiveKeyForTest sets the active key directly (for testing only).
func (r *KeyRotator) SetActiveKeyForTest(key string) {
	r.activeKey.Store(&key)
}

// SetClockForTest overrides the time source used for cooldown calculations.
func (r *KeyRotator) SetClockForTest(clock func() time.Time) {
	r.clock = clock
}

// SetUsageForTest seeds a usage entry directly (for testing only).
func (r *KeyRotator) SetUsageForTest(usage *KeyUsage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.usages == nil {
		r.usages = make(map[string]*KeyUsage)
	}
	r.usages[usage.Key] = usage
}

// UsageForTest returns the usage entry for key (for testing only).
func (r *KeyRotator) UsageForTest(key string) *KeyUsage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.usages[key]
}

func (r *KeyRotator) nowTime() time.Time {
	if r.clock != nil {
		return r.clock()
	}
	return time.Now()
}

// llmCooldown returns how long a runtime LLM error keeps a key out of rotation.
func (r *KeyRotator) llmCooldown() time.Duration {
	secs := config.DefaultLLMErrorCooldownSec
	if r.cfg != nil && r.cfg.LLMErrorCooldownSec > 0 {
		secs = r.cfg.LLMErrorCooldownSec
	}
	return time.Duration(secs) * time.Second
}

// LLMCooldownSeconds exposes the configured runtime error cooldown for
// diagnostics.
func (r *KeyRotator) LLMCooldownSeconds() int {
	return int(r.llmCooldown() / time.Second)
}

// isLLMCooledDown reports whether the key is still within the runtime LLM error
// cooldown and must be skipped by the rotator.
func (r *KeyRotator) isLLMCooledDown(usage *KeyUsage) bool {
	if usage == nil || usage.LLMHTTPError == "" {
		return false
	}
	return r.nowTime().Sub(usage.LLMErrorAt) < r.llmCooldown()
}

// isPoolCooledDown reports whether the key is under a whole-key runtime error
// cooldown or a capacity cooldown scoped to the given billing pool.
func (r *KeyRotator) isPoolCooledDown(usage *KeyUsage, pool string) bool {
	if r.isLLMCooledDown(usage) {
		return true
	}
	if usage == nil {
		return false
	}
	until := usage.poolCooldown(pool)
	if until.IsZero() {
		return false
	}
	return r.nowTime().Before(until)
}

// ResolveKeyForRequest keeps the original model-agnostic behaviour: it prefers
// the presented key and rotates only on exhaustion, cooldown or an expiring
// month (see expiringKeyFor). Retained for
// callers that do not know the model; LLM requests should use SelectKeyForModel.
func (r *KeyRotator) ResolveKeyForRequest(presentedKey string) string {
	key, _ := r.SelectKeyForModel(presentedKey, "", nil)
	return key
}

// SelectKeyForModel chooses the key for one LLM request and returns the key
// together with the billing pool it will serve. The model's billing pool gates
// selection so that Standard-only models never land on a key whose Standard
// pool is exhausted (which would return a 402 and make droid switch the session
// to a Droid Core model), while Core-capable models may fall back to Core.
func (r *KeyRotator) SelectKeyForModel(presentedKey, modelID string, excluded map[string]struct{}) (string, string) {
	key, pool := r.selectKeyForModel(presentedKey, modelID, excluded, openRoute)
	r.noteKeyUsed(key)
	return key, pool
}

// PreviewKeyForModel is the key the next request for modelID would get,
// without counting it as a request: it never records a month opening.
func (r *KeyRotator) PreviewKeyForModel(modelID string) (string, string) {
	return r.selectKeyForModel("", modelID, nil, openPreview)
}

func (r *KeyRotator) selectKeyForModel(presentedKey, modelID string, excluded map[string]struct{}, mode openMode) (string, string) {
	pool := ModelBillingPool(modelID)

	if presentedKey != "" {
		if _, skip := excluded[presentedKey]; !skip {
			r.mu.RLock()
			usage := r.usages[presentedKey]
			r.mu.RUnlock()
			if usage != nil && usage.Error == "" {
				if chosen := r.usablePoolFor(usage, pool); chosen != "" {
					if usage.poolRatio(chosen) < config.SpilloverThreshold && usage.poolRemaining(chosen) > 0 {
						if expiring := r.expiringKeyFor(presentedKey, usage, chosen, pool, excluded); expiring != "" {
							return expiring, PoolStandard
						}
						return presentedKey, chosen
					}
				}
			}
		}
	}

	if key, keyPool := r.selectActiveKeyForPool(pool, excluded, mode); key != "" {
		return key, keyPool
	}

	// While retrying, an empty result means every key has already been attempted;
	// do not fall back to a key the request has already used.
	if len(excluded) > 0 {
		return "", pool
	}

	active := r.GetActiveKey()
	if active != "" {
		return active, pool
	}
	if presentedKey != "" {
		return presentedKey, pool
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.keys) > 0 {
		return r.keys[0], pool
	}
	return "", pool
}

// ExpiringWithin is how soon a Standard month has to end for its key to be
// expiring: the Key Pool moves droid onto it, and the Usage page lists it.
const ExpiringWithin = 7 * 24 * time.Hour

// expiringKeyFor is the key droid should leave the presented key for, or "":
// the routable key whose Standard month ends first within ExpiringWithin and
// before the presented key's. Droid otherwise stays on the key it sent until
// that one runs out, while another month ends and loses what is left of it. A
// key with a nearly full 5-hour bucket is passed over, so droid comes back to
// it once the bucket resets. A presented key that serves Core stays.
func (r *KeyRotator) expiringKeyFor(presented string, presentedUsage *KeyUsage, presentedPool, requestPool string, excluded map[string]struct{}) string {
	if presentedPool != PoolStandard {
		return ""
	}
	now := r.nowTime()
	limit := now.Add(ExpiringWithin)
	if end, ok := presentedUsage.standardMonthEnd(); ok && end.After(now) && end.Before(limit) {
		limit = end
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.openMu.Lock()
	defer r.openMu.Unlock()
	best := ""
	for _, c := range r.buildCandidatesForPool(requestPool, excluded) {
		if c.key == presented || c.pool != PoolStandard || c.hot() || c.unopened || c.usedRatio >= config.SpilloverThreshold {
			continue
		}
		end, ok := r.usages[c.key].standardMonthEnd()
		if ok && end.After(now) && end.Before(limit) {
			best, limit = c.key, end
		}
	}
	return best
}

func (r *KeyRotator) MarkKeyError(key string) {
	r.MarkKeyHTTPError(key, http.StatusUnauthorized)
}

func (r *KeyRotator) MarkKeyHTTPError(key string, statusCode int) {
	label := fmt.Sprintf("http_%d", statusCode)
	now := r.nowTime()
	r.mu.Lock()
	if usage, ok := r.usages[key]; ok {
		usage.LLMHTTPError = label
		usage.LLMErrorAt = now
	} else if key != "" {
		r.usages[key] = &KeyUsage{Key: key, LLMHTTPError: label, LLMErrorAt: now, LastCheckedAt: now}
	}
	r.mu.Unlock()
	r.reselectActiveKey()
}

// MarkKeyPoolError records a capacity error (402/429) against a single billing
// pool so only that (key, pool) pair is taken out of rotation. Unlike
// MarkKeyHTTPError this leaves the key's other pool usable: an exhausted
// Standard pool must not disable an otherwise healthy Core pool, and vice
// versa. The cooldown runs until the pool's real reset when known.
func (r *KeyRotator) MarkKeyPoolError(key, pool string, statusCode int) {
	if key == "" || (pool != PoolStandard && pool != PoolCore) {
		return
	}
	now := r.nowTime()
	until := now.Add(r.llmCooldown())

	r.mu.Lock()
	usage, ok := r.usages[key]
	if !ok {
		usage = &KeyUsage{Key: key, LastCheckedAt: now}
		r.usages[key] = usage
	}
	if reset := usage.poolResetMs(pool); reset > 0 {
		if t := time.UnixMilli(int64(reset)); t.After(until) {
			until = t
		}
	}
	usage.setPoolCooldown(pool, until)
	usage.LastCheckedAt = now
	r.mu.Unlock()
	r.reselectActiveKey()
	r.RecheckKey(key)
}

func (r *KeyRotator) reselectActiveKey() {
	selected := r.selectActiveKey()
	if selected != "" {
		r.activeKey.Store(&selected)
	}
}

// selectActiveKey is the pool-agnostic entry point used by the refresh loop to
// keep the sticky "active" key. Model-aware callers use selectActiveKeyForPool.
func (r *KeyRotator) selectActiveKey() string {
	key, _ := r.selectActiveKeyForPool(PoolAny, nil, openNone)
	return key
}

// selectActiveKeyForPool returns the best key that can serve requestPool and the
// billing pool that key will serve. A key whose month is not running is
// chosen first while today's openings are below config.DailyKeyOpens (unless
// mode is openNone), otherwise only when no open key has room; see opening.go.
func (r *KeyRotator) selectActiveKeyForPool(requestPool string, excluded map[string]struct{}, mode openMode) (string, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.openMu.Lock()
	defer r.openMu.Unlock()

	candidates := r.buildCandidatesForPool(requestPool, excluded)
	if len(candidates) == 0 {
		return r.firstUsableKeyLockedForPool(requestPool, excluded), requestPool
	}

	var open, waiting []candidate
	for _, c := range candidates {
		if c.unopened {
			waiting = append(waiting, c)
		} else {
			open = append(open, c)
		}
	}
	sortOpeningQueue(waiting)
	now := r.nowTime()
	opening := func(c candidate) (string, string) {
		if mode == openRoute {
			if r.openedAt == nil {
				r.openedAt = make(map[string]time.Time)
			}
			r.openedAt[c.key] = now
		}
		return c.key, c.pool
	}
	if mode != openNone && !r.scheduledOpening.Load() && len(waiting) > 0 && r.openedTodayLocked(now) < config.DailyKeyOpens {
		return opening(waiting[0])
	}
	for _, c := range open {
		if c.usedRatio < config.SpilloverThreshold {
			return c.key, c.pool
		}
	}
	if len(waiting) > 0 {
		return opening(waiting[0])
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.remaining > best.remaining {
			best = c
		}
	}
	return best.key, best.pool
}

func (r *KeyRotator) firstUsableKeyLockedForPool(requestPool string, excluded map[string]struct{}) string {
	for _, key := range r.keys {
		if _, skip := excluded[key]; skip {
			continue
		}
		if usage, ok := r.usages[key]; ok && (IsInvalidKeyError(usage.Error) || r.isPoolCooledDown(usage, requestPool)) {
			continue
		}
		return key
	}
	// Every remaining key is unusable; fall back to the first key that has not
	// already been attempted this request (never re-try an excluded key).
	for _, key := range r.keys {
		if _, skip := excluded[key]; !skip {
			return key
		}
	}
	return ""
}

// buildCandidatesForPool: caller holds r.mu (read) and r.openMu.
func (r *KeyRotator) buildCandidatesForPool(requestPool string, excluded map[string]struct{}) []candidate {
	var candidates []candidate
	now := r.nowTime()
	for i, key := range r.keys {
		if _, skip := excluded[key]; skip {
			continue
		}
		usage, ok := r.usages[key]
		if !ok || usage.Error != "" {
			continue
		}
		// The pool this key would actually serve for this request. A key whose
		// required pool is exhausted (or missing), or whose only usable pool is
		// cooled down, is not a candidate at all.
		chosen := r.usablePoolFor(usage, requestPool)
		if chosen == "" {
			continue
		}
		percent, known := usage.poolPercent(chosen)
		if !known {
			continue
		}
		remaining := 100 - percent
		if remaining <= 0 {
			continue
		}
		expiresTsMs := math.Inf(1)
		if deadline := quotaDeadlineMs(usage.poolLimitsFor(chosen), r.nowTime()); deadline > 0 {
			expiresTsMs = deadline
		} else if reset := usage.poolResetMs(chosen); reset > 0 {
			expiresTsMs = reset
		} else if usage.ExpiresTsMs != nil {
			expiresTsMs = *usage.ExpiresTsMs
		}
		fiveHour := 0.0
		weeklyBurn := 0.0
		if limits := usage.poolLimitsFor(chosen); limits != nil {
			if limits.FiveHour != nil {
				fiveHour = limits.FiveHour.UsedPercent / 100
			}
			weeklyBurn = bucketBurn(limits.Weekly, weeklyWindowHours, r.nowTime())
		}
		candidates = append(candidates, candidate{
			key:          key,
			expiresTsMs:  expiresTsMs,
			usedRatio:    percent / 100,
			remaining:    remaining,
			total:        100,
			pool:         chosen,
			tier:         poolTier(requestPool, chosen),
			fiveHour:     fiveHour,
			weeklyBurn:   weeklyBurn,
			concentrate:  requestPool == PoolCore && chosen == PoolStandard,
			unopened:     chosen == PoolStandard && r.waitingToOpenLocked(key, usage, now),
			waitingSince: usage.waitingSince(),
			order:        i,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		if ci.tier != cj.tier {
			// Core-before-Standard (tier 0 vs 1 on a Core-capable request) is a
			// preference between two healthy pools, so a nearly full 5-hour
			// bucket still spills across it: a lone Core-billing key must not
			// absorb every Core-capable request while Standard-live peers idle.
			if ci.hot() != cj.hot() {
				return !ci.hot()
			}
			return ci.tier < cj.tier
		}
		if ci.hot() != cj.hot() {
			return !ci.hot()
		}
		if ci.concentrate && cj.concentrate {
			// Concentrate Core-capable traffic on the key furthest into its
			// Standard week (highest burn under the pace cap) so it reaches the
			// weekly cap and spills into Core, while fresh keys rest.
			if ci.paced() != cj.paced() {
				return ci.paced()
			}
			if ci.paced() && ci.weeklyBurn != cj.weeklyBurn {
				return ci.weeklyBurn > cj.weeklyBurn
			}
		}
		if ci.expiresTsMs != cj.expiresTsMs {
			return ci.expiresTsMs < cj.expiresTsMs
		}
		// Same expiry: prefer the MOST used key (DESC) to burn it down first,
		// avoiding oscillation between keys with similar usage.
		if ci.usedRatio != cj.usedRatio {
			return ci.usedRatio > cj.usedRatio
		}
		return ci.remaining < cj.remaining
	})

	return candidates
}

// poolTier ranks the pool a candidate would serve for this request. A
// Core-capable request prefers a key already billing Core (its Standard pool
// is exhausted): Factory bills Standard first and only spills into Core once
// Standard is gone, so spreading Core-capable traffic across Standard-healthy
// keys would drain every key's Standard pool together and leave Core
// unreachable, while concentrating it preserves Standard headroom for
// Standard-only models. For anything else the included Standard pool stays
// the first choice. Mirrors dproxy-apps' tier assignment.
func poolTier(requestPool, pool string) int {
	if requestPool == PoolCore {
		if pool == PoolCore {
			return 0
		}
		return 1
	}
	if pool == PoolCore {
		return 1
	}
	return 0
}

func (r *KeyRotator) fetchKeyUsage(ctx context.Context, key string) *KeyUsage {
	usage := r.fetchJSON(ctx, key, r.cfg.UsageAPIURL)
	if usage.Error != "" {
		return usage
	}

	// Check if this account uses the new token-rate-limits billing
	if len(usage.RawBody) > 0 {
		var probe struct {
			UsesTokenRateLimitsBilling bool `json:"usesTokenRateLimitsBilling"`
		}
		if json.Unmarshal(usage.RawBody, &probe) == nil && !probe.UsesTokenRateLimitsBilling {
			// Legacy billing: fall back to chat-usage endpoint
			if r.cfg.ChatUsageAPIURL != "" {
				fallback := r.fetchJSON(ctx, key, r.cfg.ChatUsageAPIURL)
				if fallback.Error == "" {
					parsed := parseChatUsageResponse(key, fallback.RawBody)
					parsed.LastCheckedAt = usage.LastCheckedAt
					return parsed
				}
				// If fallback also errors, return the original billing/limits response
				if fallback.Error != "" {
					return fallback
				}
			}
			// No chat-usage URL configured: return no_usage_data
			return usage
		}
	}

	return parseUsageResponse(key, usage.RawBody)
}

// fetchJSON is a helper that GETs a URL with the key as Bearer token and
// returns the raw body. If the HTTP call fails, Error is set.
func (r *KeyRotator) fetchJSON(ctx context.Context, key, url string) *KeyUsage {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &KeyUsage{Key: key, LastCheckedAt: time.Now(), Error: err.Error()}
	}
	ua, version := r.clientHeaders()
	req.Header.Set("Authorization", "Bearer "+key)
	if version != "" {
		req.Header.Set("X-Client-Version", version)
	}
	req.Header.Set("X-Factory-Client", factoryClientCLI)
	if org := r.orgID(ctx, key, ua); org != "" {
		req.Header.Set("X-Factory-Org-Id", org)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", contentenc.AcceptEncoding)

	resp, err := r.client.Do(req)
	if err != nil {
		return &KeyUsage{Key: key, LastCheckedAt: time.Now(), Error: "fetch_error"}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.Header.Get("X-Vercel-Mitigated") != "" {
			return &KeyUsage{Key: key, LastCheckedAt: time.Now(), Error: ErrUpstreamBlocked}
		}
		return &KeyUsage{Key: key, LastCheckedAt: time.Now(), Error: fmt.Sprintf("http_%d", resp.StatusCode)}
	}

	reader, closer, ok := contentenc.Decode(resp.Header.Get("Content-Encoding"), resp.Body)
	if closer != nil {
		defer closer.Close()
	}
	if !ok {
		return &KeyUsage{Key: key, LastCheckedAt: time.Now(), Error: "decode_error"}
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return &KeyUsage{Key: key, LastCheckedAt: time.Now(), Error: "read_error"}
	}

	return &KeyUsage{Key: key, LastCheckedAt: time.Now(), RawBody: body}
}

// BucketLimit is one rate-limit window as reported by the billing endpoint.
type BucketLimit struct {
	UsedPercent float64 `json:"usedPercent"`
	WindowEnd   string  `json:"windowEnd"`
}

// PoolLimits is the per-window breakdown of one billing pool.
type PoolLimits struct {
	FiveHour *BucketLimit `json:"fiveHour,omitempty"`
	Weekly   *BucketLimit `json:"weekly,omitempty"`
	Monthly  *BucketLimit `json:"monthly,omitempty"`
}

type bucketLimit = BucketLimit
type poolLimits = PoolLimits

type billingLimitsResponse struct {
	UsesTokenRateLimitsBilling bool     `json:"usesTokenRateLimitsBilling"`
	OveragePreference          string   `json:"overagePreference"`
	ExtraUsageAllowed          bool     `json:"extraUsageAllowed"`
	ExtraUsageBalance          *float64 `json:"extraUsageBalance"`
	Limits                     struct {
		Standard *poolLimits `json:"standard"`
		Core     *poolLimits `json:"core"`
	} `json:"limits"`
}

func parseUsageResponse(key string, body []byte) *KeyUsage {
	result := &KeyUsage{Key: key, LastCheckedAt: time.Now()}

	var raw billingLimitsResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		result.Error = "parse_error"
		return result
	}

	if !raw.UsesTokenRateLimitsBilling {
		return result // Total/Used remain nil → status "no_usage_data"
	}

	result.OveragePreference = raw.OveragePreference
	result.ExtraUsageBalance = raw.ExtraUsageBalance
	result.StandardLimits = raw.Limits.Standard
	result.CoreLimits = raw.Limits.Core

	stdPercent := highestBucketPercent(raw.Limits.Standard)
	corePercent := highestBucketPercent(raw.Limits.Core)

	if stdPercent >= 0 {
		sp := stdPercent
		result.StandardPercent = &sp
		result.StandardWindowEnd = ptrString(highestBucketWindowEnd(raw.Limits.Standard))
	}
	if corePercent >= 0 {
		cp := corePercent
		result.CorePercent = &cp
		result.CoreWindowEnd = ptrString(highestBucketWindowEnd(raw.Limits.Core))
	}

	// Real reset timestamps for exhausted pools, used to time-box pool-scoped
	// cooldowns to the upstream quota reset rather than a fixed window.
	result.StandardResetMs = exhaustedBucketResetMs(raw.Limits.Standard)
	result.CoreResetMs = exhaustedBucketResetMs(raw.Limits.Core)

	if stdPercent < 0 {
		return result // no standard data
	}

	// Effective percent: standard if not exhausted, else core if available
	var effective float64
	if stdPercent < 100 {
		effective = stdPercent
	} else if corePercent >= 0 && corePercent < 100 {
		effective = corePercent
	} else {
		effective = 100
	}

	total := float64(100)
	used := effective
	result.Total = &total
	result.Used = &used

	// Nearest standard windowEnd for sorting/display
	nearestMs := nearestWindowEndMs(raw.Limits.Standard)
	if nearestMs > 0 {
		result.ExpiresTsMs = &nearestMs
		s := time.UnixMilli(int64(nearestMs)).UTC().Format("2006-01-02 15:04")
		result.Expires = &s
	}

	return result
}

// highestBucketPercent returns the highest usedPercent across all buckets,
// or -1 if the pool is nil or has no bucket data.
func highestBucketPercent(pool *poolLimits) float64 {
	if pool == nil {
		return -1
	}
	max := -1.0
	for _, b := range []*bucketLimit{pool.FiveHour, pool.Weekly, pool.Monthly} {
		if b != nil && b.UsedPercent > max {
			max = b.UsedPercent
		}
	}
	return max
}

// highestBucketWindowEnd returns the windowEnd of the bucket with the highest
// usedPercent, or "" if the pool is nil or has no bucket data.
func highestBucketWindowEnd(pool *poolLimits) string {
	if pool == nil {
		return ""
	}
	var maxPercent float64 = -1
	var windowEnd string
	for _, b := range []*bucketLimit{pool.FiveHour, pool.Weekly, pool.Monthly} {
		if b != nil && b.UsedPercent > maxPercent {
			maxPercent = b.UsedPercent
			windowEnd = b.WindowEnd
		}
	}
	return windowEnd
}

// nearestWindowEndMs returns the earliest windowEnd timestamp in milliseconds,
// or 0 if the pool is nil or has no valid timestamps.
func nearestWindowEndMs(pool *poolLimits) float64 {
	if pool == nil {
		return 0
	}
	var nearest int64
	for _, b := range []*bucketLimit{pool.FiveHour, pool.Weekly, pool.Monthly} {
		if b == nil || b.WindowEnd == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, b.WindowEnd)
		if err != nil {
			continue
		}
		ms := t.UnixMilli()
		if nearest == 0 || ms < nearest {
			nearest = ms
		}
	}
	return float64(nearest)
}

// exhaustedBucketResetMs returns the latest windowEnd (ms) among the buckets
// that are currently exhausted (usedPercent >= 100), or 0 when none are. A pool
// blocked by several exhausted buckets stays out of rotation until the last one
// resets.
func exhaustedBucketResetMs(pool *poolLimits) float64 {
	if pool == nil {
		return 0
	}
	var latest int64
	for _, b := range []*bucketLimit{pool.FiveHour, pool.Weekly, pool.Monthly} {
		if b == nil || b.UsedPercent < 100 || b.WindowEnd == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, b.WindowEnd)
		if err != nil {
			continue
		}
		if ms := t.UnixMilli(); ms > latest {
			latest = ms
		}
	}
	return float64(latest)
}

func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// --- Legacy chat-usage parser (fallback for non-token-rate-limits accounts) ---

type chatUsageSection struct {
	TotalAllowance     *float64 `json:"totalAllowance"`
	BasicAllowance     *float64 `json:"basicAllowance"`
	Allowance          *float64 `json:"allowance"`
	OrgTotalTokensUsed *float64 `json:"orgTotalTokensUsed"`
	Used               *float64 `json:"used"`
	TokensUsed         *float64 `json:"tokensUsed"`
	OrgOverageUsed     *float64 `json:"orgOverageUsed"`
	UsedRatio          *float64 `json:"usedRatio"`
}

type chatUsageResponse struct {
	Usage struct {
		EndDate   *float64          `json:"endDate"`
		ExpireAt  *float64          `json:"expire_at"`
		ExpiresAt *float64          `json:"expires_at"`
		Standard  *chatUsageSection `json:"standard"`
		Premium   *chatUsageSection `json:"premium"`
		Total     *chatUsageSection `json:"total"`
		Main      *chatUsageSection `json:"main"`
	} `json:"usage"`
}

func parseChatUsageResponse(key string, body []byte) *KeyUsage {
	result := &KeyUsage{Key: key, LastCheckedAt: time.Now(), LegacyBilling: true}

	var raw chatUsageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		result.Error = "parse_error"
		return result
	}

	u := raw.Usage

	// Find the first section with allowance data
	var total *float64
	var used float64
	var ratio *float64
	for _, sec := range []*chatUsageSection{u.Standard, u.Premium, u.Total, u.Main} {
		if sec == nil {
			continue
		}
		t := firstNotNilPtr(sec.TotalAllowance, sec.BasicAllowance, sec.Allowance)
		if t != nil {
			total = t
			used = ptrVal(sec.OrgTotalTokensUsed) + ptrVal(sec.Used) + ptrVal(sec.TokensUsed) + ptrVal(sec.OrgOverageUsed)
			ratio = sec.UsedRatio
			break
		}
	}

	if total == nil {
		return result // no usage data
	}

	result.Total = total
	v := used
	result.Used = &v
	result.StandardUsedTokens = &v
	result.StandardTotalTokens = total

	// Populate StandardPercent from usedRatio (preferred) or computed
	if ratio != nil {
		sp := *ratio * 100
		result.StandardPercent = &sp
	} else if *total > 0 {
		sp := (used / *total) * 100
		result.StandardPercent = &sp
	}

	// Expiry from endDate/expire_at/expires_at
	expRaw := firstNotNilPtr(u.EndDate, u.ExpireAt, u.ExpiresAt)
	if expRaw != nil && *expRaw > 0 {
		ms := *expRaw
		result.ExpiresTsMs = &ms
		s := time.Unix(int64(ms/1000), 0).UTC().Format("2006-01-02")
		result.Expires = &s
	}

	return result
}

func ptrVal(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func firstNotNilPtr(vals ...*float64) *float64 {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func NormalizeBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[len("bearer "):])
	}
	return ""
}

// ErrUpstreamBlocked marks a usage probe that Vercel's firewall denied
// (X-Vercel-Mitigated). The block is per client IP and says nothing about
// the key, so the probe keeps the key's last usage and retries.
const ErrUpstreamBlocked = "upstream_blocked"

func IsInvalidKeyError(errorStr string) bool {
	return errorStr == "http_401" || errorStr == "http_403"
}
