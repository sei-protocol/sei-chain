package state_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestCode(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	require.Equal(t, common.Hash{}, statedb.GetCodeHash(addr))
	require.Nil(t, statedb.GetCode(addr))
	require.Equal(t, 0, statedb.GetCodeSize(addr))

	code := []byte{1, 2, 3, 4, 5}
	statedb.SetCode(addr, code)
	require.Equal(t, crypto.Keccak256Hash(code), statedb.GetCodeHash(addr))
	require.Equal(t, code, statedb.GetCode(addr))
	require.Equal(t, 5, statedb.GetCodeSize(addr))
}

func TestCodeCacheHitAndSetCodeUpdate(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	code := []byte{1, 2, 3, 4, 5}
	statedb.SetCode(addr, code)
	require.Equal(t, code, statedb.GetCode(addr))
	// Second read must return the same bytes (served from the tx code cache).
	require.Equal(t, code, statedb.GetCode(addr))

	updated := []byte{9, 8, 7}
	statedb.SetCode(addr, updated)
	require.Equal(t, updated, statedb.GetCode(addr))
	require.Equal(t, crypto.Keccak256Hash(updated), statedb.GetCodeHash(addr))
}

func TestCodeCacheClearedOnRevert(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	initial := []byte{1, 2, 3}
	statedb.SetCode(addr, initial)
	require.Equal(t, initial, statedb.GetCode(addr))

	rev := statedb.Snapshot()
	updated := []byte{4, 5, 6, 7}
	statedb.SetCode(addr, updated)
	require.Equal(t, updated, statedb.GetCode(addr))

	statedb.RevertToSnapshot(rev)
	require.Equal(t, initial, statedb.GetCode(addr))
}

// TestCodeCacheUnrelatedWarmSurvivesRevert pins address-level journal restore:
// nested reverts must not wipe warmed entries for accounts that were not
// mutated, otherwise an attacker can force wholesale code re-fetch via
// reverting subcalls. After revert we poison the Multistore for the
// unrelated address without RefreshCodeCache; a surviving memo returns the
// pre-poison bytes, while a blanket clear would re-read the poison.
func TestCodeCacheUnrelatedWarmSurvivesRevert(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addrA := testkeeper.MockAddressPair()
	_, addrB := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	codeA := []byte{1, 2, 3}
	codeB := []byte{10, 11, 12, 13}
	statedb.SetCode(addrA, codeA)
	statedb.SetCode(addrB, codeB)
	require.Equal(t, codeB, statedb.GetCode(addrB))

	rev := statedb.Snapshot()
	statedb.SetCode(addrA, []byte{9, 9, 9})
	statedb.RevertToSnapshot(rev)
	require.Equal(t, codeA, statedb.GetCode(addrA))

	poison := []byte{7, 7, 7, 7, 7}
	k.SetCode(statedb.Ctx(), addrB, poison)
	require.Equal(t, codeB, statedb.GetCode(addrB), "unrelated warm must survive revert (not re-read poison)")
	require.Equal(t, poison, k.GetCode(statedb.Ctx(), addrB))
}

func TestCodeCacheInvalidatedOnCreateAccount(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	statedb.CreateAccount(addr)
	statedb.SetCode(addr, []byte("code"))
	require.Equal(t, []byte("code"), statedb.GetCode(addr))

	statedb.CreateAccount(addr)
	require.Nil(t, statedb.GetCode(addr))
	require.Equal(t, 0, statedb.GetCodeSize(addr))
}

func TestCodeCacheEmptyMatchesKeeperNil(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	statedb.SetCode(addr, []byte{})
	require.Nil(t, statedb.GetCode(addr))
	require.Equal(t, 0, statedb.GetCodeSize(addr))

	// Second read must still be nil (cached empty normalized to nil).
	require.Nil(t, statedb.GetCode(addr))
}

func TestCodeCacheMissHitFromPreSeededKeeperCode(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()

	// Seed bytecode via the keeper so StateDB's first GetCode is a true store miss
	// (the CALL-family hot path: repeated GetCode against already-committed contracts).
	code := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	k.SetCode(ctx, addr, code)

	statedb := state.NewDBImpl(ctx, k, false)
	require.Equal(t, code, statedb.GetCode(addr))
	require.Equal(t, code, statedb.GetCode(addr))
	require.Equal(t, len(code), statedb.GetCodeSize(addr))
}

func TestCodeCacheCopyStartsEmptyAndIndependent(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	parent := state.NewDBImpl(ctx, k, false)

	initial := []byte{1, 2, 3}
	parent.SetCode(addr, initial)
	require.Equal(t, initial, parent.GetCode(addr))

	child := parent.Copy().(*state.DBImpl)
	// Copy does not clone parent bytecode; child loads from Multistore then caches.
	require.Equal(t, initial, child.GetCode(addr))

	// Separate maps: after the child has warmed, parent writes do not poison it.
	updated := []byte{9, 9, 9}
	parent.SetCode(addr, updated)
	require.Equal(t, updated, parent.GetCode(addr))
	require.Equal(t, initial, child.GetCode(addr))

	childUpdated := []byte{7, 7}
	child.SetCode(addr, childUpdated)
	require.Equal(t, childUpdated, child.GetCode(addr))
	require.Equal(t, updated, parent.GetCode(addr))
}

func TestCodeCacheDisabledForSimulationAndWasmdEntry(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()

	initial := []byte{1, 2, 3}
	updated := []byte{9, 9, 9, 9}

	// Deliver DB: StateDB.SetCode must refresh the memo after a warm GetCode.
	deliver := state.NewDBImpl(ctx, k, false)
	deliver.SetCode(addr, initial)
	require.Equal(t, initial, deliver.GetCode(addr))
	deliver.SetCode(addr, updated)
	require.Equal(t, updated, deliver.GetCode(addr))
	require.Equal(t, len(updated), deliver.GetCodeSize(addr))

	// Keeper write billed on a separate meter: refresh memo without SetCode.
	_, addr2 := testkeeper.MockAddressPair()
	deliver.SetCode(addr2, initial)
	require.Equal(t, initial, deliver.GetCode(addr2))
	k.SetCode(deliver.Ctx(), addr2, updated)
	deliver.RefreshCodeCache(addr2, updated)
	require.Equal(t, updated, deliver.GetCode(addr2))
	require.Equal(t, len(updated), deliver.GetCodeSize(addr2))

	// Simulation/RPC DB always reads the store (caching disabled).
	sim := state.NewDBImpl(ctx, k, true)
	sim.SetCode(addr, initial)
	require.Equal(t, initial, sim.GetCode(addr))
	k.SetCode(sim.Ctx(), addr, updated)
	require.Equal(t, updated, sim.GetCode(addr))

	// Wasmd-entry deliver DB also skips the memo so nested CallEVM Finalizes
	// cannot leave a stale outer GetCode (see NewDBImpl).
	_, addr3 := testkeeper.MockAddressPair()
	wasmdEntry := state.NewDBImpl(ctx.WithEVMEntryViaWasmdPrecompile(true), k, false)
	wasmdEntry.SetCode(addr3, initial)
	require.Equal(t, initial, wasmdEntry.GetCode(addr3))
	k.SetCode(wasmdEntry.Ctx(), addr3, updated)
	require.Equal(t, updated, wasmdEntry.GetCode(addr3))
}
