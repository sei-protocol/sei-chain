package state_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestAddAddressToAccessList(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	statedb := state.NewDBImpl(ctx, k, false)

	_, addr := testkeeper.MockAddressPair()
	require.False(t, statedb.AddressInAccessList(addr))
	statedb.AddAddressToAccessList(addr)
	require.Nil(t, statedb.Err())
	require.True(t, statedb.AddressInAccessList(addr))

	// add same address again
	statedb.AddAddressToAccessList(addr)
	require.Nil(t, statedb.Err())
	require.True(t, statedb.AddressInAccessList(addr))
}

func TestAddSlotToAccessList(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	statedb := state.NewDBImpl(ctx, k, false)

	_, addr := testkeeper.MockAddressPair()
	statedb.AddAddressToAccessList(addr)

	slot := common.BytesToHash([]byte("abc"))
	addAndCheckSlot(t, statedb, addr, true, slot, false)

	slot2 := common.BytesToHash([]byte("def"))
	addAndCheckSlot(t, statedb, addr, true, slot2, false)

	existingSlot := slot
	addAndCheckSlot(t, statedb, addr, true, existingSlot, true)
}

func TestAddSlotToAccessListWithNonExistentAddress(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	statedb := state.NewDBImpl(ctx, k, false)

	_, addr := testkeeper.MockAddressPair()

	slot := common.BytesToHash([]byte("abc"))
	addAndCheckSlot(t, statedb, addr, false, slot, false)
}

func TestPrepare(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	statedb := state.NewDBImpl(ctx, k, false)

	_, sender := testkeeper.MockAddressPair()
	_, coinbase := testkeeper.MockAddressPair()
	_, dest := testkeeper.MockAddressPair()
	p1 := common.BytesToAddress([]byte{1})
	p2 := common.BytesToAddress([]byte{2})
	p3 := common.BytesToAddress([]byte{3})
	precompiles := []common.Address{p1, p2, p3}
	_, access1 := testkeeper.MockAddressPair()
	_, access2 := testkeeper.MockAddressPair()
	txaccesses := []ethtypes.AccessTuple{
		{Address: access1, StorageKeys: []common.Hash{common.BytesToHash([]byte("abc"))}},
		{
			Address: access2,
			StorageKeys: []common.Hash{
				common.BytesToHash([]byte("def")),
				common.BytesToHash([]byte("ghi")),
			},
		},
	}
	shanghai := params.Rules{ChainID: k.ChainID(ctx), IsShanghai: true}
	statedb.Prepare(
		shanghai, sender, coinbase, &dest, precompiles, txaccesses,
	)
	inAccessList := []common.Address{sender, dest, p1, p2, p3, access1, access2, coinbase}
	for _, addr := range inAccessList {
		require.True(t, statedb.AddressInAccessList(addr))
	}
	slotInAccessList := []struct {
		addr common.Address
		slot common.Hash
	}{
		{access1, common.BytesToHash([]byte("abc"))},
		{access2, common.BytesToHash([]byte("def"))},
		{access2, common.BytesToHash([]byte("ghi"))},
	}
	for _, el := range slotInAccessList {
		addrOk, slotOk := statedb.SlotInAccessList(el.addr, el.slot)
		require.True(t, addrOk)
		require.True(t, slotOk)
	}
}

func addAndCheckSlot(t *testing.T, statedb *state.DBImpl, addr common.Address, addrInAl bool, slot common.Hash, slotInAl bool) {
	addrOk, slotOk := statedb.SlotInAccessList(addr, slot)
	require.Equal(t, addrOk, addrInAl)
	require.Equal(t, slotOk, slotInAl)
	statedb.AddSlotToAccessList(addr, slot)
	addrOk, slotOk = statedb.SlotInAccessList(addr, slot)
	require.True(t, addrOk)
	require.Nil(t, statedb.Err())
	require.True(t, slotOk)
}
