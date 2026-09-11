package lthash

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"

	"github.com/zeebo/blake3"
)

// referenceExpand is the specification every backend must match: the first
// 2048 bytes of the Blake3 XOF, read as little-endian uint16 limbs.
func referenceExpand(data []byte) *LtHash {
	var out [LtHashBytes]byte
	h := blake3.New()
	_, _ = h.Write(data)
	_, _ = h.Digest().Read(out[:])
	lth := New()
	for i := range lth.limbs {
		lth.limbs[i] = binary.LittleEndian.Uint16(out[2*i:])
	}
	return lth
}

// expandSizes covers block and chunk boundaries of Blake3, including the
// multi-chunk tree path that the SIMD backend delegates.
var expandSizes = []int{1, 8, 63, 64, 65, 124, 127, 128, 129, 500, 1023, 1024, 1025, 2048, 4096, 5000}

func TestBackendsAgreeWithReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for name, b := range availableBackends() {
		t.Run(name, func(t *testing.T) {
			for _, n := range expandSizes {
				for iter := 0; iter < 8; iter++ {
					data := make([]byte, n)
					rng.Read(data)
					got := New()
					b.expand(data, got)
					if want := referenceExpand(data); !got.Equal(want) {
						t.Fatalf("expand(%d bytes) differs from the Blake3 reference", n)
					}
				}
			}
		})
	}
}

func TestBackendsAgreeOnMix(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	x, y := New(), New()
	for i := range x.limbs {
		x.limbs[i] = uint16(rng.Uint32())
		y.limbs[i] = uint16(rng.Uint32())
	}
	wantAdd, wantSub := x.Clone(), x.Clone()
	addScalar(wantAdd, y)
	subScalar(wantSub, y)
	for name, b := range availableBackends() {
		t.Run(name, func(t *testing.T) {
			gotAdd, gotSub := x.Clone(), x.Clone()
			b.add(gotAdd, y)
			b.sub(gotSub, y)
			if !gotAdd.Equal(wantAdd) {
				t.Fatal("add differs from scalar")
			}
			if !gotSub.Equal(wantSub) {
				t.Fatal("sub differs from scalar")
			}
		})
	}
}

func TestSelectBackend(t *testing.T) {
	if got := selectBackend("default").name; got != "default" {
		t.Fatalf("pinning default selected %q", got)
	}
	want := "default"
	if simd, ok := simdBackend(); ok {
		want = simd.name
	}
	if got := selectBackend("").name; got != want {
		t.Fatalf("automatic selection picked %q, want %q", got, want)
	}
	if got := selectBackend("no-such-backend").name; got != selectBackend("").name {
		t.Fatalf("unknown pin %q should fall back to automatic selection", got)
	}
	if _, ok := availableBackends()[ActiveBackend()]; !ok {
		t.Fatalf("active backend %q is not available", ActiveBackend())
	}
}

// Benchmarks are keyed by backend so `benchstat -col /backend` places the
// implementations side by side.

func benchmarkKV() []byte {
	rng := rand.New(rand.NewSource(3))
	key := make([]byte, 40)
	value := make([]byte, 76)
	rng.Read(key)
	rng.Read(value)
	return serializeKV(key, value)
}

func forEachBackend(b *testing.B, fn func(b *testing.B, be backend)) {
	all := availableBackends()
	for _, name := range availableBackendNames() {
		be := all[name]
		b.Run(fmt.Sprintf("backend=%s", name), func(b *testing.B) { fn(b, be) })
	}
}

func BenchmarkExpand(b *testing.B) {
	data := benchmarkKV()
	forEachBackend(b, func(b *testing.B, be backend) {
		dst := New()
		b.SetBytes(LtHashBytes)
		for i := 0; i < b.N; i++ {
			be.expand(data, dst)
		}
	})
}

func BenchmarkMixIn(b *testing.B) {
	forEachBackend(b, func(b *testing.B, be backend) {
		x, y := New(), New()
		for i := 0; i < b.N; i++ {
			be.add(x, y)
		}
	})
}

func BenchmarkMixOut(b *testing.B) {
	forEachBackend(b, func(b *testing.B, be backend) {
		x, y := New(), New()
		for i := 0; i < b.N; i++ {
			be.sub(x, y)
		}
	})
}

// BenchmarkHashKV is one leaf update as foldChunk performs it: expand the
// serialized pair and fold it into an accumulator.
func BenchmarkHashKV(b *testing.B) {
	data := benchmarkKV()
	forEachBackend(b, func(b *testing.B, be backend) {
		acc, h := New(), New()
		for i := 0; i < b.N; i++ {
			be.expand(data, h)
			be.add(acc, h)
		}
	})
}

// BenchmarkFoldChunk runs the full mutation pipeline through the active
// backend, switching the active backend for each sub-benchmark.
func BenchmarkFoldChunk(b *testing.B) {
	rng := rand.New(rand.NewSource(4))
	mutations := make([]KVPairWithLastValue, 1000)
	for i := range mutations {
		key := make([]byte, 40)
		last := make([]byte, 76)
		value := make([]byte, 76)
		rng.Read(key)
		rng.Read(last)
		rng.Read(value)
		mutations[i] = KVPairWithLastValue{Key: key, LastValue: last, Value: value}
	}
	saved := active
	defer func() { active = saved }()
	forEachBackend(b, func(b *testing.B, be backend) {
		active = be
		for i := 0; i < b.N; i++ {
			foldChunk(mutations)
		}
	})
}
