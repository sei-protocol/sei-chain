//go:build !jemalloc || !cgo

package jemalloc

// Enabled reports whether this binary was built with the jemalloc build tag.
const Enabled = false

// Version returns "" because no jemalloc is linked into this binary.
func Version() string { return "" }

// Allocated returns 0 because no jemalloc is linked into this binary.
func Allocated() uint64 { return 0 }
