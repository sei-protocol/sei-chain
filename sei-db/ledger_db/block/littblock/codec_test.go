package littblock

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestKeyRoundTrip(t *testing.T) {
	cases := []types.GlobalBlockNumber{
		0,
		1,
		42,
		255,
		256,
		1 << 32,
		^types.GlobalBlockNumber(0), // max uint64
	}
	for _, n := range cases {
		key := encodeKey(n)
		require.Len(t, key, 8, "key must be 8 bytes")
		require.Equal(t, n, decodeKey(key))
	}
}

func TestKeyBigEndianOrdering(t *testing.T) {
	// Lexicographic byte order must match numeric order so LittDB's
	// insertion/range semantics line up with block numbers.
	pairs := [][2]types.GlobalBlockNumber{
		{0, 1},
		{1, 2},
		{255, 256},
		{1 << 16, 1 << 32},
	}
	for _, p := range pairs {
		require.Negative(t, bytes.Compare(encodeKey(p[0]), encodeKey(p[1])),
			"encodeKey(%d) should sort before encodeKey(%d)", p[0], p[1])
	}
}

func TestPrefixedKeys(t *testing.T) {
	// Block and QC number keys carry distinct kind prefixes, so the same number
	// never collides across the two record kinds in the shared table.
	n := types.GlobalBlockNumber(42)
	bk := blockKey(n)
	qk := qcKey(n)
	require.Len(t, bk, 9, "block key is 1 prefix byte + 8 number bytes")
	require.Len(t, qk, 9, "qc key is 1 prefix byte + 8 number bytes")
	require.Equal(t, kindBlock, keyKind(bk))
	require.Equal(t, kindQC, keyKind(qk))
	require.NotEqual(t, bk, qk, "same number must not collide across kinds")
	require.Equal(t, n, decodeNumberKey(bk))
	require.Equal(t, n, decodeNumberKey(qk))

	// The header-hash alias has its own kind and round-trips the 32-byte hash.
	hash := types.GenBlockHeaderHash(utils.TestRngFromSeed(7))
	hk := blockHashKey(hash)
	require.Equal(t, kindBlockHash, keyKind(hk))
	require.Len(t, hk, 1+len(hash.Bytes()))
	require.Equal(t, hash.Bytes(), hk[1:])
}

func TestBlockRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(1)
	for range 16 {
		blk := types.GenBlock(rng)
		value := encodeBlock(blk)
		decoded, err := decodeBlock(value)
		require.NoError(t, err)
		// Header hash uniquely identifies a block; equal hash => same block.
		require.Equal(t, blk.Header().Hash(), decoded.Header().Hash())
		// Re-encoding the decoded block must reproduce the same bytes.
		require.Equal(t, value, encodeBlock(decoded))
	}
}

func TestQCRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(2)
	for range 16 {
		qc := types.GenFullCommitQC(rng)
		value := encodeQC(qc)
		decoded, err := decodeQC(value)
		require.NoError(t, err)
		// Re-encoding the decoded QC must reproduce the same bytes.
		require.Equal(t, value, encodeQC(decoded))
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	// Invalid protobuf wire bytes must surface an error rather than a partial value.
	garbage := []byte{0xff, 0xff, 0xff, 0xff}
	_, blockErr := decodeBlock(garbage)
	require.Error(t, blockErr)
	_, qcErr := decodeQC(garbage)
	require.Error(t, qcErr)
}
