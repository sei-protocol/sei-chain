package rootmulti

// Snapshot / restore integration tests and prune-boundary coverage for the
// FlatKV rootmulti wiring:
//
//   TestFlatKVSnapshotRestoreWithLatticeHash exercises the full Snapshot /
//   Restore round-trip and asserts that two nodes bootstrapped from the same
//   snapshot continue to track each other byte-for-byte (the key guarantee
//   that protects against a fork between snapshot-bootstrapped and
//   non-snapshot nodes once the lattice hash participates in consensus).
//
//   TestFlatKVPruneBoundaryQueries exercises rootmulti under aggressive
//   memiavl+flatkv snapshot pruning to guard against regressions that would
//   drop recent versions, corrupt the FlatKV LtHash, or make historical
//   queries within the retained window return bad hashes.

import (
	"bytes"
	"testing"

	protoio "github.com/gogo/protobuf/io"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Snapshot and Restore round-trip with lattice hash
// ---------------------------------------------------------------------------

func TestFlatKVSnapshotRestoreWithLatticeHash(t *testing.T) {
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0x33)

	// Source: commit 5 blocks
	srcDir := t.TempDir()
	srcStore, srcKeys := newTestRootMulti(t, srcDir, cfg)
	for block := 1; block <= 5; block++ {
		simulateBlock(t, srcStore, srcKeys, block, evmData)
	}

	srcLattice := findStoreInfo(srcStore.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, srcLattice)
	require.NotEmpty(t, srcLattice.CommitId.Hash)

	// Snapshot to buffer (keep srcStore open to continue the chain below).
	var buf bytes.Buffer
	writer := protoio.NewDelimitedWriter(&buf)
	require.NoError(t, srcStore.Snapshot(5, writer))
	require.NotEmpty(t, buf.Bytes())

	// Destination: restore from snapshot
	dstDir := t.TempDir()
	dstStore, _ := newTestRootMulti(t, dstDir, cfg)
	reader := protoio.NewDelimitedReader(bytes.NewReader(buf.Bytes()), 1<<30)
	_, err := dstStore.Restore(5, 1, reader)
	require.NoError(t, err)

	require.Equal(t, int64(5), dstStore.LastCommitID().Version)

	dstLattice := findStoreInfo(dstStore.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, dstLattice, "evm_lattice must be present after restore")
	require.NotEmpty(t, dstLattice.CommitId.Hash, "restored lattice hash must be non-empty")

	// NOTE: exact hash equality (srcLatticeHash == dstLattice hash) is not
	// asserted because the export/import round-trip decomposes merged account
	// rows into separate nonce+codehash nodes and re-merges them, which
	// produces a different serialized form and thus a different LtHash.
	// The memiavl tree hashes (acc, bank, evm) are unchanged because
	// the leaf key/value bytes survive the round-trip.
	// TODO: make the round-trip lossless so that lattice hashes match exactly.
	//
	// Until that is fixed we still need the two following guarantees to hold,
	// because otherwise enabling the lattice hash in consensus would fork the
	// network between snapshot-bootstrapped and non-snapshot nodes:
	//
	//   (a) the restored FlatKV is internally self-consistent: a full-scan
	//       recomputation of LtHash from disk must match the committed
	//       CommittedRootHash. Any drift here means snapshot import produced
	//       a corrupt LtHash state. Verified via VerifyLtHash below.
	//
	//   (b) nodes bootstrapped from the same snapshot continue to track each
	//       other byte-for-byte: the app hash produced by driving identical
	//       blocks on top of the restored state equals the app hash produced
	//       by driving those same blocks on a node that kept running.
	//       Verified via the "continue the chain" comparison below.

	// Extract the destination store keys (must include every mounted store).
	dstKeys := make(map[string]*types.KVStoreKey)
	for name, key := range dstStore.storeKeys {
		kvKey, ok := key.(*types.KVStoreKey)
		require.Truef(t, ok, "unexpected store-key type for %q: %T", name, key)
		dstKeys[name] = kvKey
	}
	require.Lenf(t, dstKeys, len(storeNames),
		"expected %d KV store keys after restore, got %d", len(storeNames), len(dstKeys))

	// Continue the chain on both source and destination with identical input
	// and assert byte-for-byte parity of the app hash each block (guarantee b).
	for block := 6; block <= 8; block++ {
		srcRec := simulateBlock(t, srcStore, srcKeys, block, evmData)
		dstRec := simulateBlock(t, dstStore, dstKeys, block, evmData)

		require.Equalf(t, srcRec.version, dstRec.version,
			"src and dst must be at the same version after block %d", block)
		require.Equalf(t, srcRec.hash, dstRec.hash,
			"src and dst app hashes must match post-restore at block %d", block)

		srcLt := findStoreInfo(srcRec.infos, "evm_lattice")
		dstLt := findStoreInfo(dstRec.infos, "evm_lattice")
		require.NotNil(t, srcLt)
		require.NotNil(t, dstLt)
		require.NotEmpty(t, dstLt.CommitId.Hash)
		require.NotEqual(t, dstLattice.CommitId.Hash, dstLt.CommitId.Hash,
			"lattice hash should change after new commit post-restore")
	}

	require.NoError(t, srcStore.Close())
	require.NoError(t, dstStore.Close())

	// Guarantee (a): restored FlatKV's on-disk LtHash is self-consistent.
	verifyFlatKVSelfConsistent(t, dstDir, cfg)
}

// ---------------------------------------------------------------------------
// TestFlatKVPruneBoundaryQueries: aggressive snapshot pruning
//
// This test wires up both memiavl and FlatKV with very small SnapshotKeepRecent
// windows (1 older snapshot each on top of the current one) while snapshotting
// every block. After N > keep-window commits the store must still:
//
//  1. Report the correct latest version and app hash.
//  2. Serve historical hash queries for versions inside the retained window
//     (recent versions near the tip) with byte-for-byte parity against the
//     records captured at commit time.
//  3. Keep the FlatKV on-disk state self-consistent — a full-scan LtHash
//     recomputation after close must match the committed root.
//
// Layer-local pruning is already covered by unit tests in
// sei-db/state_db/sc/memiavl/store_test.go and sei-db/state_db/sc/flatkv/
// snapshot_test.go. The value added here is composite behavior through
// rootmulti: specifically that rootmulti's hash amendment / CommitInfo
// reconstruction still works correctly once both backends have pruned their
// older snapshots underneath it.
// ---------------------------------------------------------------------------

func TestFlatKVPruneBoundaryQueries(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	// Aggressive pruning: keep only the latest snapshot plus 1 older for both
	// backends. With SnapshotInterval=1 every commit produces a new snapshot,
	// so after 10 blocks each backend should have pruned several older
	// snapshots.
	cfg.MemIAVLConfig.SnapshotKeepRecent = 1
	cfg.FlatKVConfig.SnapshotKeepRecent = 1

	evmData := newEVMTestData(0xCD)
	store, storeKeys := newTestRootMulti(t, dir, cfg)

	const numBlocks = 10
	var records []commitRecord
	for block := 1; block <= numBlocks; block++ {
		records = append(records, simulateBlock(t, store, storeKeys, block, evmData))
	}

	require.Equal(t, int64(numBlocks), store.LastCommitID().Version)
	require.Equal(t, records[numBlocks-1].hash, store.lastCommitInfo.Hash(),
		"latest app hash must match the last commit record")

	// Versions at the tip must remain queryable through rootmulti's
	// LoadVersion + amendCommitInfo path even when older snapshots have been
	// pruned. We assert for the latest two versions which are always inside
	// every reasonable retention window.
	for _, v := range []int64{numBlocks - 1, numBlocks} {
		scStore, err := store.scStore.LoadVersion(v, true)
		require.NoErrorf(t, err, "LoadVersion(%d) should succeed within retention window", v)

		commitInfo := convertCommitInfo(scStore.LastCommitInfo())
		commitInfo = amendCommitInfo(commitInfo, store.storesParams)
		require.Equalf(t, records[v-1].hash, commitInfo.Hash(),
			"retained version %d app hash mismatch after prune", v)

		lattice := findStoreInfo(commitInfo.StoreInfos, "evm_lattice")
		require.NotNilf(t, lattice, "evm_lattice must still be present at retained version %d", v)
		require.Equalf(t,
			findStoreInfo(records[v-1].infos, "evm_lattice").CommitId.Hash,
			lattice.CommitId.Hash,
			"retained version %d lattice hash must match original", v)

		require.NoError(t, scStore.Close())
	}

	require.NoError(t, store.Close())

	// FlatKV must still be internally consistent after its own pruning.
	verifyFlatKVSelfConsistent(t, dir, cfg)
}
