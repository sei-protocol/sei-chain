package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// addrOf encodes n into a 20-byte address big-endian in the low 8 bytes so that
// byte order matches integer order (lets tests use ints as sorted addresses).
func addrOf(n uint64) []byte {
	a := make([]byte, keys.AddressLen)
	binary.BigEndian.PutUint64(a[keys.AddressLen-8:], n)
	return a
}

// accessor over a sorted slice of addresses (as ints).
func addrAccessor(addrs []uint64) func(i int) []byte {
	return func(i int) []byte { return addrOf(addrs[i]) }
}

// sortedDistinct returns count distinct uint64s in [0,span), sorted ascending.
func sortedDistinct(r *rand.Rand, count, span int) []uint64 {
	if count > span {
		count = span
	}
	set := make(map[uint64]struct{}, count)
	for len(set) < count {
		set[uint64(r.Intn(span))] = struct{}{}
	}
	out := make([]uint64, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TestPartitionAccounts_CoverageDisjoint is the core invariant: across randomized
// nonce/codehash address sets and many N, the per-pair nonce sub-ranges tile
// [0,nNonce) exactly and the per-pair codehash sub-ranges tile [0,nCode)
// exactly (contiguous, disjoint, complete) -- so every nonce and codehash leaf
// is owned by exactly one pair (no drops, no double counts).
func TestPartitionAccounts_CoverageDisjoint(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	ns := []int{1, 2, 3, 7, 64}
	for iter := 0; iter < 400; iter++ {
		nonceAddrs := sortedDistinct(r, r.Intn(40), 200)
		codeAddrs := sortedDistinct(r, r.Intn(40), 200)
		nNonce, nCode := len(nonceAddrs), len(codeAddrs)
		for _, n := range ns {
			parts := partitionAccounts(nNonce, nCode, n, addrAccessor(nonceAddrs), addrAccessor(codeAddrs))
			assertTiling(t, parts, nNonce, nCode, n, nonceAddrs, codeAddrs)
		}
	}
}

func assertTiling(t *testing.T, parts []accountPartition, nNonce, nCode, n int, nonceAddrs, codeAddrs []uint64) {
	t.Helper()
	ctx := fmt.Sprintf("nNonce=%d nCode=%d n=%d", nNonce, nCode, n)

	// Nonce ranges must tile [0,nNonce).
	expNonce := 0
	for _, p := range parts {
		require.LessOrEqual(t, p.nonceLo, p.nonceHi, ctx)
		require.Equal(t, expNonce, p.nonceLo, "nonce ranges not contiguous: %s", ctx)
		expNonce = p.nonceHi
	}
	require.Equal(t, nNonce, expNonce, "nonce ranges do not cover [0,nNonce): %s", ctx)

	// Codehash ranges must tile [0,nCode).
	expCode := 0
	for _, p := range parts {
		require.LessOrEqual(t, p.codeLo, p.codeHi, ctx)
		require.Equal(t, expCode, p.codeLo, "code ranges not contiguous: %s", ctx)
		expCode = p.codeHi
	}
	require.Equal(t, nCode, expCode, "code ranges do not cover [0,nCode): %s", ctx)

	// Semantic attachment: every codehash address must be owned by the pair
	// whose half-open nonce address interval contains it (with -inf/+inf ends).
	for ci := 0; ci < nCode; ci++ {
		owner := -1
		for pi, p := range parts {
			if ci >= p.codeLo && ci < p.codeHi {
				owner = pi
				break
			}
		}
		require.GreaterOrEqual(t, owner, 0, "codehash %d unowned: %s", ci, ctx)
		p := parts[owner]
		ca := codeAddrs[ci]
		// addrLow (owner>0) = first nonce addr in owner's nonce range; addrHigh
		// (owner<last non-empty) = next pair's first nonce addr.
		if owner > 0 && p.nonceLo < p.nonceHi {
			require.GreaterOrEqual(t, ca, nonceAddrs[p.nonceLo], "codehash below owner interval: %s", ctx)
		}
	}
}

func TestPartitionAccounts_EdgeCases(t *testing.T) {
	t.Run("empty nonce streams codehash", func(t *testing.T) {
		code := []uint64{5, 9, 12}
		parts := partitionAccounts(0, len(code), 4, addrAccessor(nil), addrAccessor(code))
		require.Len(t, parts, 1)
		require.Equal(t, accountPartition{0, 0, 0, 3}, parts[0])
	})
	t.Run("both empty", func(t *testing.T) {
		require.Empty(t, partitionAccounts(0, 0, 4, addrAccessor(nil), addrAccessor(nil)))
	})
	t.Run("empty codehash yields nonce-only pairs", func(t *testing.T) {
		nonce := []uint64{1, 2, 3, 4}
		parts := partitionAccounts(len(nonce), 0, 2, addrAccessor(nonce), addrAccessor(nil))
		require.Len(t, parts, 2)
		for _, p := range parts {
			require.Equal(t, 0, p.codeLo)
			require.Equal(t, 0, p.codeHi)
		}
	})
	t.Run("N clamped to nonce count", func(t *testing.T) {
		nonce := []uint64{1, 2}
		parts := partitionAccounts(len(nonce), 0, 10, addrAccessor(nonce), addrAccessor(nil))
		require.Len(t, parts, 2)
	})
	t.Run("N<1 treated as 1", func(t *testing.T) {
		nonce := []uint64{1, 2, 3}
		parts := partitionAccounts(len(nonce), 0, 0, addrAccessor(nonce), addrAccessor(nil))
		require.Len(t, parts, 1)
		require.Equal(t, accountPartition{0, 3, 0, 0}, parts[0])
	})
	t.Run("codehash below first and above last nonce", func(t *testing.T) {
		nonce := []uint64{10, 20}
		code := []uint64{5, 15, 25} // 5 < first nonce, 25 > last nonce
		parts := partitionAccounts(len(nonce), len(code), 2, addrAccessor(nonce), addrAccessor(code))
		// code must be fully covered.
		expCode := 0
		for _, p := range parts {
			require.Equal(t, expCode, p.codeLo)
			expCode = p.codeHi
		}
		require.Equal(t, len(code), expCode)
	})
}

// --- zipper -----------------------------------------------------------------

type mergedAccount struct {
	addr     uint64
	nonceVal []byte
	codeVal  []byte
	leaves   int
}

// leafFunc builds a (key,value) accessor over address/value slices for a kind.
func leafFunc(kind keys.EVMKeyKind, addrs []uint64, vals [][]byte) func(i int) ([]byte, []byte) {
	return func(i int) ([]byte, []byte) {
		return keys.BuildEVMKey(kind, addrOf(addrs[i])), vals[i]
	}
}

// referenceMerge produces the expected sorted-union merge of nonce and codehash
// addresses (both sorted ascending, distinct within each kind).
func referenceMerge(nonceAddrs []uint64, nonceVals [][]byte, codeAddrs []uint64, codeVals [][]byte) []mergedAccount {
	var out []mergedAccount
	ni, ci := 0, 0
	for ni < len(nonceAddrs) || ci < len(codeAddrs) {
		switch {
		case ni < len(nonceAddrs) && (ci >= len(codeAddrs) || nonceAddrs[ni] < codeAddrs[ci]):
			out = append(out, mergedAccount{nonceAddrs[ni], nonceVals[ni], nil, 1})
			ni++
		case ci < len(codeAddrs) && (ni >= len(nonceAddrs) || codeAddrs[ci] < nonceAddrs[ni]):
			out = append(out, mergedAccount{codeAddrs[ci], nil, codeVals[ci], 1})
			ci++
		default:
			out = append(out, mergedAccount{nonceAddrs[ni], nonceVals[ni], codeVals[ci], 2})
			ni++
			ci++
		}
	}
	return out
}

func collectZip(t *testing.T, nonceAddrs []uint64, nonceVals [][]byte, codeAddrs []uint64, codeVals [][]byte) []mergedAccount {
	t.Helper()
	var got []mergedAccount
	emit := func(addr, nonceVal, codeVal []byte, leaves int) error {
		got = append(got, mergedAccount{binary.BigEndian.Uint64(addr[keys.AddressLen-8:]), nonceVal, codeVal, leaves})
		return nil
	}
	err := zipRange(
		0, len(nonceAddrs), leafFunc(keys.EVMKeyNonce, nonceAddrs, nonceVals),
		0, len(codeAddrs), leafFunc(keys.EVMKeyCodeHash, codeAddrs, codeVals),
		emit,
	)
	require.NoError(t, err)
	return got
}

func valsFor(addrs []uint64, salt byte) [][]byte {
	out := make([][]byte, len(addrs))
	for i, a := range addrs {
		v := make([]byte, 8)
		binary.BigEndian.PutUint64(v, a)
		out[i] = append([]byte{salt}, v...)
	}
	return out
}

func TestZipRange_MatchesReferenceMerge(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for iter := 0; iter < 500; iter++ {
		nonceAddrs := sortedDistinct(r, r.Intn(30), 100)
		codeAddrs := sortedDistinct(r, r.Intn(30), 100)
		nonceVals := valsFor(nonceAddrs, 0x01)
		codeVals := valsFor(codeAddrs, 0x02)

		want := referenceMerge(nonceAddrs, nonceVals, codeAddrs, codeVals)
		got := collectZip(t, nonceAddrs, nonceVals, codeAddrs, codeVals)
		require.Equal(t, want, got, "iter %d", iter)
	}
}

// TestPartitionThenZip_EqualsFullZip verifies the combined invariant: splitting
// into N pairs and concatenating each pair's zip output yields exactly the same
// (address-sorted) merged accounts as a single full zip -- independent of N.
func TestPartitionThenZip_EqualsFullZip(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for iter := 0; iter < 300; iter++ {
		nonceAddrs := sortedDistinct(r, r.Intn(40), 200)
		codeAddrs := sortedDistinct(r, r.Intn(40), 200)
		nonceVals := valsFor(nonceAddrs, 0x01)
		codeVals := valsFor(codeAddrs, 0x02)

		full := collectZip(t, nonceAddrs, nonceVals, codeAddrs, codeVals)

		for _, n := range []int{1, 2, 3, 5, 16} {
			parts := partitionAccounts(len(nonceAddrs), len(codeAddrs), n, addrAccessor(nonceAddrs), addrAccessor(codeAddrs))
			var combined []mergedAccount
			for _, p := range parts {
				emit := func(addr, nonceVal, codeVal []byte, leaves int) error {
					combined = append(combined, mergedAccount{binary.BigEndian.Uint64(addr[keys.AddressLen-8:]), nonceVal, codeVal, leaves})
					return nil
				}
				err := zipRange(
					p.nonceLo, p.nonceHi, leafFunc(keys.EVMKeyNonce, nonceAddrs, nonceVals),
					p.codeLo, p.codeHi, leafFunc(keys.EVMKeyCodeHash, codeAddrs, codeVals),
					emit,
				)
				require.NoError(t, err)
			}
			require.Equal(t, full, combined, "iter %d n=%d", iter, n)
		}
	}
}

func TestZipRange_RejectsWrongKind(t *testing.T) {
	// A storage key in the "nonce" position must trip the kind assertion.
	leafBad := func(i int) ([]byte, []byte) {
		return keys.BuildEVMKey(keys.EVMKeyStorage, make([]byte, keys.AddressLen+32)), []byte{0x00}
	}
	err := zipRange(
		0, 1, leafBad,
		0, 0, leafFunc(keys.EVMKeyCodeHash, nil, nil),
		func(addr, nonceVal, codeVal []byte, leaves int) error { return nil },
	)
	require.Error(t, err)
}

func TestZipRange_RejectsUnsorted(t *testing.T) {
	addrs := []uint64{5, 3} // descending -> not strictly ascending
	vals := valsFor(addrs, 0x01)
	err := zipRange(
		0, 2, leafFunc(keys.EVMKeyNonce, addrs, vals),
		0, 0, leafFunc(keys.EVMKeyCodeHash, nil, nil),
		func(addr, nonceVal, codeVal []byte, leaves int) error { return nil },
	)
	require.Error(t, err)
}

// --- complement / split helpers ---------------------------------------------

func TestComplementIntervals(t *testing.T) {
	cases := []struct {
		total    int
		excluded [][2]int
		want     [][2]int
	}{
		{10, [][2]int{{2, 4}, {6, 8}}, [][2]int{{0, 2}, {4, 6}, {8, 10}}},
		{10, [][2]int{{0, 4}}, [][2]int{{4, 10}}},
		{10, [][2]int{{6, 10}}, [][2]int{{0, 6}}},
		{10, [][2]int{{0, 10}}, nil},
		{10, nil, [][2]int{{0, 10}}},
		{10, [][2]int{{3, 3}}, [][2]int{{0, 10}}},                 // empty excluded ignored
		{10, [][2]int{{8, 10}, {2, 4}}, [][2]int{{0, 2}, {4, 8}}}, // unsorted input
	}
	for i, c := range cases {
		got := complementIntervals(c.total, c.excluded)
		require.Equal(t, c.want, got, "case %d", i)
	}
}

func TestSplitIntervals_CoversExactlyOnce(t *testing.T) {
	intervals := [][2]int{{0, 5}, {10, 13}, {20, 21}} // total length 9
	for _, parts := range []int{1, 2, 3, 4, 100} {
		split := splitIntervals(intervals, parts)
		// Flatten and compare to the concatenation of inputs.
		var flat []int
		for _, group := range split {
			for _, iv := range group {
				require.Less(t, iv[0], iv[1])
				for x := iv[0]; x < iv[1]; x++ {
					flat = append(flat, x)
				}
			}
		}
		var want []int
		for _, iv := range intervals {
			for x := iv[0]; x < iv[1]; x++ {
				want = append(want, x)
			}
		}
		require.Equal(t, want, flat, "parts=%d", parts)
	}
}

func TestSplitIntervals_Empty(t *testing.T) {
	require.Nil(t, splitIntervals(nil, 4))
	require.Nil(t, splitIntervals([][2]int{{3, 3}}, 4))
}

// sanity: addrOf ordering matches integer ordering.
func TestAddrOfOrdering(t *testing.T) {
	require.True(t, bytes.Compare(addrOf(1), addrOf(2)) < 0)
	require.True(t, bytes.Compare(addrOf(255), addrOf(256)) < 0)
}
