package utils

import (
	"encoding/json"
)

// Option type inspired https://pkg.go.dev/github.com/samber/mo.
type Option[T any] struct {
	ReadOnly
	isPresent bool
	value     T
}

// Some creates an Option with a value.
func Some[T any](value T) Option[T] {
	return Option[T]{isPresent: true, value: value}
}

// None creates an Option without a value.
func None[T any]() (zero Option[T]) { return }

// Get unpacks the value from the Option, returning true if it was present.
func (o Option[T]) Get() (T, bool) {
	if o.isPresent {
		return o.value, true
	}
	return Zero[T](), false
}

// IsPresent checks if the Option contains a value.
func (o Option[T]) IsPresent() bool {
	return o.isPresent
}

// Or returns the value if present, otherwise returns the default value.
func (o *Option[T]) Or(def T) T {
	if o.isPresent {
		return o.value
	}
	return def
}

// MapOpt applies a function to the value if present, returning a new Option.
func MapOpt[T, R any](o Option[T], f func(T) R) Option[R] {
	if o.isPresent {
		return Some(f(o.value))
	}
	return None[R]()
}

// MarshalJSON implements the json.Marshaler interface.
// Note that it is defined on value, not pointer, because
// json.Marshal cannot call pointer methods on fields
// (i.e. it is broken by design).
func (o Option[T]) MarshalJSON() ([]byte, error) {
	if o.isPresent {
		return json.Marshal(o.value)
	}
	return []byte("null"), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (o *Option[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		o.isPresent = false
		return nil
	}
	if err := json.Unmarshal(data, &o.value); err != nil {
		return err
	}
	o.isPresent = true
	return nil
}
