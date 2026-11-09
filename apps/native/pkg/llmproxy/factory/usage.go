// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT), index.mjs: windowEnd 971-981.

package factory

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var isoInstant = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)$`)
var digits = regexp.MustCompile(`^[+-]?\d+$`)

// WindowEnd reads the end of a usage window in /api/billing/limits as droid does, which hands it to new Date: an ISO
// time, or epoch milliseconds as a number or as a string of digits. False for anything else, null included.
func WindowEnd(raw json.RawMessage) (time.Time, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return time.Time{}, false
	}
	switch x := v.(type) {
	case string:
		if s := strings.ToUpper(x); isoInstant.MatchString(s) {
			t, err := time.Parse(time.RFC3339Nano, s)
			return t.UTC(), err == nil
		}
		if digits.MatchString(x) {
			if ms, err := strconv.ParseInt(x, 10, 64); err == nil && ms > 0 {
				return time.UnixMilli(ms).UTC(), true
			}
		}
	case float64:
		if x > 0 {
			return time.UnixMilli(int64(x)).UTC(), true
		}
	}
	return time.Time{}, false
}
