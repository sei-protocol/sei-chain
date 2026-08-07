package config

import "maps"

// NativeTraceTracers exposes the allowlist IsNativeTraceTracer answers from, so a test can hold every
// entry to being non-JS in geth rather than restating the set beside it.
//
// This file is compiled only under test, so nothing here widens the package's shipped surface.
//
// A function rather than a var. A package-level var is initialised before any init runs and would
// capture the map reference at that moment, so reassigning nativeTraceTracers later would leave a test
// asserting over the old map while the accessor answered from the new one. Reading it on each call
// keeps a test looking at whatever IsNativeTraceTracer looks at.
//
// A copy rather than the live map. Tests in a package share process state, so handing back the real
// allowlist would let any one of them mutate what every other test resolves against, and the mutation
// would surface somewhere else entirely. Copying on each call keeps both properties: a reassignment is
// still visible, and the set cannot be edited through this.
func NativeTraceTracers() map[string]struct{} {
	return maps.Clone(nativeTraceTracers)
}
