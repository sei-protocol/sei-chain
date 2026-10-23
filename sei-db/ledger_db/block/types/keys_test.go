package types

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyRoundTrip(t *testing.T) {
	cases := []BlockNumber{
		0,
		1,
		42,
		255,
		256,
		1 << 32,
		^BlockNumber(0), // maximum block number
	}
	for _, n := range cases {
		key := encodeKey(n)
		require.Len(t, key, 8, "key must be 8 bytes")
		require.Equal(t, n, decodeKey([8]byte(key)))
	}
}

func TestKeyBigEndianOrdering(t *testing.T) {
	// Lexicographic byte order must match numeric order so LittDB's
	// insertion/range semantics line up with block numbers.
	pairs := [][2]BlockNumber{
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
	const n BlockNumber = 42
	bk := numberKey(kindBlock, n)
	qk := numberKey(kindQC, n)
	require.Equal(t, kindBlock, keyKind(bk))
	require.Equal(t, kindQC, keyKind(qk))
	require.NotEqual(t, bk, qk, "same number must not collide across kinds")
	require.Equal(t, n, decodeNumberKey(bk))
	require.Equal(t, n, decodeNumberKey(qk))

	// The header-hash alias has its own kind and round-trips the hash bytes.
	hash := make(BlockHash, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	hk := BlockHashKey(hash)
	require.Equal(t, kindBlockHash, keyKind(hk))
	_, prefixLen := binary.Uvarint(hk)
	require.Positive(t, prefixLen)
	require.Equal(t, hash, BlockHash(hk[prefixLen:]))
}

func TestKindPrefixRoundTrip(t *testing.T) {
	// Only primary record kinds may be returned by a scan.
	kinds := []RecordKind{
		KindQCGroup,
		KindAppProposal,
		KindAppQC,
	}
	seen := make(map[uint64]RecordKind, len(kinds))
	for _, kind := range kinds {
		prefix := kindPrefix(kind)
		require.NotContains(t, seen, prefix, "kind prefixes must be distinct")
		seen[prefix] = kind

		got, ok := recordKind(prefix)
		require.True(t, ok)
		require.Equal(t, kind, got)
	}

	for _, prefix := range []uint64{kindQC, kindBlock, kindBlockHash} {
		_, ok := recordKind(prefix)
		require.False(t, ok, "lookup prefixes must not identify primary records")
	}
}

func TestVarintKindPrefixes(t *testing.T) {
	for _, kind := range []uint64{0, 127, 128, 300, math.MaxUint64} {
		key := numberKey(kind, 42)
		got, n := binary.Uvarint(key)
		require.Positive(t, n)
		require.Equal(t, kind, got)
		require.Equal(t, kind, keyKind(key))
		require.Equal(t, BlockNumber(42), decodeNumberKey(key))
		require.Len(t, key, n+8)
	}
}
