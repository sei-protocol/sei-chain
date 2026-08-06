package state_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/stretchr/testify/require"
)

func TestCode(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
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
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	code := []byte{1, 2, 3, 4, 5}
	statedb.SetCode(addr, code)
	require.Equal(t, code, statedb.GetCode(addr))
	require.Equal(t, code, statedb.GetCode(addr))

	updated := []byte{9, 8, 7}
	statedb.SetCode(addr, updated)
	require.Equal(t, updated, statedb.GetCode(addr))
	require.Equal(t, crypto.Keccak256Hash(updated), statedb.GetCodeHash(addr))
}

func TestCodeCacheClearedOnRevert(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
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

// TestCodeCacheUnrelatedWarmSurvivesRevert pins address-level journal restore
// (parity with x/evm/state): nested reverts must not wipe unrelated warms.
func TestCodeCacheUnrelatedWarmSurvivesRevert(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
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
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
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
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	statedb.SetCode(addr, []byte{})
	require.Nil(t, statedb.GetCode(addr))
	require.Equal(t, 0, statedb.GetCodeSize(addr))
	require.Nil(t, statedb.GetCode(addr))
}

func TestCodeCacheMissHitFromPreSeededKeeperCode(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()

	code := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	k.SetCode(ctx, addr, code)

	statedb := state.NewDBImpl(ctx, k, false)
	require.Equal(t, code, statedb.GetCode(addr))
	require.Equal(t, code, statedb.GetCode(addr))
	require.Equal(t, len(code), statedb.GetCodeSize(addr))
}

func TestCodeCacheCopyStartsEmptyAndIndependent(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	parent := state.NewDBImpl(ctx, k, false)

	initial := []byte{1, 2, 3}
	parent.SetCode(addr, initial)
	require.Equal(t, initial, parent.GetCode(addr))

	child := parent.Copy().(*state.DBImpl)
	require.Equal(t, initial, child.GetCode(addr))

	updated := []byte{9, 9, 9}
	parent.SetCode(addr, updated)
	require.Equal(t, updated, parent.GetCode(addr))
	require.Equal(t, initial, child.GetCode(addr))

	childUpdated := []byte{7, 7}
	child.SetCode(addr, childUpdated)
	require.Equal(t, childUpdated, child.GetCode(addr))
	require.Equal(t, updated, parent.GetCode(addr))
}

func TestCodeCacheDisabledForSimulation(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	ctx = ctx.WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()

	initial := []byte{1, 2, 3}
	updated := []byte{9, 9, 9, 9}

	deliver := state.NewDBImpl(ctx, k, false)
	deliver.SetCode(addr, initial)
	require.Equal(t, initial, deliver.GetCode(addr))
	deliver.SetCode(addr, updated)
	require.Equal(t, updated, deliver.GetCode(addr))

	_, addr2 := testkeeper.MockAddressPair()
	deliver.SetCode(addr2, initial)
	require.Equal(t, initial, deliver.GetCode(addr2))
	k.SetCode(deliver.Ctx(), addr2, updated)
	deliver.RefreshCodeCache(addr2, updated)
	require.Equal(t, updated, deliver.GetCode(addr2))

	sim := state.NewDBImpl(ctx, k, true)
	sim.SetCode(addr, initial)
	require.Equal(t, initial, sim.GetCode(addr))
	k.SetCode(sim.Ctx(), addr, updated)
	require.Equal(t, updated, sim.GetCode(addr))
}
