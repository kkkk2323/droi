package kernel

import "encoding/json"

// Field is one field of a change, which has three states: absent, cleared and
// set.
//
// The contract says a change replaces a field when it is given, clears it when
// it is null, and leaves it alone when it is absent. A pointer alone cannot say
// all three — nil would be both "absent" and "null" — so a field of a change is
// a *Field: nil for absent, Some(v) for set, None for null.
type Field[T any] struct {
	// Value is what the field is set to; nil is the contract's null, which
	// clears the field.
	Value *T
}

// Some is a field the change sets to v.
func Some[T any](v T) *Field[T] { return &Field[T]{Value: &v} }

// None is a field the change clears: the contract's null.
func None[T any]() *Field[T] { return &Field[T]{} }

// MarshalJSON writes the value, or null.
func (f Field[T]) MarshalJSON() ([]byte, error) {
	if f.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*f.Value)
}

// UnmarshalJSON reads a value, or null.
func (f *Field[T]) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		f.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	f.Value = &v
	return nil
}
