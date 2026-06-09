package utils

// OrPanic panics if err is non-nil. Use for initialization-time or otherwise
// unrecoverable failures where returning an error is not an option (e.g. var
// initializers, metric instrument creation).
func OrPanic(err error) {
	if err != nil {
		panic(err)
	}
}

// OrPanic1 returns v, panicking if err is non-nil. Convenience for wrapping a
// (value, error) call in a var initializer that cannot fail at runtime.
func OrPanic1[T any](v T, err error) T {
	OrPanic(err)
	return v
}
