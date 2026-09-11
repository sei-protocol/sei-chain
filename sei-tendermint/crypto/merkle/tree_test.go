package merkle

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	ctest "github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/test"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
)

type testItem []byte

func (tI testItem) Hash() []byte {
	return []byte(tI)
}

func TestHashFromByteSlices(t *testing.T) {
	testcases := map[string]struct {
		slices     [][]byte
		expectHash string // in hex format
	}{
		"nil":          {nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		"empty":        {[][]byte{}, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		"single":       {[][]byte{{1, 2, 3}}, "054edec1d0211f624fed0cbca9d4f9400b0e491c43742af2c5b0abebf0c990d8"},
		"single blank": {[][]byte{{}}, "6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d"},
		"two":          {[][]byte{{1, 2, 3}, {4, 5, 6}}, "82e6cfce00453804379b53962939eaa7906b39904be0813fcadd31b100773c4b"},
		"many": {
			[][]byte{{1, 2}, {3, 4}, {5, 6}, {7, 8}, {9, 10}},
			"f326493eceab4f2d9ffbc78c59432a0a005d6ea98392045c74df5d14a113be18",
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			hash := HashFromByteSlices(tc.slices)
			assert.Equal(t, tc.expectHash, hex.EncodeToString(hash))
		})
	}
}

func TestProof(t *testing.T) {

	// Try an empty proof first
	rootHash, proofs := ProofsFromByteSlices([][]byte{})
	require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", hex.EncodeToString(rootHash))
	require.Empty(t, proofs)

	total := 100

	items := make([][]byte, total)
	for i := 0; i < total; i++ {
		items[i] = testItem(tmrand.Bytes(crypto.HashSize))
	}

	rootHash = HashFromByteSlices(items)

	rootHash2, proofs := ProofsFromByteSlices(items)

	require.Equal(t, rootHash, rootHash2, "Unmatched root hashes: %X vs %X", rootHash, rootHash2)

	// For each item, check the trail.
	for i, item := range items {
		proof := proofs[i]

		// Check total/index
		require.EqualValues(t, proof.Index, i, "Unmatched indicies: %d vs %d", proof.Index, i)

		require.EqualValues(t, proof.Total, total, "Unmatched totals: %d vs %d", proof.Total, total)

		// Verify success
		err := proof.Verify(rootHash, item)
		require.NoError(t, err, "Verification failed: %v.", err)

		// Trail too long should make it fail
		origAunts := proof.Aunts
		proof.Aunts = append(proof.Aunts, tmrand.Bytes(32))
		err = proof.Verify(rootHash, item)
		require.Error(t, err, "Expected verification to fail for wrong trail length")

		proof.Aunts = origAunts

		// Trail too short should make it fail
		proof.Aunts = proof.Aunts[0 : len(proof.Aunts)-1]
		err = proof.Verify(rootHash, item)
		require.Error(t, err, "Expected verification to fail for wrong trail length")

		proof.Aunts = origAunts

		// Mutating the itemHash should make it fail.
		err = proof.Verify(rootHash, ctest.MutateByteSlice(item))
		require.Error(t, err, "Expected verification to fail for mutated leaf hash")

		// Mutating the rootHash should make it fail.
		err = proof.Verify(ctest.MutateByteSlice(rootHash), item)
		require.Error(t, err, "Expected verification to fail for mutated root hash")
	}
}

func TestHashAlternatives(t *testing.T) {

	total := 100

	items := make([][]byte, total)
	for i := 0; i < total; i++ {
		items[i] = testItem(tmrand.Bytes(crypto.HashSize))
	}

	rootHash1 := HashFromByteSlicesIterative(items)
	rootHash2 := HashFromByteSlices(items)
	require.Equal(t, rootHash1, rootHash2, "Unmatched root hashes: %X vs %X", rootHash1, rootHash2)
}

// See https://blog.verichains.io/p/vsa-2022-100-tendermint-forging-membership-proof?utm_source=substack&utm_campaign=post_embed&utm_medium=web
// for context
func TestForgeEmptyMerkleTreeAttack(t *testing.T) {
	key := []byte{0x13}
	value := []byte{0x37}
	vhash := tmhash.Sum(value)
	bz := new(bytes.Buffer)
	_ = EncodeByteSlice(bz, key)
	_ = EncodeByteSlice(bz, vhash)
	kvhash := tmhash.Sum(append([]byte{0}, bz.Bytes()...))
	op := NewValueOp(key, &Proof{LeafHash: kvhash})
	var root []byte
	err := ProofOperators{op}.Verify(root, "/"+string(key), [][]byte{value})
	// Must return error or else the attack would be possible
	require.NotNil(t, err)
}

func BenchmarkHashAlternatives(b *testing.B) {
	total := 100

	items := make([][]byte, total)
	for i := 0; i < total; i++ {
		items[i] = testItem(tmrand.Bytes(crypto.HashSize))
	}

	b.ResetTimer()
	b.Run("recursive", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = HashFromByteSlices(items)
		}
	})

	b.Run("iterative", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = HashFromByteSlicesIterative(items)
		}
	})
}

// TestHashFromByteSlicesBatched checks the level-batched tree against the
// recursive reference around every lane-count boundary.
func TestHashFromByteSlicesBatched(t *testing.T) {
	sha := sha256.New()
	for _, size := range []int{2, 32, 128} {
		for total := 1; total <= 70; total++ {
			items := make([][]byte, total)
			for i := range items {
				items[i] = tmrand.Bytes(size)
			}
			require.Equal(t, hashFromByteSlices(sha, items), hashFromByteSlicesBatched(items), "size=%d total=%d", size, total)
		}
	}
}

// BenchmarkHashFromByteSlices measures the whole tree for tx-hash sized and
// tx sized leaves. The sub-benchmark is named after the active tmhash
// backend so runs under different SEI_TMHASH_BACKEND values can be compared
// with benchstat.
func BenchmarkHashFromByteSlices(b *testing.B) {
	for _, tc := range []struct {
		name  string
		total int
		size  int
	}{
		{"leaves=1024/leaf=32", 1024, 32},
		{"leaves=1024/leaf=512", 1024, 512},
		{"leaves=100/leaf=32", 100, 32},
	} {
		items := make([][]byte, tc.total)
		for i := range items {
			items[i] = tmrand.Bytes(tc.size)
		}
		b.Run(tc.name+"/backend="+tmhash.ActiveBackend(), func(b *testing.B) {
			b.SetBytes(int64(tc.total * tc.size))
			for b.Loop() {
				_ = HashFromByteSlices(items)
			}
		})
	}
}

// BenchmarkHashFromByteSlicesBatched forces the level-batched tree so that,
// pinned to the default backend, it isolates the restructuring from the SIMD
// kernel.
func BenchmarkHashFromByteSlicesBatched(b *testing.B) {
	items := make([][]byte, 1024)
	for i := range items {
		items[i] = tmrand.Bytes(32)
	}
	b.Run("leaves=1024/leaf=32/backend="+tmhash.ActiveBackend(), func(b *testing.B) {
		b.SetBytes(int64(len(items) * 32))
		for b.Loop() {
			_ = hashFromByteSlicesBatched(items)
		}
	})
}

func Test_getSplitPoint(t *testing.T) {
	tests := []struct {
		length int64
		want   int64
	}{
		{1, 0},
		{2, 1},
		{3, 2},
		{4, 2},
		{5, 4},
		{10, 8},
		{20, 16},
		{100, 64},
		{255, 128},
		{256, 128},
		{257, 256},
	}
	for _, tt := range tests {
		got := getSplitPoint(tt.length)
		require.EqualValues(t, tt.want, got, "getSplitPoint(%d) = %v, want %v", tt.length, got, tt.want)
	}
}

func EncodeUvarint(w io.Writer, u uint64) (err error) {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], u)
	_, err = w.Write(buf[0:n])
	return
}

func EncodeByteSlice(w io.Writer, bz []byte) (err error) {
	err = EncodeUvarint(w, uint64(len(bz)))
	if err != nil {
		return
	}
	_, err = w.Write(bz)
	return
}
