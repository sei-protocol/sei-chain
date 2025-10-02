// Package require reexports strongly typed `testify/require` API.
// We don't reexport `New`, because methods cannot be generic.
package require

import (
	"cmp"

	"github.com/stretchr/testify/require"
)

// TestingT .
type TestingT = require.TestingT

// False .
var False = require.False

// True .
var True = require.True

// Zero .
var Zero = require.Zero

// NotZero .
var NotZero = require.NotZero

// Contains .
var Contains = require.Contains

func ElementsMatch[T any](t TestingT, a []T, b []T, msgAndArgs ...any) {
	require.ElementsMatch(t, a, b, msgAndArgs...)
}

// Eventually .
var Eventually = require.Eventually

// EqualError .
// TODO: get rid of comparing errors by strings,
// use concrete error types instead.
var EqualError = require.EqualError

// Error .
var Error = require.Error

// ErrorIs .
var ErrorIs = require.ErrorIs

// NoError .
var NoError = require.NoError

// Empty .
var Empty = require.Empty

// NotEmpty .
var NotEmpty = require.NotEmpty

// Len .
var Len = require.Len

// Nil .
var Nil = require.Nil

// NotNil .
var NotNil = require.NotNil

// Panics .
var Panics = require.Panics

// Fail .
var Fail = require.Fail

// Positive .
func Positive[T cmp.Ordered](t TestingT, e T, msgAndArgs ...any) {
	require.Positive(t, e, msgAndArgs...)
}

// Less .
func Less[T cmp.Ordered](t TestingT, e1, e2 T, msgAndArgs ...any) {
	require.Less(t, e1, e2, msgAndArgs...)
}

// LessOrEqual .
func LessOrEqual[T cmp.Ordered](t TestingT, e1, e2 T, msgAndArgs ...any) {
	require.LessOrEqual(t, e1, e2, msgAndArgs...)
}

// Greater .
func Greater[T cmp.Ordered](t TestingT, e1, e2 T, msgAndArgs ...any) {
	require.Greater(t, e1, e2, msgAndArgs...)
}

// GreaterOrEqual .
func GreaterOrEqual[T cmp.Ordered](t TestingT, e1, e2 T, msgAndArgs ...any) {
	require.GreaterOrEqual(t, e1, e2, msgAndArgs...)
}

// Equal .
func Equal[T any](t TestingT, expected, actual T, msgAndArgs ...any) {
	require.Equal(t, expected, actual, msgAndArgs...)
}

// NotEqual .
func NotEqual[T any](t TestingT, expected, actual T, msgAndArgs ...any) {
	require.NotEqual(t, expected, actual, msgAndArgs...)
}
