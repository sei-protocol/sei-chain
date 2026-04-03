package enforce

import (
	"fmt"

	"golang.org/x/exp/constraints"
)

// If convenient, it's ok to add additional assertions to this collection, as long as those assertions are
// general purpose and not specific to a particular domain or use case. For example, don't import custom
// types or packages that are not part of the standard library or common Go ecosystem.

// Asserts a condition is true and panics with a message if the condition is false.
func True(condition bool, message string, args ...any) {
	if !condition {
		panic("Expected condition to be true: " + fmt.Sprintf(message, args...))
	}
}

// Asserts a condition is false and panics with an error message if the condition is true.
func False(condition bool, message string, args ...any) {
	if condition {
		panic("Expected condition to be false: " + fmt.Sprintf(message, args...))
	}
}

// Asserts that two values are equal and panics with an error if they are not.
func Equals[T comparable](expected T, actual T, message string, args ...any) {
	if expected != actual {
		panic(fmt.Sprintf("Expected equality, %v != %v: %s", expected, actual, fmt.Sprintf(message, args...)))
	}
}

// Asserts that two values are not equal and panics with an error if they are equal.
//
// May not behave as expected for NaN values in floating point comparisons.
func NotEquals[T comparable](notExpected T, actual T, message string, args ...any) {
	if notExpected == actual {
		panic(fmt.Sprintf("Expected inequality, %v == %v: %s", notExpected, actual,
			fmt.Sprintf(message, args...)))
	}
}

// Asserts a > b
//
// May not behave as expected for NaN values in floating point comparisons.
func GreaterThan[T constraints.Ordered](a T, b T, message string, args ...any) {
	if a <= b {
		panic(fmt.Sprintf("Expected %v > %v: %s", a, b, fmt.Sprintf(message, args...)))
	}
}

// Asserts a >= b
//
// May not behave as expected for NaN values in floating point comparisons.
func GreaterThanOrEqual[T constraints.Ordered](a T, b T, message string, args ...any) {
	if a < b {
		panic(fmt.Sprintf("Expected %v >= %v: %s", a, b, fmt.Sprintf(message, args...)))
	}
}

// Asserts a < b
//
// May not behave as expected for NaN values in floating point comparisons.
func LessThan[T constraints.Ordered](a T, b T, message string, args ...any) {
	if a >= b {
		panic(fmt.Sprintf("Expected %v < %v: %s", a, b, fmt.Sprintf(message, args...)))
	}
}

// Asserts a <= b
//
// May not behave as expected for NaN values in floating point comparisons.
func LessThanOrEqual[T constraints.Ordered](a T, b T, message string, args ...any) {
	if a > b {
		panic(fmt.Sprintf("Expected %v <= %v: %s", a, b, fmt.Sprintf(message, args...)))
	}
}

// Asserts that a value is not nil and panics with an error message if it is nil.
func NotNil[T any](value *T, message string, args ...any) {
	if value == nil {
		panic("Expected value to be not nil: " + fmt.Sprintf(message, args...))
	}
}

// Asserts that a value is nil and panics with an error message if it is not nil.
func Nil[T any](value *T, message string, args ...any) {
	if value != nil {
		panic("Expected value to be nil: " + fmt.Sprintf(message, args...))
	}
}

// Asserts that a slice is not empty and panics with an error message if it is empty.
func NotEmptyList[T any](list []T, message string, args ...any) {
	if len(list) == 0 {
		panic("Expected list to be not empty: " + fmt.Sprintf(message, args...))
	}
}

// Asserts that a string is not the empty string and panics with an error message if it is.
func NotEmptyString(value string, message string, args ...any) {
	if value == "" {
		panic("Expected string to be not empty: " + fmt.Sprintf(message, args...))
	}
}

// Asserts that a map is not empty and panics with an error message if it is empty.
func NotEmptyMap[K comparable, V any](m map[K]V, message string, args ...any) {
	if len(m) == 0 {
		panic("Expected map to be not empty: " + fmt.Sprintf(message, args...))
	}
}

// Asserts that a map contains a specific key and panics with an error message if it does not.
func MapContainsKey[K comparable, V any](m map[K]V, key K, message string, args ...any) {
	if _, ok := m[key]; !ok {
		panic(fmt.Sprintf("Expected map to contain key %v: %s", key, fmt.Sprintf(message, args...)))
	}
}

// Asserts that a map does not contain a specific key and panics with an error message if it does.
func MapDoesNotContainKey[K comparable, V any](m map[K]V, key K, message string, args ...any) {
	if _, ok := m[key]; ok {
		panic(fmt.Sprintf("Expected map to not contain key %v: %s", key, fmt.Sprintf(message, args...)))
	}
}

// Asserts that an error is nil and panics with a message if it is not nil.
func NilError(err error, message string, args ...any) {
	if err != nil {
		panic(fmt.Sprintf("Expected error to be nil but got '%v': %s", err, fmt.Sprintf(message, args...)))
	}
}
