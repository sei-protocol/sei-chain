package littblock

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

func TestKeyRoundTrip(t *testing.T) {
	cases := []uint64{
		0,
		1,
		42,
		255,
		256,
		1 << 32,
		^uint64(0), // max uint64
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
	pairs := [][2]uint64{
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
	const n uint64 = 42
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
	hk := blockHashKey(hash)
	require.Equal(t, kindBlockHash, keyKind(hk))
	require.Len(t, hk, 1+len(hash))
	require.Equal(t, hash, hk[1:])
}

func TestKindPrefixRoundTrip(t *testing.T) {
	// Every kind the storage contract defines must map to a prefix and back, so
	// a record written under one kind is never scanned back as another.
	kinds := []blocktypes.RecordKind{
		blocktypes.KindBlock,
		blocktypes.KindQC,
		blocktypes.KindAppProposal,
		blocktypes.KindAppQC,
	}
	seen := make(map[byte]blocktypes.RecordKind, len(kinds))
	for _, kind := range kinds {
		prefix, err := kindPrefix(kind)
		require.NoError(t, err)
		require.NotContains(t, seen, prefix, "kind prefixes must be distinct")
		seen[prefix] = kind

		got, ok := recordKind(prefix)
		require.True(t, ok)
		require.Equal(t, kind, got)
	}

	// A hash alias is not number-keyed, so a scan must not report it as a record.
	_, ok := recordKind(kindBlockHash)
	require.False(t, ok, "hash alias prefix must not decode to a record kind")
}
