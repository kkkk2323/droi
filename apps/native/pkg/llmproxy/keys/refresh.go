package keys

import (
	"context"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

// Only Droi spends the Key Pool, so a key's usage moves only when droid sent
// a request with it or one of its windows ended. A refresh round therefore re-reads just
// those keys; a full round every UsageFullRefreshSec catches the rest (an
// error that cleared, a key spent elsewhere).

// NoteKeyUsed records that a request went out with key, so the next round
// re-reads it.
func (r *KeyRotator) NoteKeyUsed(key string) { r.noteKeyUsed(key) }

func (r *KeyRotator) noteKeyUsed(key string) {
	if key == "" {
		return
	}
	r.usedMu.Lock()
	if r.usedKeys == nil {
		r.usedKeys = make(map[string]struct{})
	}
	r.usedKeys[key] = struct{}{}
	r.usedMu.Unlock()
}

func (r *KeyRotator) takeUsedKeys() map[string]struct{} {
	r.usedMu.Lock()
	defer r.usedMu.Unlock()
	used := r.usedKeys
	r.usedKeys = nil
	return used
}

// refreshRound runs one round of the refresh loop: a full round when the last
// one is UsageFullRefreshSec old, otherwise only the keys that can have
// changed.
func (r *KeyRotator) refreshRound(ctx context.Context) {
	now := r.nowTime()
	if now.Sub(time.Unix(0, r.lastFullRound.Load())) >= time.Duration(config.UsageFullRefreshSec)*time.Second {
		r.FetchAllUsages(ctx)
		return
	}
	r.lastRound.Store(now.UnixNano())
	r.refreshKeys(ctx, r.changedKeys(now))
}

// manualRefreshMin is how fresh a full round must be for RefreshNow to skip
// another: pressing Refresh again and again should not probe every key each
// time.
const manualRefreshMin = 10 * time.Second

// RefreshNow reads every key now, for someone who asked to.
func (r *KeyRotator) RefreshNow(ctx context.Context) {
	if r.nowTime().Sub(time.Unix(0, r.lastFullRound.Load())) < manualRefreshMin {
		return
	}
	r.FetchAllUsages(ctx)
}

// changedKeys is every key used since the last round, every key with a
// window that ended since its last check and every key whose last check
// failed, in key order.
func (r *KeyRotator) changedKeys(now time.Time) []string {
	used := r.takeUsedKeys()
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, k := range r.keys {
		if _, ok := used[k]; ok || windowEndedSinceCheck(r.usages[k], now) || checkFailed(r.usages[k]) {
			out = append(out, k)
		}
	}
	return out
}

// failedKeys is every key whose last check failed, in key order.
func (r *KeyRotator) failedKeys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, k := range r.keys {
		if checkFailed(r.usages[k]) {
			out = append(out, k)
		}
	}
	return out
}

// checkFailed reports a check that says nothing about the key: a network
// error, a 5xx or a body that could not be read. Without a read window or a
// request through the key it would wait for the next full round, half an
// hour of activity away, showing the failure. A rejected key (401, 403)
// stays rejected and is left to the full rounds.
func checkFailed(u *KeyUsage) bool {
	return u != nil && u.Error != "" && !IsInvalidKeyError(u.Error)
}

func windowEndedSinceCheck(u *KeyUsage, now time.Time) bool {
	if u == nil {
		return false
	}
	for _, pool := range []*PoolLimits{u.StandardLimits, u.CoreLimits} {
		if pool == nil {
			continue
		}
		for _, b := range []*BucketLimit{pool.FiveHour, pool.Weekly, pool.Monthly} {
			if b == nil || b.WindowEnd == "" {
				continue
			}
			end, err := time.Parse(time.RFC3339, b.WindowEnd)
			if err == nil && end.After(u.LastCheckedAt) && !end.After(now) {
				return true
			}
		}
	}
	return false
}

// refreshKeys re-reads the given keys and stores them like a full round.
func (r *KeyRotator) refreshKeys(ctx context.Context, keyList []string) {
	if len(keyList) == 0 || r.usageRefreshDisabled.Load() {
		return
	}
	fetched := r.probeKeys(ctx, keyList)
	r.mu.Lock()
	snapshots := r.changedSnapshotsLocked(fetched)
	for _, usage := range fetched {
		if usage.Error != ErrUpstreamBlocked {
			r.storeUsageLocked(usage)
		}
	}
	logTo := r.usageLog
	r.mu.Unlock()
	writeSnapshots(logTo, snapshots)
	r.reselectActiveKey()
}
