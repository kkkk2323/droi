// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: contentRefused 412-422, explain 428 (its content refusal), upstreamIds 436-453, vendorError 952-969,
// errorReply 792-802.

package factory

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
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

// ContentPolicyHint is what to do about a refusal of the usage policy check.
const ContentPolicyHint = "Factory's usage policy check reads the whole conversation, so this conversation's requests are refused again until the text that tripped it is gone: start a new conversation, or remove or reword what the conversation quotes (another agent's system prompt or instructions pasted in a message or a tool's output are the usual cause)"

// VendorError is the message in an error body: Factory's {error: {message}} or {error}, {message}, {detail}, else the
// body cut short after fallback.
func VendorError(body []byte, fallback string) string {
	var v map[string]any
	if json.Unmarshal(body, &v) == nil && v != nil {
		err := v["error"]
		if d, ok := v["detail"]; ok && d != nil {
			err = d
		}
		if errs, ok := v["errors"].([]any); ok && len(errs) > 0 {
			if first, ok := errs[0].(map[string]any); ok {
				if s, ok := first["message"].(string); ok && s != "" {
					return s
				}
			}
		}
		if e, ok := err.(map[string]any); ok {
			if s, ok := e["message"].(string); ok && s != "" {
				return s
			}
		}
		if s, ok := err.(string); ok && s != "" {
			return s
		}
		for _, k := range []string{"message", "msg"} {
			if s, ok := v[k].(string); ok && s != "" {
				return s
			}
		}
	}
	s := strings.Join(strings.Fields(string(body)), " ")
	if s == "" || strings.HasPrefix(s, "<") {
		return fallback
	}
	if utf8.RuneCountInString(s) > 300 {
		s = string([]rune(s)[:300]) + "…"
	}
	return fallback + ": " + s
}

var requestIDHeaders = []string{"X-Request-Id", "Request-Id", "X-Amzn-Requestid", "Cf-Ray"}

var printable = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)

// UpstreamIDs is what Factory's refusal says to tell it apart when asking Factory about it: the request id from its
// body or headers, and " (upstream request id …, code …, type …)" with what of them it has.
func UpstreamIDs(header http.Header, body []byte) (id, said string) {
	var v map[string]any
	_ = json.Unmarshal(body, &v)
	errObj, _ := v["error"].(map[string]any)
	str := func(x any) string {
		if s, ok := x.(string); ok && printable.MatchString(s) {
			return s
		}
		return ""
	}
	for _, c := range []any{v["request_id"], v["requestId"], errObj["request_id"], errObj["requestId"]} {
		if id = str(c); id != "" {
			break
		}
	}
	for _, h := range requestIDHeaders {
		if id != "" {
			break
		}
		id = str(header.Get(h))
	}
	var parts []string
	if id != "" {
		parts = append(parts, "request id "+id)
	}
	code := str(errObj["code"])
	if code == "" {
		code = str(v["code"])
	}
	if code != "" {
		parts = append(parts, "code "+code)
	}
	if t := str(errObj["type"]); t != "" {
		parts = append(parts, "type "+t)
	}
	if len(parts) > 0 {
		said = " (upstream " + strings.Join(parts, ", ") + ")"
	}
	return id, said
}

// ErrorBody is an error as the API a request to path words one: Anthropic's on /llm/a/, OpenAI's on the others.
// code is the vendor's error code, kept on the error: clients tell a refusal of the request apart by it.
func ErrorBody(path, kind, message, requestID, code string) []byte {
	var body map[string]any
	if strings.Contains(path, "/llm/a/") {
		e := map[string]any{"type": kind, "message": message}
		if code != "" {
			e["code"] = code
		}
		body = map[string]any{"type": "error", "error": e}
		if requestID != "" {
			body["request_id"] = requestID
		}
	} else {
		var c any
		if code != "" {
			c = code
		}
		body = map[string]any{"error": map[string]any{"message": message, "type": kind, "code": c}}
	}
	b, _ := marshalJSON(body)
	return b
}
