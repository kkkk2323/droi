package keys

import (
	"encoding/json"
	"reflect"
	"time"
)

// UsageSnapshot is one key's billing windows as seen by a usage refresh. A
// series of them lets credits spent between two refreshes be matched to the
// percentage points they moved.
type UsageSnapshot struct {
	Time     time.Time   `json:"time"`
	Index    int         `json:"index"`
	Key      string      `json:"key"` // masked
	Plan     string      `json:"plan,omitempty"`
	Standard *PoolLimits `json:"standard,omitempty"`
	Core     *PoolLimits `json:"core,omitempty"`
}

// UsageLineWriter receives one JSON line per snapshot (diag.FileSink fits).
type UsageLineWriter interface {
	Write(line []byte) error
}

// SetUsageLog makes later refreshes record a snapshot for every key whose
// billing windows changed, plus every key on its first successful probe.
func (r *KeyRotator) SetUsageLog(w UsageLineWriter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usageLog = w
}

func (r *KeyRotator) changedSnapshotsLocked(fetched []*KeyUsage) []UsageSnapshot {
	if r.usageLog == nil {
		return nil
	}
	index := make(map[string]int, len(r.keys))
	for i, k := range r.keys {
		index[k] = i + 1
	}
	var out []UsageSnapshot
	for _, u := range fetched {
		if u.Error != "" || (u.StandardLimits == nil && u.CoreLimits == nil) {
			continue
		}
		if prev, ok := r.usages[u.Key]; ok &&
			reflect.DeepEqual(prev.StandardLimits, u.StandardLimits) &&
			reflect.DeepEqual(prev.CoreLimits, u.CoreLimits) {
			continue
		}
		out = append(out, UsageSnapshot{
			Time:     u.LastCheckedAt,
			Index:    index[u.Key],
			Key:      MaskKey(u.Key),
			Plan:     u.Plan,
			Standard: u.StandardLimits,
			Core:     u.CoreLimits,
		})
	}
	return out
}

func writeSnapshots(w UsageLineWriter, snapshots []UsageSnapshot) {
	if w == nil {
		return
	}
	for _, s := range snapshots {
		if line, err := json.Marshal(s); err == nil {
			_ = w.Write(line)
		}
	}
}
