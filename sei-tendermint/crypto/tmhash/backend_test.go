package tmhash

import (
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// batchSizes covers SHA-256 block boundaries with and without the one-byte
// prefix, the 65-byte Merkle inner node and a few multi-block messages.
var batchSizes = []int{0, 1, 31, 32, 54, 55, 56, 63, 64, 65, 100, 118, 119, 120, 127, 128, 129, 200, 1000, 4096, 5000}

func referenceSum(prefix, msg []byte) [Size]byte {
	return sha256.Sum256(slices.Concat(prefix, msg))
}

func randomMsgs(rng *rand.Rand, n, size int) [][]byte {
	msgs := make([][]byte, n)
	for i := range msgs {
		msgs[i] = make([]byte, size)
		for j := range msgs[i] {
			msgs[i][j] = byte(rng.UintN(256))
		}
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
						rng := rand.New(rand.NewPCG(uint64(size), uint64(n)))
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

func TestBackendsAgreeOnMixedSizes(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	msgs := make([][]byte, 200)
	for i := range msgs {
		msgs[i] = randomMsgs(rng, 1, int(rng.UintN(600)))[0]
	}
	for _, name := range availableBackendNames() {
		out := make([][Size]byte, len(msgs))
		availableBackends()[name].sumBatch([]byte{0}, msgs, out)
		for i, msg := range msgs {
			require.Equal(t, referenceSum([]byte{0}, msg), out[i])
		}
	}
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

func benchmarkSumBatch(b *testing.B, size, n int) {
	rng := rand.New(rand.NewPCG(3, 4))
	msgs := randomMsgs(rng, n, size)
	out := make([][Size]byte, n)
	prefix := []byte{0}
	for _, name := range availableBackendNames() {
		be := availableBackends()[name]
		b.Run("backend="+name, func(b *testing.B) {
			b.SetBytes(int64(n * (size + 1)))
			for b.Loop() {
				be.sumBatch(prefix, msgs, out)
			}
		})
	}
}

// BenchmarkSumBatchInner is a Merkle inner-node level: 1024 x 64-byte
// messages behind a one-byte prefix.
func BenchmarkSumBatchInner(b *testing.B) { benchmarkSumBatch(b, 64, 1024) }

// BenchmarkSumBatchLeaf256 is a leaf level of 1024 x 256-byte items.
func BenchmarkSumBatchLeaf256(b *testing.B) { benchmarkSumBatch(b, 256, 1024) }

// BenchmarkSumBatchLeaf1K is a leaf level of 1024 x 1 KiB items.
func BenchmarkSumBatchLeaf1K(b *testing.B) { benchmarkSumBatch(b, 1024, 1024) }
