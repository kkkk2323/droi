package factory

import (
	"bytes"
	"encoding/json"
	"strconv"
	"unicode/utf8"
)

// object is a JSON object as JavaScript's JSON.parse holds one, so a body this package changes goes out as
// upstream's JSON.stringify writes it: the keys keep the order they came in, a key set anew goes last, and a key
// given twice keeps its first place and its last value.
type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func (o *object) get(key string) json.RawMessage { return o.vals[key] }

func (o *object) has(key string) bool {
	_, ok := o.vals[key]
	return ok
}

func (o *object) len() int { return len(o.keys) }

func (o *object) set(key string, v json.RawMessage) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

func (o *object) del(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// raw is o as JSON, its keys in JavaScript's order (jsOrder) and its values as they are.
func (o *object) raw() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range jsOrder(o.keys) {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSString(&b, k)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

// newObject is an object of keys and values, in that order.
func newObject(kv ...any) *object {
	o := &object{vals: map[string]json.RawMessage{}}
	for i := 0; i+1 < len(kv); i += 2 {
		o.set(kv[i].(string), kv[i+1].(json.RawMessage))
	}
	return o
}

// A body is checked to be JSON once, where it comes in (Request, LeaveOutAttachments, sanitizeBody); what is read
// out of it after that is valid JSON, and is read without being checked again.

// objectOf is raw, valid JSON, as an object: false for any other value. Its values are slices of raw.
func objectOf(raw []byte) (*object, bool) {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return nil, false
	}
	o := &object{vals: map[string]json.RawMessage{}}
	eachMember(raw, i, func(key string, value []byte) { o.set(key, value) })
	return o, true
}

// arrayOf is raw, valid JSON, as an array's values, slices of raw: false for any other value, null included.
func arrayOf(raw []byte) ([]json.RawMessage, bool) {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '[' {
		return nil, false
	}
	out := []json.RawMessage{}
	i = skipSpace(raw, i+1)
	for raw[i] != ']' {
		end := skipValue(raw, i)
		out = append(out, raw[i:end])
		i = skipSpace(raw, end)
		if raw[i] == ',' {
			i = skipSpace(raw, i+1)
		}
	}
	return out, true
}

// eachMember calls f with each key and value of the object at raw[i], which is valid JSON, in their order, and is
// the index past the object.
func eachMember(raw []byte, i int, f func(key string, value []byte)) int {
	i = skipSpace(raw, i+1)
	for raw[i] != '}' {
		end := skipString(raw, i)
		key := stringOf(raw[i:end])
		start := skipSpace(raw, skipSpace(raw, end)+1)
		stop := skipValue(raw, start)
		f(key, raw[start:stop])
		i = skipSpace(raw, stop)
		if raw[i] == ',' {
			i = skipSpace(raw, i+1)
		}
	}
	return i + 1
}

// stringOf is the text of quoted, a valid JSON string.
func stringOf(quoted []byte) string {
	if bytes.IndexByte(quoted, '\\') < 0 && utf8.Valid(quoted) {
		return string(quoted[1 : len(quoted)-1])
	}
	var s string
	_ = json.Unmarshal(quoted, &s)
	return s
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// skipString is the index past the JSON string at b[i]: past the first quote after it that an odd run of
// backslashes does not escape.
func skipString(b []byte, i int) int {
	from := i + 1
	for {
		q := from + bytes.IndexByte(b[from:], '"')
		n := 0
		for k := q - 1; k >= from && b[k] == '\\'; k-- {
			n++
		}
		if n%2 == 0 {
			return q + 1
		}
		from = q + 1
	}
}

// skipValue is the index past the JSON value at b[i], which is valid JSON.
func skipValue(b []byte, i int) int {
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		depth := 0
		for ; ; i++ {
			switch b[i] {
			case '"':
				i = skipString(b, i) - 1
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return i + 1
				}
			}
		}
	}
	for i < len(b) && b[i] != ',' && b[i] != '}' && b[i] != ']' && b[i] != ' ' && b[i] != '\t' && b[i] != '\n' && b[i] != '\r' {
		i++
	}
	return i
}

// jsOrder is the order JavaScript gives an object's keys: those that are array indexes first, in their numeric
// order, then the others in the order they were set.
func jsOrder(keys []string) []string {
	indexes := 0
	for _, k := range keys {
		if arrayIndex(k) {
			indexes++
		}
	}
	if indexes == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if arrayIndex(k) {
			out = append(out, k)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && indexValue(out[j]) < indexValue(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	for _, k := range keys {
		if !arrayIndex(k) {
			out = append(out, k)
		}
	}
	return out
}

// arrayIndex reports a key ECMAScript counts as an array index: the canonical decimal of an integer below 2^32-1.
func arrayIndex(k string) bool {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	return indexValue(k) < 1<<32-1
}

func indexValue(k string) uint64 {
	n, _ := strconv.ParseUint(k, 10, 64)
	return n
}

// jsonStr is text as a JSON string, as JSON.stringify writes it.
func jsonStr(text string) json.RawMessage {
	var b bytes.Buffer
	writeJSString(&b, text)
	return b.Bytes()
}

// jsonList is a JSON array of items.
func jsonList(items []json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(item)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// writeJSString writes s as JSON.stringify does: the quote, the backslash and the control characters escaped,
// \b \f \n \r \t by their short names, and nothing else: not <, > and &, and not U+2028 and U+2029, which Go's
// encoder escapes.
func writeJSString(b *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s[i:])
			b.WriteRune(r)
			i += size
			continue
		}
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
		i++
	}
	b.WriteByte('"')
}

// canonical is valid, a JSON body, as upstream's JSON.stringify(JSON.parse(valid)) writes it, which is how upstream
// sends any body it changed: no spaces, keys in JavaScript's order, a key given twice once, strings written as
// writeJSString writes them. Numbers keep their own spelling, so a large integer is not rounded on its way through.
func canonical(valid []byte) []byte {
	var b bytes.Buffer
	b.Grow(len(valid))
	writeCanonical(&b, valid, skipSpace(valid, 0))
	return b.Bytes()
}

// writeCanonical writes the value at raw[i], valid JSON, as canonical writes it, and is the index past it.
func writeCanonical(b *bytes.Buffer, raw []byte, i int) int {
	switch raw[i] {
	case '"':
		end := skipString(raw, i)
		writeCanonicalString(b, raw[i:end])
		return end
	case '[':
		b.WriteByte('[')
		i = skipSpace(raw, i+1)
		for n := 0; raw[i] != ']'; n++ {
			if n > 0 {
				b.WriteByte(',')
			}
			i = skipSpace(raw, writeCanonical(b, raw, i))
			if raw[i] == ',' {
				i = skipSpace(raw, i+1)
			}
		}
		b.WriteByte(']')
		return i + 1
	case '{':
		// Written as it is read, unless a key comes twice or is an array index: then JavaScript's order is not the
		// order read, and the object is written again from its members.
		start := b.Len()
		b.WriteByte('{')
		var keys []string
		j := skipSpace(raw, i+1)
		for raw[j] != '}' {
			end := skipString(raw, j)
			key := stringOf(raw[j:end])
			if arrayIndex(key) || seenKey(keys, key) {
				b.Truncate(start)
				return writeCanonicalObject(b, raw, i)
			}
			if len(keys) > 0 {
				b.WriteByte(',')
			}
			keys = append(keys, key)
			writeCanonicalString(b, raw[j:end])
			b.WriteByte(':')
			j = writeCanonical(b, raw, skipSpace(raw, skipSpace(raw, end)+1))
			j = skipSpace(raw, j)
			if raw[j] == ',' {
				j = skipSpace(raw, j+1)
			}
		}
		b.WriteByte('}')
		return j + 1
	}
	end := skipValue(raw, i)
	b.Write(raw[i:end])
	return end
}

// writeCanonicalObject writes the object at raw[i] from its members, so a key given twice is written once and the
// array indexes come first.
func writeCanonicalObject(b *bytes.Buffer, raw []byte, i int) int {
	o := &object{vals: map[string]json.RawMessage{}}
	j := skipSpace(raw, i+1)
	for raw[j] != '}' {
		end := skipString(raw, j)
		key := stringOf(raw[j:end])
		var v bytes.Buffer
		j = skipSpace(raw, writeCanonical(&v, raw, skipSpace(raw, skipSpace(raw, end)+1)))
		o.set(key, v.Bytes())
		if raw[j] == ',' {
			j = skipSpace(raw, j+1)
		}
	}
	b.Write(o.raw())
	return j + 1
}

// writeCanonicalString writes quoted, a valid JSON string, as writeJSString writes its text: as it is when it holds
// no escape to read and is valid UTF-8.
func writeCanonicalString(b *bytes.Buffer, quoted []byte) {
	if bytes.IndexByte(quoted, '\\') < 0 && utf8.Valid(quoted) {
		b.Write(quoted)
		return
	}
	writeJSString(b, stringOf(quoted))
}

func seenKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
