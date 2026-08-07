package config

// NativeTraceTracers exposes the allowlist IsNativeTraceTracer answers from, so a test can hold every
// entry to being non-JS in geth rather than restating the set beside it.
//
// This file is compiled only under test, so nothing here widens the package's shipped surface.
//
// A function rather than a var. A package-level var is initialised before any init runs and would
// capture the map reference at that moment, so reassigning nativeTraceTracers later would leave the
// test asserting over the old map while the accessor answered from the new one. Returning it on each
// call keeps the test reading whatever IsNativeTraceTracer reads.
func NativeTraceTracers() map[string]struct{} { return nativeTraceTracers }
