package rootmulti

// Snapshot / Restore round-trips, concurrent commit race, and prune boundary.

import (
	"bytes"
	"context"
	"sync"
	"testing"

	protoio "github.com/gogo/protobuf/io"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
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
// EVMMigrated Snapshot/Restore — EVM lives only in FlatKV
//
// TestFlatKVSnapshotRestoreWithLatticeHash above covers TestOnlyDualWrite, where EVM
// state is present in both memiavl and FlatKV. In EVMMigrated the "evm"
// memiavl subtree is intentionally empty and FlatKV is the sole authoritative
// store for EVM data, so the risk profile of state sync is different:
//
//   * The snapshot writer must include the FlatKV evm rows (there is no
//     copy in memiavl to fall back on).
//   * The restored node's memiavl evm subtree must remain empty — i.e. the
//     snapshot pipeline must not accidentally reroute evm data into memiavl
//     just because it is bootstrapping.
//   * The restored FlatKV must hold the source's EVM values at the snapshot
//     version.
//   * Two nodes bootstrapped from the same snapshot must continue to track
//     each other byte-for-byte as the chain advances (the consensus-parity
//     guarantee; identical to the TestOnlyDualWrite case but pinned separately here
//     because the data path is different).
// ---------------------------------------------------------------------------

func TestFlatKVEVMMigratedSnapshotRestore(t *testing.T) {
	cfg := evmMigratedConfig()
	evmData := newEVMTestData(0x34)

	// Source: drive 5 blocks under EVMMigrated.
	srcDir := t.TempDir()
	srcStore, srcKeys := newTestRootMulti(t, srcDir, cfg)
	for block := 1; block <= 5; block++ {
		simulateBlock(t, srcStore, srcKeys, block, evmData)
	}
	srcLattice := findStoreInfo(srcStore.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, srcLattice)
	require.NotEmpty(t, srcLattice.CommitId.Hash)

	// The pre-router "memiavl evm subtree must be empty" check used to be
	// asserted here via scStore.GetChildStoreByName("evm"), but that
	// accessor now returns a router-wrapped view that surfaces FlatKV
	// data in EVMMigrated mode, so it can no longer distinguish "memiavl
	// is empty" from "router routed to flatkv". The consensus-parity
	// check on app hashes below carries the same load: if memiavl had
	// drifted on either side, the per-block hashes would not match.

	// Snapshot to buffer (keep srcStore open to continue the chain below).
	var buf bytes.Buffer
	writer := protoio.NewDelimitedWriter(&buf)
	require.NoError(t, srcStore.Snapshot(5, writer))
	require.NotEmpty(t, buf.Bytes())

	// Destination: restore from snapshot into a fresh EVMMigrated store.
	dstDir := t.TempDir()
	dstStore, _ := newTestRootMulti(t, dstDir, cfg)
	reader := protoio.NewDelimitedReader(bytes.NewReader(buf.Bytes()), 1<<30)
	_, err := dstStore.Restore(5, 1, reader)
	require.NoError(t, err)

	require.Equal(t, int64(5), dstStore.LastCommitID().Version)

	dstLattice := findStoreInfo(dstStore.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, dstLattice, "evm_lattice must be present after restore")
	require.NotEmpty(t, dstLattice.CommitId.Hash, "restored lattice hash must be non-empty")

	// The pre-router "restored memiavl evm subtree stays empty" check used
	// to be asserted here; it relied on GetChildStoreByName("evm")
	// returning a memiavl-direct view. That view is now router-wrapped
	// (see the matching comment on the source side), so the check is
	// pinned by the consensus-parity guarantee further down: drift on
	// either backend on either side would show up as a hash mismatch.

	// Extract destination store keys for the continuation below.
	dstKeys := make(map[string]*types.KVStoreKey)
	for name, key := range dstStore.storeKeys {
		kvKey, ok := key.(*types.KVStoreKey)
		require.Truef(t, ok, "unexpected store-key type for %q: %T", name, key)
		dstKeys[name] = kvKey
	}
	require.Lenf(t, dstKeys, len(storeNames),
		"expected %d KV store keys after restore, got %d", len(storeNames), len(dstKeys))

	// Consensus-parity guarantee: driving identical blocks on src and dst
	// must produce identical app hashes per block.
	for block := 6; block <= 8; block++ {
		srcRec := simulateBlock(t, srcStore, srcKeys, block, evmData)
		dstRec := simulateBlock(t, dstStore, dstKeys, block, evmData)

		require.Equalf(t, srcRec.version, dstRec.version,
			"src and dst must be at the same version after block %d", block)
		require.Equalf(t, srcRec.hash, dstRec.hash,
			"src and dst app hashes must match post-restore at block %d", block)
	}

	require.NoError(t, srcStore.Close())
	require.NoError(t, dstStore.Close())

	// Smoke-check the ro.Get path on a post-sync, post-continuation FlatKV.
	// The strong "restore delivered the right v5 state" property is already
	// pinned by the app-hash parity above: if FlatKV did not receive the
	// correct v5 snapshot, dst's block-6 lattice hash could not match src's.
	// Here we just assert the latest value is the value block 8 wrote, so a
	// regression in ro.Get after a state-sync restore would still surface.
	ro := openFlatKVReadOnly(t, dstDir, cfg, 0)
	expectedLatest := makeSlot(0x08, 0xAA) // block 8 writes (0x08, 0xAA)
	got, found := ro.Get(keys.EVMStoreKey, evmData.storKey)
	require.Truef(t, found, "restored flatkv should hold EVM key %x", evmData.storKey)
	require.Equalf(t, expectedLatest, got,
		"restored flatkv latest value mismatch for key %x", evmData.storKey)
	require.NoError(t, ro.Close())

	// Restored FlatKV is internally self-consistent (full-scan LtHash matches
	// committed LtHash). Equivalent of guarantee (a) from the TestOnlyDualWrite test.
	verifyFlatKVSelfConsistent(t, dstDir, cfg)
}

func TestFlatKVOnlySnapshotRestoreAppHashParity(t *testing.T) {
	cfg := flatKVOnlyConfig()
	evmData := newEVMTestData(0x35)

	srcDir := t.TempDir()
	srcStore, srcKeys := newTestRootMulti(t, srcDir, cfg)
	var snapshotRecord commitRecord
	for block := 1; block <= 8; block++ {
		snapshotRecord = simulateFlatKVOnlyBlock(t, srcStore, srcKeys, block, evmData)
	}

	srcLattice := findStoreInfo(srcStore.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, srcLattice)
	require.NotEmpty(t, srcLattice.CommitId.Hash)
	srcAppHash := append([]byte(nil), srcStore.LastCommitID().Hash...)

	var buf bytes.Buffer
	writer := protoio.NewDelimitedWriter(&buf)
	require.NoError(t, srcStore.Snapshot(8, writer))
	require.NotEmpty(t, buf.Bytes())

	dstDir := t.TempDir()
	dstStore, dstKeys := newTestRootMulti(t, dstDir, cfg)
	reader := protoio.NewDelimitedReader(bytes.NewReader(buf.Bytes()), 1<<30)
	_, err := dstStore.Restore(8, 1, reader)
	require.NoError(t, err)

	require.Equal(t, int64(8), dstStore.LastCommitID().Version)
	dstLattice := findStoreInfo(dstStore.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, dstLattice)
	require.Equal(t, srcLattice.CommitId.Hash, dstLattice.CommitId.Hash,
		"flatkv_only snapshot restore must reproduce the exact consensus lattice hash")
	require.Equal(t, srcAppHash, dstStore.LastCommitID().Hash,
		"flatkv_only restore AppHash must match the snapshot source at the restored height")
	require.Equal(t, snapshotRecord.workingHash, dstStore.LastCommitID().Hash,
		"flatkv_only restore AppHash must match the FinalizeBlock AppHash used by Tendermint")

	for block := 9; block <= 10; block++ {
		srcRec := simulateFlatKVOnlyBlock(t, srcStore, srcKeys, block, evmData)
		dstRec := simulateFlatKVOnlyBlock(t, dstStore, dstKeys, block, evmData)
		require.Equalf(t, srcRec.version, dstRec.version,
			"src and dst must be at the same version after block %d", block)
		require.Equalf(t, srcRec.hash, dstRec.hash,
			"src and dst app hashes must match post-restore at block %d", block)
	}

	require.NoError(t, srcStore.Close())
	require.NoError(t, dstStore.Close())
	verifyFlatKVSelfConsistent(t, srcDir, cfg)
	verifyFlatKVSelfConsistent(t, dstDir, cfg)
}

// TestFlatKVOnlySnapshotRestorePopulatesSS pins the second half of
// flatkv_only state-sync correctness: not just that the State Commit (flatkv)
// restores to a matching AppHash, but that the State Store (SS) — which serves
// EVM-RPC and historical client queries — is actually populated by the
// restore. Before the exporter fix, CompositeCommitStore.Exporter returned the
// bare flatkv exporter in flatkv_only mode, omitting the keys.FlatKVStoreKey
// module header; the restore-side SS importer then never ran convertFlatKVNodes
// and the SS came up empty, so every post-restore query returned nil even
// though consensus (SC/AppHash) looked healthy. This test drives both an EVM
// store and cosmos (acc/bank) modules, restores into a fresh SS-enabled store,
// and asserts the read path (Prove=false → SS) returns the snapshot-height
// values. It fails (nil values) without the fix.
func TestFlatKVOnlySnapshotRestorePopulatesSS(t *testing.T) {
	cfg := flatKVOnlyConfig()
	ssCfg := seidbconfig.DefaultStateStoreConfig()
	ssCfg.Enable = true
	ssCfg.AsyncWriteBuffer = 0
	evmData := newEVMTestData(0x42)

	const snapHeight = 8

	srcDir := t.TempDir()
	srcStore, srcKeys := newTestRootMultiWithSS(t, srcDir, cfg, ssCfg)
	for block := 1; block <= snapHeight; block++ {
		simulateFlatKVOnlyBlock(t, srcStore, srcKeys, block, evmData)
	}
	waitUntilSSVersion(t, srcStore, snapHeight)

	var buf bytes.Buffer
	require.NoError(t, srcStore.Snapshot(snapHeight, protoio.NewDelimitedWriter(&buf)))
	require.NotEmpty(t, buf.Bytes())
	require.NoError(t, srcStore.Close())

	dstDir := t.TempDir()
	dstStore, _ := newTestRootMultiWithSS(t, dstDir, cfg, ssCfg)
	defer func() { require.NoError(t, dstStore.Close()) }()
	reader := protoio.NewDelimitedReader(bytes.NewReader(buf.Bytes()), 1<<30)
	_, err := dstStore.Restore(snapHeight, 1, reader)
	require.NoError(t, err)
	require.Equal(t, int64(snapHeight), dstStore.LastCommitID().Version)

	dstSS := dstStore.GetStateStore()
	require.NotNil(t, dstSS)
	require.GreaterOrEqualf(t, dstSS.GetLatestVersion(), int64(snapHeight),
		"restore must initialize the SS latest version to the snapshot height")

	// Read through the production query path (Prove=false routes to SS) at the
	// restored height for cosmos and EVM modules; every value must match what
	// block 8 committed on the source.
	queryEqual := func(path string, key, want []byte) {
		t.Helper()
		resp := dstStore.Query(context.Background(), abci.RequestQuery{
			Path:   path,
			Data:   key,
			Height: snapHeight,
			Prove:  false,
		})
		require.EqualValuesf(t, 0, resp.Code, "query %s failed: %s", path, resp.Log)
		require.Equalf(t, want, resp.Value,
			"restored SS value mismatch for %s (key=%x)", path, key)
	}

	// block 8: acct1={8,0xA0}, supply={8,8,0xB0}; storKey overwritten to
	// makeSlot(8,0xBB) (block%2==0); nonce=8.
	queryEqual("/acc/key", []byte("acct1"), []byte{snapHeight, 0xA0})
	queryEqual("/bank/key", []byte("supply"), []byte{snapHeight, snapHeight, 0xB0})
	queryEqual("/evm/key", evmData.storKey, makeSlot(snapHeight, 0xBB))
	queryEqual("/evm/key", evmData.nonKey, makeNonce(uint64(snapHeight)))
}

func simulateFlatKVOnlyBlock(
	t *testing.T,
	store *Store,
	storeKeys map[string]*types.KVStoreKey,
	block int,
	evmData evmTestData,
) commitRecord {
	t.Helper()
	cms := store.CacheMultiStore()
	b := byte(block)

	accKV := cms.GetKVStore(storeKeys["acc"])
	bankKV := cms.GetKVStore(storeKeys["bank"])
	evmKV := cms.GetKVStore(storeKeys["evm"])

	accKV.Set([]byte("acct1"), []byte{b, 0xA0})
	accKV.Set([]byte("acct-overwrite"), []byte{b})
	bankKV.Set([]byte("supply"), []byte{b, b, 0xB0})
	bankKV.Set([]byte("denom/sei"), []byte{0x53, b})

	if block%3 == 0 {
		accKV.Delete([]byte("acct-overwrite"))
	}
	if block%4 == 0 {
		bankKV.Delete([]byte("denom/sei"))
	}
	if block%5 == 0 {
		bankKV.Set([]byte("empty-value"), []byte{})
	}

	evmKV.Set(evmData.storKey, makeSlot(b, 0xAA))
	evmKV.Set(evmData.nonKey, makeNonce(uint64(block)))
	if block == 1 {
		evmKV.Set(evmData.codeKey, []byte{0x60, 0x60, 0x60, b})
	}
	if block%2 == 0 {
		evmKV.Set(evmData.storKey, makeSlot(b, 0xBB))
	}
	if block%6 == 0 {
		evmKV.Delete(evmData.codeKey)
	}

	cms.Write()
	return finalizeBlock(t, store)
}

// ---------------------------------------------------------------------------
// TestFlatKVConcurrentSnapshotAndCommit: Snapshot export racing with Commit
//
// In production, Cosmos SDK's snapshot manager invokes rootmulti.Store.Snapshot
// on a separate goroutine while the consensus goroutine continues to advance
// the chain via Commit. rootmulti.Store.Snapshot does NOT take rs.mtx, so the
// two paths genuinely run concurrently. The coupling goes through
// CompositeCommitStore.Exporter -> flatkv.CommitStore.Exporter, which creates
// a readonly clone in a temp dir via LoadVersion(v, true) and iterates there
// while the main flatkv.CommitStore keeps receiving ApplyChangeSets + Commit.
//
// If any of this sharing is unsafe (readonly clone sees half-written state,
// the main writer stomps on a snapshot dir the clone is reading, Pebble
// handle reuse, etc.) this test should surface it under `go test -race`.
//
// Asserts:
//
//  1. The snapshot taken at v5 while commits continue is non-empty and
//     restorable into a fresh node.
//  2. The restored node post-restore holds the v5 state (version == 5,
//     evm_lattice present).
//  3. Driving identical blocks on the restored node and on a reference node
//     that did not have a concurrent snapshot writer produces byte-identical
//     app hashes per block. If the restored state was corrupted by the race,
//     this parity would diverge.
//  4. Both the source (after its concurrent commits) and the restored node
//     are internally self-consistent (full-scan LtHash == committed LtHash).
// ---------------------------------------------------------------------------

func TestFlatKVConcurrentSnapshotAndCommit(t *testing.T) {
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0x5C)

	// Source: drive to v5 with the snapshot target state, then concurrently
	// (a) snapshot v5 and (b) keep committing blocks 6..15.
	srcDir := t.TempDir()
	srcStore, srcKeys := newTestRootMulti(t, srcDir, cfg)
	for block := 1; block <= 5; block++ {
		simulateBlock(t, srcStore, srcKeys, block, evmData)
	}

	var (
		buf     bytes.Buffer
		snapErr error
		wg      sync.WaitGroup
	)

	// Goroutine A: snapshot v5 into buf.
	wg.Add(1)
	go func() {
		defer wg.Done()
		writer := protoio.NewDelimitedWriter(&buf)
		snapErr = srcStore.Snapshot(5, writer)
	}()

	// Goroutine B: keep committing blocks 6..15 while the snapshot runs.
	// Uses a different seed-base so values differ from the v5 snapshot,
	// exercising the case where the live writer modifies state during export.
	const extraBlocks = 10
	wg.Add(1)
	go func() {
		defer wg.Done()
		for block := 6; block <= 5+extraBlocks; block++ {
			simulateBlock(t, srcStore, srcKeys, block, evmData)
		}
	}()

	wg.Wait()
	require.NoError(t, snapErr, "concurrent Snapshot must not error")
	require.NotEmpty(t, buf.Bytes(), "snapshot buffer must be non-empty")
	require.Equal(t, int64(5+extraBlocks), srcStore.LastCommitID().Version,
		"source must have advanced past the snapshot height while export ran")

	// Restore into a fresh node and assert it lands exactly at v5.
	dstDir := t.TempDir()
	dstStore, _ := newTestRootMulti(t, dstDir, cfg)
	reader := protoio.NewDelimitedReader(bytes.NewReader(buf.Bytes()), 1<<30)
	_, err := dstStore.Restore(5, 1, reader)
	require.NoError(t, err, "restore of concurrent snapshot must succeed")
	require.Equal(t, int64(5), dstStore.LastCommitID().Version)

	dstLattice := findStoreInfo(dstStore.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, dstLattice, "evm_lattice must be present after concurrent-snapshot restore")
	require.NotEmpty(t, dstLattice.CommitId.Hash)

	// Reference node: drive v1..v5 cleanly (no concurrent snapshot), then the
	// same extra blocks. This reproduces the state the source had at v5 plus
	// the canonical continuation — no concurrency on the source side. If the
	// concurrent-snapshot path corrupted anything, parity breaks here.
	refDir := t.TempDir()
	refStore, refKeys := newTestRootMulti(t, refDir, cfg)
	for block := 1; block <= 5; block++ {
		simulateBlock(t, refStore, refKeys, block, evmData)
	}

	dstKeys := make(map[string]*types.KVStoreKey)
	for name, key := range dstStore.storeKeys {
		kvKey, ok := key.(*types.KVStoreKey)
		require.Truef(t, ok, "unexpected store-key type for %q: %T", name, key)
		dstKeys[name] = kvKey
	}

	for block := 6; block <= 8; block++ {
		refRec := simulateBlock(t, refStore, refKeys, block, evmData)
		dstRec := simulateBlock(t, dstStore, dstKeys, block, evmData)
		require.Equalf(t, refRec.version, dstRec.version,
			"ref and dst must be at the same version at block %d", block)
		require.Equalf(t, refRec.hash, dstRec.hash,
			"restored node must match a cleanly-built reference at block %d", block)
	}

	require.NoError(t, srcStore.Close())
	require.NoError(t, refStore.Close())
	require.NoError(t, dstStore.Close())

	// Both the source that had a concurrent snapshot writer running through
	// its commit pipeline and the restored node must be internally
	// self-consistent on disk. If the race caused the live writer to corrupt
	// the main flatkv DB (or the snapshot's readonly clone to stomp on it),
	// full-scan LtHash would no longer match committed LtHash.
	verifyFlatKVSelfConsistent(t, srcDir, cfg)
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
