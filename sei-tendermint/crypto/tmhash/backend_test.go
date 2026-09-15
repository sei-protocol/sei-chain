package tmhash

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// batchSizes covers SHA-256 block boundaries with and without the one-byte
// prefix, the 65-byte Merkle inner node and a few multi-block messages.
var batchSizes = []int{0, 1, 31, 32, 54, 55, 56, 63, 64, 65, 100, 118, 119, 120, 127, 128, 129, 200, 1000, 4096, 5000}

func availableBackendNames() []string {
	return slices.Sorted(maps.Keys(availableBackends()))
}

func referenceSum(prefix, msg []byte) [Size]byte {
	return sha256.Sum256(slices.Concat(prefix, msg))
}

func randomMsgs(rng utils.Rng, n, size int) [][]byte {
	msgs := make([][]byte, n)
	for i := range msgs {
		msgs[i] = utils.GenBytes(rng, size)
	}
	return msgs
}

func TestBackendsAgreeWithReference(t *testing.T) {
	for _, name := range availableBackendNames() {
		b := availableBackends()[name]
		for _, prefix := range [][]byte{nil, {0}, {1}} {
			for _, size := range batchSizes {
				// One partial batch, one exact multiple, one with a remainder.
				for _, n := range []int{1, 15, 16, 32, 37} {
					t.Run(fmt.Sprintf("%s/prefix=%d/size=%d/n=%d", name, len(prefix), size, n), func(t *testing.T) {
						rng := utils.TestRng()
						msgs := randomMsgs(rng, n, size)
						out := make([][Size]byte, n)
						b.sumBatch(prefix, msgs, out)
						for i, msg := range msgs {
							require.Equal(t, referenceSum(prefix, msg), out[i])
						}
					})
				}
			}
		}
	}
}

// checkBatch hashes msgs with every available backend and compares each
// digest with crypto/sha256.
func checkBatch(t testing.TB, prefix []byte, msgs [][]byte) {
	for _, name := range availableBackendNames() {
		out := make([][Size]byte, len(msgs))
		availableBackends()[name].sumBatch(prefix, msgs, out)
		for i, msg := range msgs {
			require.Equal(t, referenceSum(prefix, msg), out[i], "%s msg %d len %d", name, i, len(msg))
		}
	}
}

// TestBackendsAgreeOnMixedSizes mixes, in one call, sizes that fill SIMD
// lanes, sizes that land in the smaller buckets and a few messages beyond
// the SIMD kernel's block limit.
func TestBackendsAgreeOnMixedSizes(t *testing.T) {
	rng := utils.TestRng()
	msgs := make([][]byte, 200)
	for i := range msgs {
		size := rng.Intn(600)
		if rng.Intn(20) == 0 {
			size = 4096 + rng.Intn(2000)
		}
		msgs[i] = utils.GenBytes(rng, size)
	}
	checkBatch(t, []byte{0}, msgs)
}

// FuzzSumBatch drives every backend with an arbitrary prefix and message
// length list against crypto/sha256. Each byte of lens is one message whose
// length is the byte value scaled by 24, so that lengths span from 0 to
// beyond the SIMD kernel's block limit.
func FuzzSumBatch(f *testing.F) {
	f.Add([]byte{0}, []byte{2, 3, 200, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3})
	f.Add([]byte{}, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255})
	f.Fuzz(func(t *testing.T, prefix, lens []byte) {
		if len(prefix) > 63 {
			prefix = prefix[:63]
		}
		rng := utils.TestRng()
		msgs := make([][]byte, len(lens))
		for i, l := range lens {
			msgs[i] = utils.GenBytes(rng, int(l)*24)
		}
		checkBatch(t, prefix, msgs)
	})
}

func TestSelectBackend(t *testing.T) {
	require.Equal(t, "default", selectBackend("default").name)
	require.Equal(t, 1, selectBackend("default").lanes)
	auto := selectBackend("")
	require.Equal(t, auto.name, selectBackend("unknown").name)
	if simd, ok := simdBackend(); ok {
		require.Equal(t, simd.name, auto.name)
		require.Equal(t, simd.name, selectBackend(simd.name).name)
	} else {
		require.Equal(t, "default", auto.name)
	}
	require.True(t, slices.Contains(availableBackendNames(), ActiveBackend()))
}

// benchmarkSumBatch measures the active backend only, named after it so runs
// under different SEI_TMHASH_BACKEND values or builds compare with benchstat
// without one build's scalar samples polluting the other's column.
func benchmarkSumBatch(b *testing.B, size, n int) {
	msgs := randomMsgs(utils.TestRng(), n, size)
	out := make([][Size]byte, n)
	prefix := []byte{0}
	b.Run("backend="+ActiveBackend(), func(b *testing.B) {
		b.SetBytes(int64(n * (size + 1)))
		for b.Loop() {
			SumBatch(prefix, msgs, out)
		}
	})
}

// BenchmarkSumBatchInner is a Merkle inner-node level: 1024 x 64-byte
// messages behind a one-byte prefix.
func BenchmarkSumBatchInner(b *testing.B) { benchmarkSumBatch(b, 64, 1024) }

// BenchmarkSumBatchLeaf256 is a leaf level of 1024 x 256-byte items.
func BenchmarkSumBatchLeaf256(b *testing.B) { benchmarkSumBatch(b, 256, 1024) }

// BenchmarkSumBatchLeaf1K is a leaf level of 1024 x 1 KiB items.
func BenchmarkSumBatchLeaf1K(b *testing.B) { benchmarkSumBatch(b, 1024, 1024) }
