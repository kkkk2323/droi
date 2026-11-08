// Ported from @magpie-community/opencode-factory-auth 0.1.42 (MIT),
// index.mjs: VERSION 23, factoryHeaders 342-349, send's per-request headers 2071-2092.

package factory

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
)

// ClientVersion is the droid release the requests say they are, as upstream sends it. Factory's min_cli_version is
// read server side, and a release behind droid's has come with bare 403s.
const ClientVersion = "0.242.0"

// openAIPlatform is the OpenAI organization droid names on a request to an OpenAI model.
const openAIPlatform = "org-bHuLtG1fGmYk5YaOihAAXFBw"

// Headers are what droid sends with a model request to Factory, set on h for a request on path whose body names
// model, from a key of identity id, in session sessionID. The Authorization is the caller's: it carries the key.
func Headers(h http.Header, id Identity, path, model, sessionID string) {
	// A client's own vendor keys: Factory takes the bearer token alone.
	h.Del("X-Api-Key")
	h.Del("X-Goog-Api-Key")
	h.Set("X-Factory-Client", "cli")
	h.Set("X-Client-Version", ClientVersion)
	h.Set("User-Agent", "factory-cli/"+ClientVersion)
	if id.OrgID != "" {
		h.Set("X-Factory-Org-Id", id.OrgID)
	} else {
		h.Del("X-Factory-Org-Id")
	}
	upstream := Upstream(path, model)
	h.Set("X-Api-Provider", upstream)
	h.Set("X-Session-Id", sessionID)
	h.Set("X-Assistant-Message-Id", newUUID())
	h.Set("X-Provider-Routing-Source", "registry_default")
	if upstream == "openai" {
		h.Set("OpenAI-Platform", openAIPlatform)
	} else {
		h.Del("OpenAI-Platform")
	}
	// droid's Anthropic client is made with the key "placeholder", which Anthropic's SDK sends beside the bearer.
	if strings.Contains(path, "/llm/a/") {
		h.Set("X-Api-Key", "placeholder")
	}
	FastBeta(h, path, model)
}

// Upstream is the vendor a request names in x-api-provider: its model's, or for a model droid does not offer, the
// route's own vendor.
func Upstream(path, model string) string {
	if m, ok := ModelByID(model); ok {
		return m.Upstream
	}
	if strings.Contains(path, "/llm/o/") {
		return "openai"
	}
	if strings.Contains(path, "/llm/g/") {
		return "google"
	}
	return "anthropic"
}

// SessionID is the X-Session-Id of a conversation: the same for every request of one conversation on one install,
// and different on another install, so Factory's records of two computers never meet. It is a UUID, as droid's is.
func SessionID(install, conversation string) string {
	sum := sha256.Sum256([]byte("droi-session\x00" + install + "\x00" + conversation))
	return uuidOf(sum[:16], 5)
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return uuidOf(b[:], 4)
}

// uuidOf formats 16 bytes as an RFC 9562 UUID of the given version.
func uuidOf(b []byte, version byte) string {
	b[6] = b[6]&0x0f | version<<4
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
