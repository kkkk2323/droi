// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: contentRefused 412-422.

package factory

import (
	"encoding/json"
	"net/http"
)

// ContentRefused reports Factory's usage policy check refusing what a request says, not who sent it: a 403 with code
// content_policy_rejected and no x-factory-upstream-error header (droid 0.245.0 tells it apart the same way). The
// check reads the whole conversation, so the same request is refused again under any key, and the key is not at
// fault.
func ContentRefused(status int, header http.Header, body []byte) bool {
	if status != http.StatusForbidden || header.Get("X-Factory-Upstream-Error") != "" {
		return false
	}
	var v struct {
		Code  string          `json:"code"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &v) != nil {
		return false
	}
	if v.Code == "content_policy_rejected" {
		return true
	}
	var inner struct {
		Code string `json:"code"`
	}
	return json.Unmarshal(v.Error, &inner) == nil && inner.Code == "content_policy_rejected"
}
