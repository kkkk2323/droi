package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf16"
)

// ErrOps says a delta push's ops do not follow on from the view they were applied to: the view held is no longer
// Kernel's, and the subscriber opens the conversation again for a whole one.
var ErrOps = errors.New("the view ops do not follow on from the view held")

// ApplyOps returns v with a "delta" push's ops applied in order (Pi's chord ops: r, s, d, a, t, p, m). v and what it
// holds are left as they were: the entries and the docs are copied before they change, so a reader that holds the
// old view still holds the old view.
//
// The ops are exact JSON edits, and their shape carries no meaning, so any of them is applied as an edit of the
// view's JSON. Two places are kept typed for speed, because they are where a running turn writes: a splice of the
// entries decodes only the entries it inserts, and an edit inside one doc decodes and encodes that doc alone, once
// per push. A key of the view this type does not hold is skipped, as decoding the whole view would skip it.
func ApplyOps(v View, raw json.RawMessage) (View, error) {
	var ops []json.RawMessage
	if err := json.Unmarshal(raw, &ops); err != nil {
		return v, fmt.Errorf("%w: %v", ErrOps, err)
	}
	a := opApplier{view: v}
	for _, op := range ops {
		if err := a.apply(op); err != nil {
			return v, fmt.Errorf("%w: %v", ErrOps, err)
		}
	}
	if err := a.flushDocs(); err != nil {
		return v, fmt.Errorf("%w: %v", ErrOps, err)
	}
	return a.view, nil
}

type opApplier struct {
	view View
	// ownEntries and ownDocs say the view's entries and docs are this call's copies, which later ops may change.
	ownEntries, ownDocs bool
	// docs are the docs an op edited inside, decoded; they are encoded back once, at the end.
	docs map[string]any
}

func (a *opApplier) apply(raw json.RawMessage) error {
	var op []json.RawMessage
	if err := json.Unmarshal(raw, &op); err != nil || len(op) == 0 {
		return errors.New("an op is not a tuple")
	}
	var kind string
	if err := json.Unmarshal(op[0], &kind); err != nil {
		return errors.New("an op has no kind")
	}
	if kind == "r" {
		if len(op) != 2 {
			return errors.New("a replace without its value")
		}
		var whole View
		if err := json.Unmarshal(op[1], &whole); err != nil {
			return err
		}
		a.view, a.ownEntries, a.ownDocs, a.docs = whole, true, true, nil
		return nil
	}
	if len(op) < 2 {
		return errors.New("an op has no path")
	}
	path, err := decodePath(op[1])
	if err != nil {
		return err
	}
	args := op[2:]
	if len(path) == 0 {
		return errors.New("an op on the root")
	}
	top, ok := path[0].(string)
	if !ok {
		return errors.New("an index into the view")
	}
	if len(path) == 1 && (kind == "s" || kind == "d") {
		return a.setTop(top, kind, args)
	}
	switch top {
	case "entries":
		return a.entriesOp(kind, path[1:], args)
	case "docs":
		return a.docsOp(kind, path[1:], args)
	case "conversation":
		return editJSON(&a.view.Conversation, kind, path[1:], args)
	case "before":
		return editRaw(&a.view.Before, kind, path[1:], args)
	}
	// A key View does not keep: decoding the whole view would have dropped it too.
	return nil
}

// setTop sets or deletes one key of the view.
func (a *opApplier) setTop(key, kind string, args []json.RawMessage) error {
	var value json.RawMessage
	if kind == "s" {
		if len(args) != 1 {
			return errors.New("a set without its value")
		}
		value = args[0]
	}
	switch key {
	case "entries":
		a.view.Entries, a.ownEntries = nil, true
		if value != nil {
			return json.Unmarshal(value, &a.view.Entries)
		}
	case "docs":
		a.view.Docs, a.ownDocs, a.docs = nil, true, nil
		if value != nil {
			return json.Unmarshal(value, &a.view.Docs)
		}
	case "conversation":
		a.view.Conversation = Conversation{}
		if value != nil {
			return json.Unmarshal(value, &a.view.Conversation)
		}
	case "before":
		a.view.Before = nil
		if value != nil && !isNull(value) {
			a.view.Before = slices.Clone(value)
		}
	}
	return nil
}

// entriesOp applies an op whose path starts inside the view's entries; rest is the path below "entries".
func (a *opApplier) entriesOp(kind string, rest []any, args []json.RawMessage) error {
	if !a.ownEntries {
		a.view.Entries, a.ownEntries = slices.Clone(a.view.Entries), true
	}
	entries := a.view.Entries
	if len(rest) == 0 {
		switch kind {
		case "p":
			index, remove, items, err := spliceArgs(args)
			if err != nil {
				return err
			}
			if index > len(entries) || index+remove > len(entries) {
				return errors.New("a splice past the entries")
			}
			var inserted []Entry
			if err := json.Unmarshal(items, &inserted); err != nil {
				return err
			}
			a.view.Entries = slices.Concat(entries[:index], inserted, entries[index+remove:])
			return nil
		case "m":
			order, err := permutation(args, len(entries))
			if err != nil {
				return err
			}
			moved := make([]Entry, len(entries))
			for i, from := range order {
				moved[i] = entries[from]
			}
			a.view.Entries = moved
			return nil
		}
		return fmt.Errorf("a %q op on the entries", kind)
	}
	index, ok := rest[0].(int)
	if !ok {
		return errors.New("an entry named by a key")
	}
	if len(rest) == 1 && (kind == "s" || kind == "d") {
		switch {
		case kind == "d" && index < len(entries):
			a.view.Entries = slices.Delete(entries, index, index+1)
		case kind == "s" && index <= len(entries):
			if len(args) != 1 {
				return errors.New("a set without its value")
			}
			var entry Entry
			if err := json.Unmarshal(args[0], &entry); err != nil {
				return err
			}
			if index == len(entries) {
				a.view.Entries = append(entries, entry)
			} else {
				entries[index] = entry
			}
		default:
			return errors.New("an index past the entries")
		}
		return nil
	}
	if index >= len(entries) {
		return errors.New("an index past the entries")
	}
	return editJSON(&entries[index], kind, rest[1:], args)
}

// docsOp applies an op whose path starts inside the view's docs; rest is the path below "docs".
func (a *opApplier) docsOp(kind string, rest []any, args []json.RawMessage) error {
	if len(rest) == 0 {
		return fmt.Errorf("a %q op on the docs", kind)
	}
	key, ok := rest[0].(string)
	if !ok {
		return errors.New("a doc named by an index")
	}
	if !a.ownDocs {
		a.view.Docs, a.ownDocs = maps.Clone(a.view.Docs), true
	}
	if a.view.Docs == nil {
		a.view.Docs = map[string]json.RawMessage{}
	}
	if len(rest) == 1 && (kind == "s" || kind == "d") {
		delete(a.docs, key)
		if kind == "d" {
			delete(a.view.Docs, key)
			return nil
		}
		if len(args) != 1 {
			return errors.New("a set without its value")
		}
		a.view.Docs[key] = slices.Clone(args[0])
		return nil
	}
	doc, decoded := a.docs[key]
	if !decoded {
		raw, ok := a.view.Docs[key]
		if !ok {
			return fmt.Errorf("no doc %q", key)
		}
		var err error
		if doc, err = decodeTree(raw); err != nil {
			return err
		}
	}
	doc, err := editTree(doc, kind, rest[1:], args)
	if err != nil {
		return err
	}
	if a.docs == nil {
		a.docs = map[string]any{}
	}
	a.docs[key] = doc
	return nil
}

// flushDocs encodes the docs ops edited inside back into the view.
func (a *opApplier) flushDocs() error {
	for key, doc := range a.docs {
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		a.view.Docs[key] = raw
	}
	a.docs = nil
	return nil
}

// editJSON applies an op to *target through its JSON: encoded, edited, and decoded back.
func editJSON[T any](target *T, kind string, path []any, args []json.RawMessage) error {
	raw, err := json.Marshal(target)
	if err != nil {
		return err
	}
	tree, err := decodeTree(raw)
	if err != nil {
		return err
	}
	if tree, err = editTree(tree, kind, path, args); err != nil {
		return err
	}
	if raw, err = json.Marshal(tree); err != nil {
		return err
	}
	var next T
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	*target = next
	return nil
}

// editRaw applies an op inside a raw JSON value.
func editRaw(target *json.RawMessage, kind string, path []any, args []json.RawMessage) error {
	if len(*target) == 0 {
		return errors.New("an op inside a value that is not there")
	}
	tree, err := decodeTree(*target)
	if err != nil {
		return err
	}
	if tree, err = editTree(tree, kind, path, args); err != nil {
		return err
	}
	raw, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	*target = raw
	return nil
}

// editTree applies one op to a decoded JSON value it owns, at path below it, and returns the value: a splice of a
// top-level array makes a new slice, so the caller keeps what comes back.
func editTree(root any, kind string, path []any, args []json.RawMessage) (any, error) {
	switch kind {
	case "p", "m":
		return modify(root, path, func(node any) (any, error) {
			list, ok := node.([]any)
			if !ok {
				return nil, errors.New("a splice of something that is not an array")
			}
			if kind == "m" {
				order, err := permutation(args, len(list))
				if err != nil {
					return nil, err
				}
				moved := make([]any, len(list))
				for i, from := range order {
					moved[i] = list[from]
				}
				return moved, nil
			}
			index, remove, raw, err := spliceArgs(args)
			if err != nil {
				return nil, err
			}
			if index > len(list) || index+remove > len(list) {
				return nil, errors.New("a splice past its array")
			}
			items, err := decodeTree(raw)
			if err != nil {
				return nil, err
			}
			inserted, ok := items.([]any)
			if !ok {
				return nil, errors.New("a splice without its items")
			}
			return slices.Concat(list[:index], inserted, list[index+remove:]), nil
		})
	case "s", "d", "a", "t":
		if len(path) == 0 {
			return nil, errors.New("an op on the root")
		}
		key := path[len(path)-1]
		return modify(root, path[:len(path)-1], func(parent any) (any, error) {
			return editChild(parent, key, kind, args)
		})
	}
	return nil, fmt.Errorf("an unknown op %q", kind)
}

// editChild applies a set, delete, append or trim to one child of parent.
func editChild(parent any, key any, kind string, args []json.RawMessage) (any, error) {
	var arg json.RawMessage
	if kind != "d" {
		if len(args) != 1 {
			return nil, fmt.Errorf("a %q op without its argument", kind)
		}
		arg = args[0]
	}
	text := func(current any) (any, error) {
		s, ok := current.(string)
		if !ok {
			return nil, errors.New("a text op on something else")
		}
		if kind == "a" {
			var tail string
			if err := json.Unmarshal(arg, &tail); err != nil {
				return nil, err
			}
			return s + tail, nil
		}
		var count int
		if err := json.Unmarshal(arg, &count); err != nil || count < 0 {
			return nil, errors.New("a trim without its count")
		}
		return trimUTF16(s, count), nil
	}
	switch node := parent.(type) {
	case map[string]any:
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("an index into an object")
		}
		switch kind {
		case "s":
			value, err := decodeTree(arg)
			if err != nil {
				return nil, err
			}
			node[name] = value
		case "d":
			delete(node, name)
		default:
			next, err := text(node[name])
			if err != nil {
				return nil, err
			}
			node[name] = next
		}
		return node, nil
	case []any:
		index, ok := key.(int)
		if !ok || index > len(node) {
			return nil, errors.New("an index past its array")
		}
		switch kind {
		case "s":
			value, err := decodeTree(arg)
			if err != nil {
				return nil, err
			}
			if index == len(node) {
				return append(node, value), nil
			}
			node[index] = value
		case "d":
			if index >= len(node) {
				return nil, errors.New("a delete past its array")
			}
			return slices.Delete(node, index, index+1), nil
		default:
			if index >= len(node) {
				return nil, errors.New("an index past its array")
			}
			next, err := text(node[index])
			if err != nil {
				return nil, err
			}
			node[index] = next
		}
		return node, nil
	}
	return nil, errors.New("an op inside something that is not a container")
}

// modify replaces the value at path below node with what change makes of it, and returns node.
func modify(node any, path []any, change func(any) (any, error)) (any, error) {
	if len(path) == 0 {
		return change(node)
	}
	switch c := node.(type) {
	case map[string]any:
		name, ok := path[0].(string)
		if !ok {
			return nil, errors.New("an index into an object")
		}
		child, ok := c[name]
		if !ok {
			return nil, fmt.Errorf("no %q", name)
		}
		next, err := modify(child, path[1:], change)
		if err != nil {
			return nil, err
		}
		c[name] = next
		return c, nil
	case []any:
		index, ok := path[0].(int)
		if !ok || index >= len(c) {
			return nil, errors.New("an index past its array")
		}
		next, err := modify(c[index], path[1:], change)
		if err != nil {
			return nil, err
		}
		c[index] = next
		return c, nil
	}
	return nil, errors.New("a path through something that is not a container")
}

// decodePath reads an op's path: object keys as strings, array indices as ints.
func decodePath(raw json.RawMessage) ([]any, error) {
	var segments []json.RawMessage
	if err := json.Unmarshal(raw, &segments); err != nil {
		return nil, errors.New("an op has no path")
	}
	path := make([]any, len(segments))
	for i, segment := range segments {
		var name string
		if json.Unmarshal(segment, &name) == nil {
			if name == "__proto__" || name == "constructor" || name == "prototype" {
				return nil, fmt.Errorf("an unsafe path segment %q", name)
			}
			path[i] = name
			continue
		}
		var index int
		if err := json.Unmarshal(segment, &index); err != nil || index < 0 {
			return nil, fmt.Errorf("an unsafe path segment %s", segment)
		}
		path[i] = index
	}
	return path, nil
}

func spliceArgs(args []json.RawMessage) (index, remove int, items json.RawMessage, err error) {
	if len(args) != 3 {
		return 0, 0, nil, errors.New("a splice without index, count and items")
	}
	if json.Unmarshal(args[0], &index) != nil || json.Unmarshal(args[1], &remove) != nil || index < 0 || remove < 0 {
		return 0, 0, nil, errors.New("a splice without index and count")
	}
	return index, remove, args[2], nil
}

func permutation(args []json.RawMessage, n int) ([]int, error) {
	var order []int
	if len(args) != 1 || json.Unmarshal(args[0], &order) != nil || len(order) != n {
		return nil, errors.New("a reorder of another array")
	}
	for _, from := range order {
		if from < 0 || from >= n {
			return nil, errors.New("a reorder past its array")
		}
	}
	return order, nil
}

// decodeTree decodes JSON into maps, slices and json.Number, so numbers encode back exactly as they came.
func decodeTree(raw json.RawMessage) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var tree any
	if err := d.Decode(&tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// trimUTF16 removes count UTF-16 code units from the front of s, as JavaScript's slice does.
func trimUTF16(s string, count int) string {
	units := 0
	for i, r := range s {
		if units >= count {
			return s[i:]
		}
		units += utf16.RuneLen(r)
	}
	return ""
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }
