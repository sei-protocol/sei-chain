package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// evmKey helpers for building a deterministic mixed EVM keyspace.

func acctAddr(b byte) []byte {
	a := make([]byte, keys.AddressLen)
	a[0] = b
	return a
}

func nonceKV(b, n byte) *proto.KVPair {
	v := make([]byte, 8)
	v[7] = n
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyNonce, acctAddr(b)), Value: v}
}

func codeHashKV(b, h byte) *proto.KVPair {
	v := make([]byte, 32)
	v[0] = h
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, acctAddr(b)), Value: v}
}

func codeKV(b byte) *proto.KVPair {
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCode, acctAddr(b)), Value: []byte{0x60, 0x60, b}}
}

func storageKV(b, slot byte) *proto.KVPair {
	sk := make([]byte, keys.AddressLen+32)
	sk[0] = b
	sk[keys.AddressLen] = slot
	v := make([]byte, 32)
	v[31] = slot
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyStorage, sk), Value: v}
}

// legacyKV builds a raw legacy key with the given first prefix byte (e.g. 0x09
// codesize, 0x0b receipt) so we exercise prefixes adjacent to nonce/codehash.
func legacyKV(prefix, b byte) *proto.KVPair {
	return &proto.KVPair{Key: []byte{prefix, b, 0xFF}, Value: []byte{b}}
}

// mixedEVMPairs returns a deterministic mix of every relevant key kind,
// including accounts with only a nonce, only a codehash, and both, plus legacy
// keys whose prefix bytes (0x09, 0x0b) sit adjacent to the account prefixes.
func mixedEVMPairs() []*proto.KVPair {
	var pairs []*proto.KVPair
	// storage (0x03)
	for b := byte(1); b <= 4; b++ {
		pairs = append(pairs, storageKV(b, b))
	}
	// code (0x07)
	pairs = append(pairs, codeKV(2), codeKV(5))
	// codehash (0x08): addresses 2, 5, 9 (5 & 9 are codehash-only)
	pairs = append(pairs, codeHashKV(2, 0xAA), codeHashKV(5, 0xBB), codeHashKV(9, 0xCC))
	// codesize legacy (0x09)
	pairs = append(pairs, legacyKV(0x09, 1), legacyKV(0x09, 2))
	// nonce (0x0a): addresses 1, 2, 3 (1 & 3 are nonce-only; 2 has both) plus a
	// zero-nonce-only account (6) that BOTH paths must drop as a no-op.
	pairs = append(pairs, nonceKV(1, 11), nonceKV(2, 22), nonceKV(3, 33), nonceKV(6, 0))
	// receipt legacy (0x0b)
	pairs = append(pairs, legacyKV(0x0b, 7))
	return pairs
}

// buildEVMSnapshot writes a single-tree memIAVL snapshot of the given pairs into
// <dir>/evm and returns the version.
func buildEVMSnapshot(t *testing.T, dir string, pairs []*proto.KVPair) int64 {
	t.Helper()
	tree := memiavl.New(0)
	tree.ApplyChangeSet(proto.ChangeSet{Pairs: pairs})
	_, version, err := tree.SaveVersion(true)
	require.NoError(t, err)
	require.NoError(t, tree.WriteSnapshot(context.Background(), filepath.Join(dir, keys.EVMStoreKey)))
	return version
}

func TestKindLeafRange_Bounds(t *testing.T) {
	dir := t.TempDir()
	buildEVMSnapshot(t, dir, mixedEVMPairs())

	snap, err := memiavl.OpenSnapshot(filepath.Join(dir, keys.EVMStoreKey), memiavl.Options{ZeroCopy: true})
	require.NoError(t, err)
	defer func() { _ = snap.Close() }()

	check := func(kind keys.EVMKeyKind, wantCount int) (lo, hi int) {
		lo, hi, err := kindLeafRange(snap, kind)
		require.NoError(t, err)
		require.Equal(t, wantCount, hi-lo, "count for kind %d", kind)
		prefix, ok := keys.EVMKeyPrefixByte(kind)
		require.True(t, ok)
		for i := lo; i < hi; i++ {
			k := snap.LeafKey(uint32(i))
			require.Equal(t, prefix, k[0], "leaf %d outside kind %d", i, kind)
		}
		return lo, hi
	}

	check(keys.EVMKeyStorage, 4)
	check(keys.EVMKeyCode, 2)
	cLo, cHi := check(keys.EVMKeyCodeHash, 3)
	nLo, nHi := check(keys.EVMKeyNonce, 4)

	// codehash (0x08) precedes nonce (0x0a) and they are disjoint.
	require.LessOrEqual(t, cHi, nLo)
	require.Less(t, cLo, cHi)
	require.Less(t, nLo, nHi)
}

// TestKindLeafRange_AbsentKindEmpty verifies a kind with no leaves yields an
// empty range (lo == hi).
func TestKindLeafRange_AbsentKindEmpty(t *testing.T) {
	dir := t.TempDir()
	// Only storage keys: nonce/codehash/code ranges must be empty.
	buildEVMSnapshot(t, dir, []*proto.KVPair{storageKV(1, 1), storageKV(2, 2)})

	snap, err := memiavl.OpenSnapshot(filepath.Join(dir, keys.EVMStoreKey), memiavl.Options{ZeroCopy: true})
	require.NoError(t, err)
	defer func() { _ = snap.Close() }()

	for _, kind := range []keys.EVMKeyKind{keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyCode} {
		lo, hi, err := kindLeafRange(snap, kind)
		require.NoError(t, err)
		require.Equal(t, lo, hi, "kind %d should be empty", kind)
	}
}

// committedRootHash reopens a flatKV store and returns its committed root hash
// at the latest version.
func committedRootHash(t *testing.T, dir string) []byte {
	t.Helper()
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = dir
	store, err := flatkv.NewCommitStore(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)
	return store.CommittedRootHash()
}

// importEVMBuffered is the reference "current" path: a single full leaf scan fed
// through ImportTranslator with all accounts buffered until Finalize. It exists
// only in tests so the new parallel merge-join path can be checked against it.
func importEVMBuffered(t *testing.T, snapshotDir, outDir string, height int64) {
	t.Helper()
	snap, err := memiavl.OpenSnapshot(filepath.Join(snapshotDir, keys.EVMStoreKey), memiavl.Options{ZeroCopy: true})
	require.NoError(t, err)
	defer func() { _ = snap.Close() }()

	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = outDir
	store, err := flatkv.NewCommitStore(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)

	imp, err := store.Importer(height)
	require.NoError(t, err)

	tr := flatkv.NewImportTranslator(height)
	batch := &proto.NamedChangeSet{Name: keys.EVMStoreKey}
	flush := func() {
		if len(batch.Changeset.Pairs) == 0 {
			return
		}
		pairs, terr := tr.Translate(batch)
		require.NoError(t, terr)
		emitPairs(imp, pairs, height)
		batch.Changeset.Pairs = batch.Changeset.Pairs[:0]
	}
	require.NoError(t, snap.ScanLeafRange(0, snap.LeavesLen(), func(key, value []byte) error {
		batch.Changeset.Pairs = append(batch.Changeset.Pairs, &proto.KVPair{Key: key, Value: value})
		if len(batch.Changeset.Pairs) >= 64 {
			flush()
		}
		return nil
	}))
	flush()
	emitPairs(imp, tr.Finalize(), height)
	require.NoError(t, imp.Close())
}

// TestImportEVM_NewPathEqualsBuffered builds a mixed EVM memIAVL snapshot and
// asserts the new parallel merge-join import produces a flatKV store with the
// same committed root hash as the buffered reference path, across several values
// of numReaders (the result must be independent of the pair count).
func TestImportEVM_NewPathEqualsBuffered(t *testing.T) {
	src := t.TempDir()
	height := buildEVMSnapshot(t, src, mixedEVMPairs())

	refDir := filepath.Join(t.TempDir(), "ref")
	importEVMBuffered(t, src, refDir, height)
	want := committedRootHash(t, refDir)
	require.NotEmpty(t, want)

	origReaders := numReaders
	defer func() { numReaders = origReaders }()

	for _, n := range []int{1, 2, 3, 8} {
		t.Run(fmt.Sprintf("numReaders=%d", n), func(t *testing.T) {
			numReaders = n
			outDir := filepath.Join(t.TempDir(), "new")
			require.NoError(t, importEVMToFlatKV(src, outDir, height))
			got := committedRootHash(t, outDir)
			require.Equal(t, want, got, "root hash must match buffered path for numReaders=%d", n)
		})
	}
}
