package rootmulti

// Mode and query-path integration tests:
//
//   TestFlatKVShadowModeNoLatticeInAppHash — shadow mode (DualWrite,
//   EnableLatticeHash=false) must keep FlatKV's on-disk LtHash bit-identical
//   to the lattice-enabled run, so that flipping EnableLatticeHash true on a
//   shadow node cannot fork the chain.
//
//   TestFlatKVQueryWithSSAndReadMode — Query path with SS enabled (no proof)
//   plus ics23 proof verification against the returned app hash.
//
//   TestFlatKVSSAsyncRestartConsistency — exercises SS with AsyncWriteBuffer
//   > 0 and asserts that after a clean restart the SS + FlatKV + memiavl
//   triplet agrees on historical EVM and cosmos values, so that the SS WAL
//   replay wired into composite.NewCompositeStateStore does not regress for
//   DualWrite. A true SIGKILL "crash" is out of scope at the integration
//   layer; the lower-level SS crash paths are covered by
//   sei-db/state_db/ss/composite/recovery_test.go.

import (
	"fmt"
	"testing"

	storerootmulti "github.com/sei-protocol/sei-chain/sei-cosmos/store/rootmulti"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Shadow mode — DualWrite + EnableLatticeHash=false
// ---------------------------------------------------------------------------

func TestFlatKVShadowModeNoLatticeInAppHash(t *testing.T) {
	evmData := newEVMTestData(0x77)

	// Store A: DualWrite with lattice *disabled* (shadow mode).
	dirA := t.TempDir()
	storeA, keysA := newTestRootMulti(t, dirA, dualWriteNoLatticeConfig())

	// Store B: CosmosOnly baseline (used to verify consensus parity).
	dirB := t.TempDir()
	storeB, keysB := newTestRootMulti(t, dirB, cosmosOnlyConfig())

	// Store C: DualWrite with lattice *enabled* on identical input. Used to
	// verify that the FlatKV LtHash computed while shadowing is bit-equal to
	// the LtHash computed when consensus actually uses it. This is the key
	// correctness property for the shadow -> enable migration path: flipping
	// EnableLatticeHash on a shadow node must not change the underlying hash
	// state.
	dirC := t.TempDir()
	storeC, keysC := newTestRootMulti(t, dirC, dualWriteConfig())

	// Drive each store with byte-identical input via simulateBlock.
	for block := 1; block <= 5; block++ {
		simulateBlock(t, storeA, keysA, block, evmData)
		simulateBlock(t, storeB, keysB, block, evmData)
		simulateBlock(t, storeC, keysC, block, evmData)

		// App hashes must be identical (flatkv does not affect consensus).
		require.Equalf(t, storeB.lastCommitInfo.Hash(), storeA.lastCommitInfo.Hash(),
			"shadow mode app hash must match CosmosOnly at block %d", block)

		// evm_lattice must NOT appear in Store A's CommitInfo.
		latticeA := findStoreInfo(storeA.lastCommitInfo.StoreInfos, "evm_lattice")
		require.Nilf(t, latticeA,
			"evm_lattice must not appear with EnableLatticeHash=false (block %d)", block)

		// Store C DOES include evm_lattice.
		latticeC := findStoreInfo(storeC.lastCommitInfo.StoreInfos, "evm_lattice")
		require.NotNilf(t, latticeC, "evm_lattice expected on lattice-enabled store (block %d)", block)
		require.NotEmpty(t, latticeC.CommitId.Hash)
	}

	require.NoError(t, storeB.Close())
	require.NoError(t, storeA.Close())
	require.NoError(t, storeC.Close())

	// Core shadow-mode guarantee: FlatKV's on-disk LtHash is identical
	// whether or not it participates in consensus. If this ever diverges,
	// flipping EnableLatticeHash=true on a shadow node will fork the chain.
	cfgShadow := dualWriteNoLatticeConfig()
	roShadow := openFlatKVReadOnly(t, dirA, cfgShadow, 0)
	defer func() { require.NoError(t, roShadow.Close()) }()
	require.NotEmpty(t, roShadow.RootHash(),
		"flatkv should have non-empty RootHash even with lattice disabled")

	cfgEnabled := dualWriteConfig()
	roEnabled := openFlatKVReadOnly(t, dirC, cfgEnabled, 0)
	defer func() { require.NoError(t, roEnabled.Close()) }()

	require.Equal(t, roEnabled.CommittedRootHash(), roShadow.CommittedRootHash(),
		"FlatKV CommittedRootHash must be identical under shadow and lattice-enabled modes for the same input")

	require.NoError(t, flatkv.VerifyLtHash(roShadow), "full-scan LtHash failed in shadow mode")
	require.NoError(t, flatkv.VerifyLtHash(roEnabled), "full-scan LtHash failed in lattice-enabled mode")
}

// ---------------------------------------------------------------------------
// Query with SS enabled and ReadMode
// ---------------------------------------------------------------------------

func TestFlatKVQueryWithSSAndReadMode(t *testing.T) {
	scCfg := dualWriteConfig()
	ssCfg := seidbconfig.DefaultStateStoreConfig()
	ssCfg.Enable = true
	ssCfg.AsyncWriteBuffer = 0

	store, storeKeys := newTestRootMultiWithSS(t, t.TempDir(), scCfg, ssCfg)
	defer func() { require.NoError(t, store.Close()) }()

	evmData := newEVMTestData(0x88)

	cms := store.CacheMultiStore()
	cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{1})
	cms.GetKVStore(storeKeys["evm"]).Set(evmData.storKey, makeSlot(0x01, 0xAA))
	cms.Write()
	c1 := finalizeBlock(t, store)
	require.Equal(t, int64(1), c1.version)

	cms = store.CacheMultiStore()
	cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{2})
	cms.GetKVStore(storeKeys["evm"]).Set(evmData.storKey, makeSlot(0x02, 0xBB))
	cms.Write()
	c2 := finalizeBlock(t, store)
	require.Equal(t, int64(2), c2.version)

	waitUntilSSVersion(t, store, c2.version)

	resp := store.Query(abci.RequestQuery{
		Path:   "/acc/key",
		Data:   []byte("acct1"),
		Height: c1.version,
		Prove:  false,
	})
	require.EqualValues(t, 0, resp.Code, "cosmos query failed: %s", resp.Log)
	require.Equal(t, []byte{1}, resp.Value)

	resp = store.Query(abci.RequestQuery{
		Path:   "/evm/key",
		Data:   evmData.storKey,
		Height: c1.version,
		Prove:  false,
	})
	require.EqualValues(t, 0, resp.Code, "evm query failed: %s", resp.Log)
	require.Equal(t, makeSlot(0x01, 0xAA), resp.Value)

	resp = store.Query(abci.RequestQuery{
		Path:   "/evm/key",
		Data:   evmData.storKey,
		Height: c2.version,
		Prove:  false,
	})
	require.EqualValues(t, 0, resp.Code, "evm latest query failed: %s", resp.Log)
	require.Equal(t, makeSlot(0x02, 0xBB), resp.Value)

	// Proof path (SC).
	resp = store.Query(abci.RequestQuery{
		Path:   "/evm/key",
		Data:   evmData.storKey,
		Height: c1.version,
		Prove:  true,
	})
	require.EqualValues(t, 0, resp.Code, "evm proof query failed: %s", resp.Log)
	require.Equal(t, makeSlot(0x01, 0xAA), resp.Value)
	require.NotNil(t, resp.ProofOps, "proof should be present")
	require.NotEmpty(t, resp.ProofOps.Ops, "proof ops should not be empty")

	// Actually verify the ics23 proof against the v1 app hash. The rootmulti
	// proof is composed of (inner) iavl commitment op for the key + (outer)
	// simple-merkle commitment op keyed by the store name. The decoded proof
	// operators consume the keypath from the last key to the first, so the
	// keypath must be "/<storeName>/<keybytes>". Binary keys are hex-encoded
	// via the "x:" prefix.
	prt := storerootmulti.DefaultProofRuntime()
	keypath := fmt.Sprintf("/evm/x:%X", evmData.storKey)
	require.NoError(t,
		prt.VerifyValue(resp.ProofOps, c1.hash, keypath, makeSlot(0x01, 0xAA)),
		"ics23 proof must verify against the v1 app hash")

	require.Error(t,
		prt.VerifyValue(resp.ProofOps, c1.hash, keypath, makeSlot(0x01, 0xBB)),
		"proof verification must reject a tampered value")
}

// ---------------------------------------------------------------------------
// TestFlatKVSSAsyncRestartConsistency: SS async writes + restart
//
// TestFlatKVQueryWithSSAndReadMode keeps SS in synchronous mode
// (AsyncWriteBuffer = 0) and only looks at a single live store. That leaves
// two integration gaps:
//
//   * The production deployment sets AsyncWriteBuffer = 100 by default. SS
//     commits race behind SC and are only "flushed" on a best-effort basis
//     (Pebble is opened with NoSync), so this path was never driven through
//     rootmulti with more than a couple of commits and a subsequent reopen.
//
//   * On store reopen, composite.NewCompositeStateStore invokes
//     RecoverCompositeStateStore, which replays the SS changelog from the
//     current SS version forward. The integration-level path that actually
//     triggers this replay on the happy "clean shutdown + reopen" path is
//     otherwise untested at the rootmulti layer; only sei-db level tests
//     exercise it.
//
// This test therefore configures SS with AsyncWriteBuffer > 0, commits
// several blocks (including an EVM-touching block at a retained height),
// waits for SS to catch up, closes and reopens the store, and verifies that
// historical cosmos and EVM reads via rootmulti (SS path, no proof) still
// return the originally committed values.
//
// NOTE: a true kill-9 crash cannot be simulated without a subprocess because
// the in-process Close() drains the async queue. For kill-9 style coverage
// of SS see sei-db/state_db/ss/composite/recovery_test.go
// (TestRecoverCompositeStateStore, TestSyncEVMStoreBehind).
// ---------------------------------------------------------------------------

func TestFlatKVSSAsyncRestartConsistency(t *testing.T) {
	home := t.TempDir()

	scCfg := dualWriteConfig()
	ssCfg := seidbconfig.DefaultStateStoreConfig()
	ssCfg.Enable = true
	ssCfg.AsyncWriteBuffer = 100 // production default

	openStore := func() (*Store, map[string]*types.KVStoreKey) {
		return newTestRootMultiWithSS(t, home, scCfg, ssCfg)
	}

	store, storeKeys := openStore()

	evmData := newEVMTestData(0x89)

	type committed struct {
		version  int64
		accVal   []byte
		evmVal   []byte
		appHash  []byte
		hasEVM   bool
		hasCosmo bool
	}
	history := make(map[int64]committed)

	// Drive N commits: EVM-only on odd blocks, cosmos-only on even blocks so
	// both SS routes (evm_ss and cosmos_ss) are exercised.
	const numBlocks = 6
	for block := 1; block <= numBlocks; block++ {
		cms := store.CacheMultiStore()
		b := byte(block)

		var acc, evmVal []byte
		var hasEVM, hasCosmo bool

		if block%2 == 1 {
			evmVal = makeSlot(b, 0xA1)
			cms.GetKVStore(storeKeys["evm"]).Set(evmData.storKey, evmVal)
			hasEVM = true
		} else {
			acc = []byte{b, 0x33}
			cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), acc)
			hasCosmo = true
		}

		cms.Write()
		_, err := store.GetWorkingHash()
		require.NoError(t, err)
		cid := store.Commit(true)

		history[cid.Version] = committed{
			version:  cid.Version,
			accVal:   acc,
			evmVal:   evmVal,
			appHash:  cid.Hash,
			hasEVM:   hasEVM,
			hasCosmo: hasCosmo,
		}
	}

	// Wait for SS to catch up, then close. Close is expected to drain the
	// async queue; a true crash window is not in scope at this layer.
	latestVersion := int64(numBlocks)
	waitUntilSSVersion(t, store, latestVersion)
	require.NoError(t, store.Close())

	// Reopen. This triggers RecoverCompositeStateStore's WAL replay path.
	store2, _ := openStore()
	defer func() { require.NoError(t, store2.Close()) }()

	require.Equal(t, latestVersion, store2.LastCommitID().Version,
		"SC version after reopen should equal the last committed version")
	require.Equal(t, history[latestVersion].appHash, store2.lastCommitInfo.Hash(),
		"app hash after reopen should match the last committed app hash")

	// SS must have caught up (replay completed); otherwise historical SS
	// queries below will observe a stale view.
	waitUntilSSVersion(t, store2, latestVersion)

	// Historical SS reads for the committed versions must return the
	// originally committed values.
	for v := int64(1); v <= latestVersion; v++ {
		rec := history[v]
		if rec.hasCosmo {
			resp := store2.Query(abci.RequestQuery{
				Path:   "/acc/key",
				Data:   []byte("acct1"),
				Height: v,
				Prove:  false,
			})
			require.EqualValuesf(t, 0, resp.Code, "cosmos query v%d failed: %s", v, resp.Log)
			require.Equalf(t, rec.accVal, resp.Value,
				"SS cosmos value at v%d should match committed value", v)
		}
		if rec.hasEVM {
			resp := store2.Query(abci.RequestQuery{
				Path:   "/evm/key",
				Data:   evmData.storKey,
				Height: v,
				Prove:  false,
			})
			require.EqualValuesf(t, 0, resp.Code, "evm query v%d failed: %s", v, resp.Log)
			require.Equalf(t, rec.evmVal, resp.Value,
				"SS evm value at v%d should match committed value", v)
		}
	}
}
