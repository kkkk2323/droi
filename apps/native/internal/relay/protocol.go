// Package relay is Droi's Relay (ADR 0034): a server that a Host dials out
// to and keeps a WebSocket open with, so that a phone away from the Host's
// network reaches its Gateway through the server. It speaks alma-relay's
// protocol 2 (github.com/yetone/alma-relay, docs/PROTOCOL.md): the Host's
// Agent works against alma-relay's servers too, and Droi's Server against an
// Agent that speaks it.
package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Protocol is the newest protocol the Agent and Server speak.
const Protocol = 2

const (
	// bodyWindow is how many request body bytes the Server sends ahead of the
	// Agent's acknowledgements; bodySlice the most it puts in one frame.
	bodyWindow = 4 << 20
	bodySlice  = 256 << 10
	// agentMaxFrame bounds a frame from the Agent: alma-relay's 32 MiB, and
	// more on Droi's Server, where a Daemon message may be larger.
	agentMaxFrame  = 32 << 20
	serverMaxFrame = 128 << 20
	// clientMaxMessage bounds a phone's WebSocket message.
	clientMaxMessage = 24 << 20
	// headTimeout is how long the Server waits for the Agent's response head
	// once it has the whole request.
	headTimeout     = 120 * time.Second
	registerTimeout = 10 * time.Second
	// chunkSize is how much of a response body the Agent puts in one frame.
	chunkSize = 32 << 10
)

// Close codes the Server ends an Agent's socket with.
const (
	// CloseReplaced: another Agent registered the same Relay Id.
	CloseReplaced = 4000
	// CloseBadHash: the token hash is malformed.
	CloseBadHash = 4002
	// CloseBadKey: the registration key is wrong.
	CloseBadKey = 4003
	// CloseBadToken: alma-relay's signed relay token is missing or wrong.
	CloseBadToken = 4401
)

// Binary frame kinds: [kind][idLen][id][payload].
const (
	binHChunk byte = 1 // Agent → Server: response body bytes
	binHBody  byte = 2 // Server → Agent: request body bytes
	binWBin   byte = 3 // both: a binary WebSocket message
	binWText  byte = 4 // both: a text WebSocket message
)

// frame is a JSON frame; T names it and the rest depends on T.
type frame struct {
	T         string  `json:"t"`
	ID        string  `json:"id,omitempty"`
	TokenHash string  `json:"tokenHash,omitempty"`
	Name      string  `json:"name,omitempty"`
	Ver       string  `json:"ver,omitempty"`
	Key       string  `json:"key,omitempty"`
	Proto     int     `json:"proto,omitempty"`
	Message   string  `json:"message,omitempty"`
	Method    string  `json:"method,omitempty"`
	Path      string  `json:"path,omitempty"`
	Headers   headers `json:"headers,omitempty"`
	Stream    bool    `json:"stream,omitempty"`
	Status    int     `json:"status,omitempty"`
	B         *string `json:"b,omitempty"`
	S         *string `json:"s,omitempty"`
	N         int64   `json:"n,omitempty"`
	Code      int     `json:"code,omitempty"`
	Reason    string  `json:"reason,omitempty"`
	// Unsupported answers an alma desktop's push: Droi's Relay sends none.
	Unsupported bool `json:"unsupported,omitempty"`
}

// headers are HTTP headers as Node writes them: lower-case names, a string
// for one value and an array for several.
type headers map[string][]string

func (h headers) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(h))
	for k, v := range h {
		if len(v) == 1 {
			out[k] = v[0]
		} else {
			out[k] = v
		}
	}
	return json.Marshal(out)
}

func (h *headers) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*h = headers{}
	for k, v := range raw {
		var one string
		if json.Unmarshal(v, &one) == nil {
			(*h)[k] = []string{one}
			continue
		}
		var many []string
		if json.Unmarshal(v, &many) == nil {
			(*h)[k] = many
		}
	}
	return nil
}

// Hop-by-hop headers, and what the tunnel sets itself.
var (
	requestDropped  = []string{"host", "connection", "content-length", "accept-encoding", "keep-alive", "transfer-encoding", "upgrade", "te", "trailer", "proxy-connection", "proxy-authorization"}
	responseDropped = []string{"connection", "keep-alive", "transfer-encoding", "upgrade", "te", "trailer"}
)

func fromHTTP(h http.Header, drop []string) headers {
	out := headers{}
	for k, v := range h {
		out[strings.ToLower(k)] = v
	}
	for _, k := range drop {
		delete(out, k)
	}
	return out
}

func (h headers) apply(to http.Header, drop []string) {
	for k, v := range h {
		lk := strings.ToLower(k)
		if contains(drop, lk) {
			continue
		}
		for _, s := range v {
			to.Add(k, s)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func encodeBinary(kind byte, id string, payload []byte) []byte {
	out := make([]byte, 2+len(id)+len(payload))
	out[0] = kind
	out[1] = byte(len(id))
	copy(out[2:], id)
	copy(out[2+len(id):], payload)
	return out
}

var errBadFrame = errors.New("malformed binary frame")

// decodeBinary returns views into b.
func decodeBinary(b []byte) (kind byte, id string, payload []byte, err error) {
	if len(b) < 2 || len(b) < 2+int(b[1]) {
		return 0, "", nil, errBadFrame
	}
	n := int(b[1])
	return b[0], string(b[2 : 2+n]), b[2+n:], nil
}

// TokenHash is what an Agent registers with for Relay Id id.
func TokenHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

var (
	validID   = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	validHash = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// ValidID reports whether id can address a Host on a Relay.
func ValidID(id string) bool { return validID.MatchString(id) }

// clientPath splits a phone's path, /c/<id>/rest, into the Relay Id and the
// rest; ok is false for any other path.
func clientPath(path string) (id, rest string, ok bool) {
	after, found := strings.CutPrefix(path, "/c/")
	if !found {
		return "", "", false
	}
	id, rest, _ = strings.Cut(after, "/")
	if !ValidID(id) {
		return "", "", false
	}
	return id, "/" + rest, true
}

// negotiate is the protocol for an Agent that offers offered: the lower of
// it and Protocol, and 1 when it offers nothing.
func negotiate(offered int) int {
	if offered >= 2 {
		return min(offered, Protocol)
	}
	return 1
}

// sanitizeCloseCode is a close code a server may send on.
func sanitizeCloseCode(code int) int {
	if code == 1000 || (code >= 3000 && code <= 4999) {
		return code
	}
	return 1000
}
