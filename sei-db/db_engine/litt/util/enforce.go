//go:build littdb_wip

package util

import "fmt"

// True asserts a condition is true and panics with a message if the condition is false.
func True(condition bool, message string, args ...any) {
	if !condition {
		panic("Expected condition to be true: " + fmt.Sprintf(message, args...))
	}
}

// MapDoesNotContainKey asserts that a map does not contain a specific key and panics
// with an error message if it does.
func MapDoesNotContainKey[K comparable, V any](m map[K]V, key K, message string, args ...any) {
	if _, ok := m[key]; ok {
		panic(fmt.Sprintf("Expected map to not contain key %v: %s", key, fmt.Sprintf(message, args...)))
	}
}
