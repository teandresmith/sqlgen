// Package omittable provides Value[T], a generic wrapper that distinguishes
// "not provided" from "set to zero" in generated input structs.
package omittable

import (
	"encoding/json"
	"fmt"
)

// Value wraps a value of type T with presence tracking.
// A zero-value Value is unset (not provided).
type Value[T any] struct {
	value T
	set   bool
}

// Set creates a Value that is present with the given value.
func Set[T any](v T) Value[T] {
	return Value[T]{value: v, set: true}
}

// Omit creates a Value that is not present (zero state).
func Omit[T any]() Value[T] {
	return Value[T]{}
}

// Get returns the value and whether it was set.
func (o Value[T]) Get() (T, bool) {
	return o.value, o.set
}

// IsSet returns true if the value was explicitly provided.
func (o Value[T]) IsSet() bool {
	return o.set
}

// MustGet returns the value, panicking if not set.
func (o Value[T]) MustGet() T {
	if !o.set {
		panic("omittable: MustGet called on unset Value")
	}
	return o.value
}

// MarshalJSON implements json.Marshaler. A set value marshals to its JSON
// representation. This method is only called when the value is present in
// a struct — omitzero on the struct field handles the unset case.
func (o Value[T]) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(o.value)
	if err != nil {
		return nil, fmt.Errorf("omittable marshal: %w", err)
	}
	return b, nil
}

// UnmarshalJSON implements json.Unmarshaler. When called, the field was
// present in the JSON input, so the value is always marked as set.
// A JSON null will set the zero value for T (nil for pointer types).
func (o *Value[T]) UnmarshalJSON(data []byte) error {
	o.set = true
	if err := json.Unmarshal(data, &o.value); err != nil {
		return fmt.Errorf("omittable unmarshal: %w", err)
	}
	return nil
}

// IsZero reports whether the value is unset. This enables the omitzero
// JSON struct tag to omit unset values from marshaled output.
func (o Value[T]) IsZero() bool {
	return !o.set
}
