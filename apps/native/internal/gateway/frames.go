package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
)

// InjectCredential replaces the Client's placeholder with the credential.
// The SDK sends it as `apiKey` in `daemon.authenticate` and as `token` (the
// spawn credential) in `daemon.initialize_session` / `daemon.load_session`.
// A login token goes in `token` in both cases, since the Daemon reads
// `apiKey` strictly as an API key. Only a top-level params field holding
// exactly the placeholder is touched; any other frame comes back unchanged.
func InjectCredential(frame []byte, cred *droid.Credential) []byte {
	if cred == nil || (cred.Token == "" && cred.APIKey == "") {
		return frame
	}
	if !bytes.Contains(frame, []byte(APIKeyPlaceholder)) {
		return frame
	}
	message, params, ok := parseWithParams(frame)
	if !ok {
		return frame
	}
	value := cred.Token
	if value == "" {
		value = cred.APIKey
	}
	changed := false
	if v, ok := params.get("apiKey"); ok && isString(v, APIKeyPlaceholder) {
		if cred.Token == "" {
			params.set("apiKey", encodeJSON(cred.APIKey))
		} else {
			params.del("apiKey")
			params.set("token", encodeJSON(cred.Token))
		}
		changed = true
	}
	if v, ok := params.get("token"); ok && isString(v, APIKeyPlaceholder) {
		params.set("token", encodeJSON(value))
		changed = true
	}
	if !changed {
		return frame
	}
	message.set("params", params.bytes())
	return message.bytes()
}

// InjectSystemPrompt adds the System Prompt Addition after Droid's own
// system prompt in a `daemon.initialize_session`, in the Daemon's
// {type: "preset", preset: "droid", append} form. A Client that chose a
// system prompt itself keeps it.
func InjectSystemPrompt(frame []byte, addition string) []byte {
	if strings.TrimSpace(addition) == "" {
		return frame
	}
	message, params, ok := parseWithParams(frame)
	if !ok {
		return frame
	}
	if m, ok := message.get("method"); !ok || !isString(m, initializeSession) {
		return frame
	}
	if _, ok := params.get("systemPrompt"); ok {
		return frame
	}
	if _, ok := params.get("systemPromptOverride"); ok {
		return frame
	}
	params.set("systemPrompt", encodeJSON(struct {
		Type   string `json:"type"`
		Preset string `json:"preset"`
		Append string `json:"append"`
	}{"preset", "droid", addition}))
	message.set("params", params.bytes())
	return message.bytes()
}

// object is a JSON object that keeps its key order and leaves every value's
// bytes as they came, so a rewritten frame differs only where it was changed.
type object []member

type member struct {
	key   string
	value json.RawMessage
}

func parseWithParams(frame []byte) (message, params object, ok bool) {
	message, ok = parseObject(frame)
	if !ok {
		return nil, nil, false
	}
	raw, ok := message.get("params")
	if !ok {
		return nil, nil, false
	}
	params, ok = parseObject(raw)
	return message, params, ok
}

func parseObject(data []byte) (object, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	var o object
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		o.set(key, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return o, true
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, m := range o {
		if m.key == key {
			return m.value, true
		}
	}
	return nil, false
}

// set replaces a key's value where it stands, as a JavaScript object does,
// or adds the key at the end.
func (o *object) set(key string, value json.RawMessage) {
	for i := range *o {
		if (*o)[i].key == key {
			(*o)[i].value = value
			return
		}
	}
	*o = append(*o, member{key, value})
}

func (o *object) del(key string) {
	for i := range *o {
		if (*o)[i].key == key {
			*o = append((*o)[:i], (*o)[i+1:]...)
			return
		}
	}
}

func (o object) bytes() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encodeJSON(m.key))
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func isString(v json.RawMessage, want string) bool {
	var s string
	return json.Unmarshal(v, &s) == nil && s == want
}

// encodeJSON is json.Marshal without HTML escaping, like JSON.stringify.
func encodeJSON(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
