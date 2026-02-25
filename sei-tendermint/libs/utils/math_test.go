package utils

import (
	"math"
	"testing"
	"unsafe"

	"golang.org/x/exp/constraints"
)

func runBitsTest[T constraints.Integer](t *testing.T, name string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		var zero T
		want := uintptr(unsafe.Sizeof(zero)) * 8
		if got := Bits[T](); got != want {
			t.Fatalf("Bits[%s]() = %d, want %d", name, got, want)
		}
	})
}

func runMaxMinTest[T constraints.Integer](t *testing.T, name string, wantMax, wantMin T) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if got := Max[T](); got != wantMax {
			t.Fatalf("Max[%s]() = %v, want %v", name, got, wantMax)
		}
		if got := Min[T](); got != wantMin {
			t.Fatalf("Min[%s]() = %v, want %v", name, got, wantMin)
		}
	})
}

func assertSafeCast[To, From constraints.Integer](t *testing.T, name string, v From, want To, wantOK bool) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		got, ok := SafeCast[To](v)
		if ok != wantOK {
			t.Fatalf("SafeCast[%s] ok = %v, want %v", name, ok, wantOK)
		}
		if wantOK && got != want {
			t.Fatalf("SafeCast[%s] = %v, want %v", name, got, want)
		}
	})
}

func runSafeCastIdentity[T constraints.Integer](t *testing.T, name string, value T) {
	assertSafeCast[T, T](t, name, value, value, true)
}

func assertClamp[To, From constraints.Integer](t *testing.T, name string, v From, want To) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if got := Clamp[To](v); got != want {
			t.Fatalf("Clamp[%s] = %v, want %v", name, got, want)
		}
	})
}

func TestBits(t *testing.T) {
	runBitsTest[int](t, "int")
	runBitsTest[int8](t, "int8")
	runBitsTest[int16](t, "int16")
	runBitsTest[int32](t, "int32")
	runBitsTest[int64](t, "int64")
	runBitsTest[uint](t, "uint")
	runBitsTest[uint8](t, "uint8")
	runBitsTest[uint16](t, "uint16")
	runBitsTest[uint32](t, "uint32")
	runBitsTest[uint64](t, "uint64")
	runBitsTest[uintptr](t, "uintptr")
}

func TestMaxMin(t *testing.T) {
	runMaxMinTest[int](t, "int", int(math.MaxInt), int(math.MinInt))
	runMaxMinTest[int8](t, "int8", math.MaxInt8, math.MinInt8)
	runMaxMinTest[int16](t, "int16", math.MaxInt16, math.MinInt16)
	runMaxMinTest[int32](t, "int32", math.MaxInt32, math.MinInt32)
	runMaxMinTest[int64](t, "int64", math.MaxInt64, math.MinInt64)

	runMaxMinTest[uint](t, "uint", uint(math.MaxUint), 0)
	runMaxMinTest[uint8](t, "uint8", math.MaxUint8, 0)
	runMaxMinTest[uint16](t, "uint16", math.MaxUint16, 0)
	runMaxMinTest[uint32](t, "uint32", math.MaxUint32, 0)
	runMaxMinTest[uint64](t, "uint64", math.MaxUint64, 0)
	runMaxMinTest[uintptr](t, "uintptr", ^uintptr(0), 0)
}

func TestSafeCast(t *testing.T) {
	// Identity casts at the boundaries for every integer type.
	runSafeCastIdentity[int](t, "int/min", int(math.MinInt))
	runSafeCastIdentity[int](t, "int/max", int(math.MaxInt))
	runSafeCastIdentity[int8](t, "int8/min", math.MinInt8)
	runSafeCastIdentity[int8](t, "int8/max", math.MaxInt8)
	runSafeCastIdentity[int16](t, "int16/min", math.MinInt16)
	runSafeCastIdentity[int16](t, "int16/max", math.MaxInt16)
	runSafeCastIdentity[int32](t, "int32/min", math.MinInt32)
	runSafeCastIdentity[int32](t, "int32/max", math.MaxInt32)
	runSafeCastIdentity[int64](t, "int64/min", math.MinInt64)
	runSafeCastIdentity[int64](t, "int64/max", math.MaxInt64)

	runSafeCastIdentity[uint](t, "uint/max", uint(math.MaxUint))
	runSafeCastIdentity[uint](t, "uint/zero", 0)
	runSafeCastIdentity[uint8](t, "uint8/max", math.MaxUint8)
	runSafeCastIdentity[uint8](t, "uint8/zero", 0)
	runSafeCastIdentity[uint16](t, "uint16/max", math.MaxUint16)
	runSafeCastIdentity[uint16](t, "uint16/zero", 0)
	runSafeCastIdentity[uint32](t, "uint32/max", math.MaxUint32)
	runSafeCastIdentity[uint32](t, "uint32/zero", 0)
	runSafeCastIdentity[uint64](t, "uint64/max", math.MaxUint64)
	runSafeCastIdentity[uint64](t, "uint64/zero", 0)
	runSafeCastIdentity[uintptr](t, "uintptr/max", ^uintptr(0))
	runSafeCastIdentity[uintptr](t, "uintptr/zero", 0)

	// Successful cross-type casts.
	assertSafeCast[int16, int8](t, "int8->int16", math.MinInt8, math.MinInt8, true)
	assertSafeCast[int32, int16](t, "int16->int32", math.MaxInt16, math.MaxInt16, true)
	assertSafeCast[int64, int32](t, "int32->int64", math.MinInt32, math.MinInt32, true)
	assertSafeCast[int64, uint32](t, "uint32->int64", math.MaxUint32, math.MaxUint32, true)
	assertSafeCast[uint32, uint16](t, "uint16->uint32", math.MaxUint16, math.MaxUint16, true)
	assertSafeCast[uint64, uint32](t, "uint32->uint64", math.MaxUint32, math.MaxUint32, true)
	assertSafeCast[uint, uint32](t, "uint32->uint", math.MaxUint32, uint(math.MaxUint32), true)
	assertSafeCast[uintptr, uint32](t, "uint32->uintptr", math.MaxUint32, uintptr(math.MaxUint32), true)
	assertSafeCast[int64, uint64](t, "uint64->int64 max", uint64(math.MaxInt64), math.MaxInt64, true)

	// Overflow and sign-mismatch detection.
	assertSafeCast[int8, int16](t, "int16->int8 overflow+", int16(math.MaxInt8)+1, 0, false)
	assertSafeCast[int16, int32](t, "int32->int16 overflow+", int32(math.MaxInt16)+1, 0, false)
	assertSafeCast[int32, int64](t, "int64->int32 overflow+", int64(math.MaxInt32)+1, 0, false)
	assertSafeCast[int64, uint64](t, "uint64->int64 overflow", uint64(math.MaxInt64)+1, 0, false)
	assertSafeCast[uint8, int8](t, "int8->uint8 negative", math.MinInt8, 0, false)
	assertSafeCast[uint16, int16](t, "int16->uint16 negative", math.MinInt16, 0, false)
	assertSafeCast[uint32, int32](t, "int32->uint32 negative", math.MinInt32, 0, false)
	assertSafeCast[uint64, int64](t, "int64->uint64 negative", math.MinInt64, 0, false)
	assertSafeCast[int, uint64](t, "uint64->int overflow", math.MaxUint64, 0, false)
	assertSafeCast[uint, int64](t, "int64->uint negative", math.MinInt64, 0, false)
	assertSafeCast[uintptr, int64](t, "int64->uintptr negative", math.MinInt64, 0, false)
	assertSafeCast[uint8, uint16](t, "uint16->uint8 overflow", uint16(math.MaxUint8)+1, 0, false)
}

func TestClamp(t *testing.T) {
	assertClamp[int](t, "int/high", uint64(math.MaxUint64), int(math.MaxInt))
	assertClamp[int](t, "int/low", int64(math.MinInt64), int(math.MinInt))

	assertClamp[int8](t, "int8/high", int16(math.MaxInt8)+1, math.MaxInt8)
	assertClamp[int8](t, "int8/low", int16(math.MinInt8)-1, math.MinInt8)

	assertClamp[int16](t, "int16/high", int32(math.MaxInt16)+1, math.MaxInt16)
	assertClamp[int16](t, "int16/low", int32(math.MinInt16)-1, math.MinInt16)
	assertClamp[int16](t, "int16/in-range", int32(12345), 12345)

	assertClamp[int32](t, "int32/high", int64(math.MaxInt64), math.MaxInt32)
	assertClamp[int32](t, "int32/low", int64(math.MinInt64), math.MinInt32)

	assertClamp[int64](t, "int64/high", uint64(math.MaxUint64), math.MaxInt64)
	assertClamp[int64](t, "int64/in-range", int64(-123456789), -123456789)

	assertClamp[uint](t, "uint/low", int64(-1), 0)
	assertClamp[uint8](t, "uint8/high", int16(math.MaxUint8)+1, math.MaxUint8)
	assertClamp[uint8](t, "uint8/low", int16(-1), 0)
	assertClamp[uint16](t, "uint16/high", int32(1<<20), math.MaxUint16)
	assertClamp[uint16](t, "uint16/in-range", int32(60000), 60000)
	assertClamp[uint32](t, "uint32/high", int64(math.MaxInt64), math.MaxUint32)
	assertClamp[uint32](t, "uint32/in-range", uint64(math.MaxUint32), math.MaxUint32)
	assertClamp[uint64](t, "uint64/low", int64(-12345), 0)
	assertClamp[uint64](t, "uint64/in-range", uint64(math.MaxUint64-1), math.MaxUint64-1)

	assertClamp[uintptr](t, "uintptr/low", int64(-1), 0)
	assertClamp[uintptr](t, "uintptr/in-range", uintptr(^uintptr(0)), ^uintptr(0))
}
