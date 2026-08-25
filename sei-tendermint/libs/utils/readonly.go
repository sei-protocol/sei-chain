package utils

import (
	"reflect"
)

// ReadOnly marks a struct so TestEqual compares its private fields.
type ReadOnly struct{}

// isReadOnly reports whether t embeds ReadOnly.
func isReadOnly(t reflect.Type) bool {
	want := reflect.TypeFor[ReadOnly]()
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		if f := t.Field(i); f.Anonymous || f.Type == want {
			return true
		}
	}
	return false
}
