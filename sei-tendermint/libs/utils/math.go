package utils

import (
	"unsafe"

	"golang.org/x/exp/constraints"
)

// Bits returns the number of bits of the given integer type.
func Bits[I constraints.Integer]() uintptr {
	return unsafe.Sizeof(I(0)) * 8
}

// Max returns the maximal value of the given integer type.
func Max[I constraints.Integer]() I { return ^Min[I]() }

// Returns true iff I is a signed integer type.
func Signed[I constraints.Integer]() bool { return ^I(0) < 0 }

// Min returns the minimal value of the given integer type.
func Min[I constraints.Integer]() I {
	if Signed[I]() {
		return I(1) << (Bits[I]() - 1)
	}
	return 0
}

// SafeCast casts between integer types, checking for overflows.
func SafeCast[To, From constraints.Integer](v From) (x To, ok bool) {
	x = To(v)
	// This can be further optimized by:
	// * making compiler detect if From -> To conversion is always safe
	// * making compiler detect if the parity check is necessary
	ok = From(x) == v && (x < 0) == (v < 0)
	return
}

// Clamp converts an integer to another integer type clamping it to the target types' [min,max] range
// in case of overflow.
func Clamp[To, From constraints.Integer](v From) To {
	if x, ok := SafeCast[To](v); ok {
		return x
	}
	if v >= 0 {
		return Max[To]()
	}
	return Min[To]()
}
