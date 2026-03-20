package state

import (
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestAccessListAddAccountChangeRevert(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	addr := common.Address{1}
	db.tempState.transientAccessLists.Addresses[addr] = 0
	change := &accessListAddAccountChange{address: addr}
	change.revert(db)
	_, ok := db.tempState.transientAccessLists.Addresses[addr]
	require.False(t, ok)
}

func TestAccessListAddSlotChangeRevert(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	addr := common.Address{2}
	slot := common.Hash{3}
	// Set up the access list properly
	db.tempState.transientAccessLists.Addresses[addr] = 0
	slots := map[common.Hash]struct{}{slot: {}}
	db.tempState.transientAccessLists.Slots = []map[common.Hash]struct{}{slots}
	change := &accessListAddSlotChange{address: addr, slot: slot}
	change.revert(db)
	// Verify the slot was removed
	require.Len(t, db.tempState.transientAccessLists.Slots, 0)
}

func TestSurplusChangeRevert(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	delta := sdk.NewInt(5)
	db.tempState.surplus = sdk.NewInt(10)
	change := &surplusChange{delta: delta}
	change.revert(db)
	require.Equal(t, sdk.NewInt(5), db.tempState.surplus)
}

func TestAddLogChangeRevert(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	db.tempState.logs = append(db.tempState.logs, nil, nil)
	change := &addLogChange{}
	change.revert(db)
	require.Len(t, db.tempState.logs, 1)
}

func TestRefundChangeRevert(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	prev := uint64(42)
	change := &refundChange{prev: prev}
	change.revert(db)
	bz := db.tempState.transientModuleStates[string(GasRefundKey)]
	require.Equal(t, prev, binary.BigEndian.Uint64(bz))
}

func TestTransientStorageChangeRevert_Delete(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	addr := common.Address{4}
	key := common.Hash{5}
	states := map[string]common.Hash{key.Hex(): {6}}
	db.tempState.transientStates[addr.Hex()] = states
	change := &transientStorageChange{account: addr, key: key, prevalue: common.Hash{}}
	change.revert(db)
	_, ok := db.tempState.transientStates[addr.Hex()]
	require.False(t, ok)
}

func TestTransientStorageChangeRevert_Update(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	addr := common.Address{7}
	key := common.Hash{8}
	states := map[string]common.Hash{}
	db.tempState.transientStates[addr.Hex()] = states
	prevalue := common.Hash{9}
	change := &transientStorageChange{account: addr, key: key, prevalue: prevalue}
	change.revert(db)
	require.Equal(t, prevalue, db.tempState.transientStates[addr.Hex()][key.Hex()])
}

func TestWatermarkRevert(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	change := &watermark{version: 1}
	change.revert(db) // should do nothing, just ensure no panic
}

func TestAccountStatusChangeRevert_Delete(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	addr := common.Address{10}
	db.tempState.transientAccounts[addr.Hex()] = []byte{1, 2, 3}
	change := &accountStatusChange{account: addr, prev: nil}
	change.revert(db)
	_, ok := db.tempState.transientAccounts[addr.Hex()]
	require.False(t, ok)
}

func TestAccountStatusChangeRevert_Update(t *testing.T) {
	db := &DBImpl{tempState: NewTemporaryState()}
	addr := common.Address{11}
	prev := []byte{4, 5, 6}
	change := &accountStatusChange{account: addr, prev: prev}
	change.revert(db)
	require.Equal(t, prev, db.tempState.transientAccounts[addr.Hex()])
}
