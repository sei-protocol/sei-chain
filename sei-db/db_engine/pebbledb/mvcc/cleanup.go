package mvcc

// cleanupOnError runs cleanup functions only if *errp is non-nil.
// This is a small helper to keep resource-lifecycle code consistent and hard to forget.
//
// Execution order:
// - cleanups are executed in reverse order (LIFO), like multiple `defer` statements.
// - so pass cleanups in the same order you would `defer` them (the “last” cleanup runs first).
func cleanupOnError(errp *error, cleanups ...func()) {
	if errp == nil || *errp == nil {
		return
	}
	// Run in reverse order, similar to defer stacking.
	for i := len(cleanups) - 1; i >= 0; i-- {
		if cleanups[i] != nil {
			cleanups[i]()
		}
	}
}
