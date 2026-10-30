package keys

import "time"

// KeyState is a display-safe snapshot of one key's routing state, exposed over
// the local debug endpoint. It never contains a full key.
type KeyState struct {
	Index  int    `json:"index"`
	Key    string `json:"key"` // masked
	Status string `json:"status"`
	// Plan is the key's Factory tier, "" while unknown (PlanName, PlanScale).
	Plan                  string     `json:"plan,omitempty"`
	StandardPercent       *float64   `json:"standardPercent,omitempty"`
	CorePercent           *float64   `json:"corePercent,omitempty"`
	StandardAvailable     bool       `json:"standardAvailable"`
	CoreAvailable         bool       `json:"coreAvailable"`
	StandardCooldownUntil *time.Time `json:"standardCooldownUntil,omitempty"`
	CoreCooldownUntil     *time.Time `json:"coreCooldownUntil,omitempty"`
	// ResetsAt is the nearest Standard window end.
	ResetsAt          *time.Time `json:"resetsAt,omitempty"`
	Limits            *KeyLimits `json:"limits,omitempty"`
	OveragePreference string     `json:"overagePreference,omitempty"`
	ExtraUsageBalance *float64   `json:"extraUsageBalance,omitempty"`
	UsageCheckedAt    *time.Time `json:"usageCheckedAt,omitempty"`
	Active            bool       `json:"active"`
}

// KeyLimits is the per-pool window breakdown behind the headline percentages.
type KeyLimits struct {
	Standard *PoolLimits `json:"standard,omitempty"`
	Core     *PoolLimits `json:"core,omitempty"`
}

// MaskKey renders a key as a display-safe prefix...suffix form.
func MaskKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// Snapshot returns the current routing state for every configured key, in
// configuration order.
func (r *KeyRotator) Snapshot() []KeyState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := r.GetActiveKey()
	out := make([]KeyState, 0, len(r.keys))
	for i, key := range r.keys {
		ks := KeyState{Index: i + 1, Key: MaskKey(key), Active: key == active}
		usage, ok := r.usages[key]
		if !ok {
			ks.Status = "no_usage_data"
			out = append(out, ks)
			continue
		}
		ks.Status = "ok"
		ks.Plan = usage.Plan
		if usage.Error != "" {
			ks.Status = usage.Error
		}
		ks.StandardPercent = usage.StandardPercent
		ks.CorePercent = usage.CorePercent
		ks.StandardAvailable = usage.StandardAvailable()
		ks.CoreAvailable = usage.CoreAvailable()
		if usage.ExpiresTsMs != nil && *usage.ExpiresTsMs > 0 {
			t := time.UnixMilli(int64(*usage.ExpiresTsMs))
			ks.ResetsAt = &t
		}
		if usage.StandardLimits != nil || usage.CoreLimits != nil {
			ks.Limits = &KeyLimits{Standard: usage.StandardLimits, Core: usage.CoreLimits}
		}
		ks.OveragePreference = usage.OveragePreference
		ks.ExtraUsageBalance = usage.ExtraUsageBalance
		if !usage.LastCheckedAt.IsZero() {
			t := usage.LastCheckedAt
			ks.UsageCheckedAt = &t
		}
		if !usage.StandardPoolCooldownUntil.IsZero() {
			t := usage.StandardPoolCooldownUntil
			ks.StandardCooldownUntil = &t
		}
		if !usage.CorePoolCooldownUntil.IsZero() {
			t := usage.CorePoolCooldownUntil
			ks.CoreCooldownUntil = &t
		}
		out = append(out, ks)
	}
	return out
}
