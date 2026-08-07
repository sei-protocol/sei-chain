package config

// NativeTraceTracers exposes the allowlist IsNativeTraceTracer answers from, so a test can hold every
// entry to being non-JS in geth rather than restating the set beside it.
//
// This file is compiled only under test, so nothing here widens the package's shipped surface.
var NativeTraceTracers = nativeTraceTracers
