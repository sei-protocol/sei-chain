package types

import (
	"bytes"
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
	require.Len(t, bk, 9, "block key is 1 prefix byte + 8 number bytes")
	require.Len(t, qk, 9, "qc key is 1 prefix byte + 8 number bytes")
	require.Equal(t, kindBlock, keyKind(bk))
	require.Equal(t, kindQC, keyKind(qk))
	require.NotEqual(t, bk, qk, "same number must not collide across kinds")
	require.Equal(t, n, decodeNumberKey(bk))
	require.Equal(t, n, decodeNumberKey(qk))

	// The header-hash alias has its own kind and round-trips the hash bytes.
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	hk := BlockHashKey(hash)
	require.Equal(t, kindBlockHash, keyKind(hk))
	require.Len(t, hk, 1+len(hash))
	require.Equal(t, hash, []byte(hk[1:]))
}

func TestKindPrefixRoundTrip(t *testing.T) {
	// Only primary record kinds may be returned by a scan.
	kinds := []RecordKind{
		KindQCGroup,
		KindAppProposal,
		KindAppQC,
	}
	seen := make(map[byte]RecordKind, len(kinds))
	for _, kind := range kinds {
		prefix, err := kindPrefix(kind)
		require.NoError(t, err)
		require.NotContains(t, seen, prefix, "kind prefixes must be distinct")
		seen[prefix] = kind

		got, ok := recordKind(prefix)
		require.True(t, ok)
		require.Equal(t, kind, got)
	}

	for _, prefix := range []byte{kindQC, kindBlock, kindBlockHash} {
		_, ok := recordKind(prefix)
		require.False(t, ok, "lookup prefixes must not identify primary records")
	}
}
