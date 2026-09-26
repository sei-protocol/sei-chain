package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// fixedStateReader answers from fixed maps, standing in for the view an overlay reads through.
type fixedStateReader struct {
	balances map[common.Address]*big.Int
	nonces   map[common.Address]uint64
	code     map[common.Address][]byte
	storage  map[storageChangeKey]common.Hash
}

func (r fixedStateReader) GetBalance(addr common.Address) *big.Int {
	if balance, ok := r.balances[addr]; ok {
		return balance
	}
	return new(big.Int)
}

func (r fixedStateReader) GetNonce(addr common.Address) uint64 { return r.nonces[addr] }
func (r fixedStateReader) GetCode(addr common.Address) []byte  { return r.code[addr] }

func (r fixedStateReader) GetState(addr common.Address, key common.Hash) common.Hash {
	return r.storage[storageChangeKey{address: addr, key: key}]
}

func TestOverlayIsSkippedWhenThereIsNothingPending(t *testing.T) {
	base := fixedStateReader{}
	require.Equal(t, StateReader(base), newPendingOverlay(base, nil))
	require.Equal(t, StateReader(base), newPendingOverlay(base, &StateChangeSet{}))
}

func TestOverlayServesPendingChangesOverTheView(t *testing.T) {
	addr := common.HexToAddress("0x1")
	untouched := common.HexToAddress("0x2")
	slot := common.HexToHash("0xaa")

	base := fixedStateReader{
		balances: map[common.Address]*big.Int{addr: big.NewInt(1), untouched: big.NewInt(7)},
		nonces:   map[common.Address]uint64{addr: 1, untouched: 7},
		code:     map[common.Address][]byte{addr: {0x01}},
		storage:  map[storageChangeKey]common.Hash{{address: addr, key: slot}: common.HexToHash("0x1")},
	}
	overlay := newPendingOverlay(base, &StateChangeSet{
		Balances: []BalanceChange{{Address: addr, Balance: big.NewInt(99)}},
		Nonces:   []NonceChange{{Address: addr, Nonce: 99}},
		Code:     []CodeChange{{Address: addr, Code: []byte{0x99}}},
		Storage:  []StorageChange{{Address: addr, Key: slot, Value: common.HexToHash("0x99")}},
	})

	require.Equal(t, big.NewInt(99), overlay.GetBalance(addr))
	require.Equal(t, uint64(99), overlay.GetNonce(addr))
	require.Equal(t, []byte{0x99}, overlay.GetCode(addr))
	require.Equal(t, common.HexToHash("0x99"), overlay.GetState(addr, slot))

	// An address the pending block did not touch still reads through.
	require.Equal(t, big.NewInt(7), overlay.GetBalance(untouched))
	require.Equal(t, uint64(7), overlay.GetNonce(untouched))
}

func TestOverlayReportsADeletedSlotAsUnsetRatherThanFallingThrough(t *testing.T) {
	addr := common.HexToAddress("0x1")
	slot := common.HexToHash("0xaa")
	base := fixedStateReader{
		storage: map[storageChangeKey]common.Hash{{address: addr, key: slot}: common.HexToHash("0x1")},
	}

	overlay := newPendingOverlay(base, &StateChangeSet{
		Storage: []StorageChange{{Address: addr, Key: slot, Delete: true}},
	})
	require.Equal(t, common.Hash{}, overlay.GetState(addr, slot))
}

func TestOverlayReportsAWipedAccountsSlotsAsUnset(t *testing.T) {
	addr := common.HexToAddress("0x1")
	kept := common.HexToAddress("0x2")
	slot := common.HexToHash("0xaa")
	base := fixedStateReader{
		storage: map[storageChangeKey]common.Hash{
			{address: addr, key: slot}: common.HexToHash("0x1"),
			{address: kept, key: slot}: common.HexToHash("0x2"),
		},
	}

	overlay := newPendingOverlay(base, &StateChangeSet{StorageClears: []common.Address{addr}})
	require.Equal(t, common.Hash{}, overlay.GetState(addr, slot), "a wiped account's slot must not read through")
	require.Equal(t, common.HexToHash("0x2"), overlay.GetState(kept, slot))
}

// A block that writes several slots and then clears the account's storage must hand the overlay,
// and the store, only the clear plus what was written after it.
func TestMergedClearDropsTheWritesBeforeIt(t *testing.T) {
	addr := common.HexToAddress("0x1")
	a, b, c := common.HexToHash("0xa"), common.HexToHash("0xb"), common.HexToHash("0xc")
	base := NewMemoryState()
	base.SetState(addr, a, common.HexToHash("0x1"))

	state := newBlockSTMState(base)
	state.apply(occTxExecution{changeSet: StateChangeSet{Storage: []StorageChange{
		{Address: addr, Key: a, Value: common.HexToHash("0x11")},
		{Address: addr, Key: b, Value: common.HexToHash("0x12")},
		{Address: addr, Key: c, Value: common.HexToHash("0x13")},
	}}})
	state.apply(occTxExecution{changeSet: StateChangeSet{StorageClears: []common.Address{addr}}})
	state.apply(occTxExecution{changeSet: StateChangeSet{Storage: []StorageChange{
		{Address: addr, Key: b, Value: common.HexToHash("0x22")},
	}}})

	changes := state.ChangeSet()
	require.Equal(t, []common.Address{addr}, changes.StorageClears)
	require.Equal(t, []StorageChange{{Address: addr, Key: b, Value: common.HexToHash("0x22")}}, changes.Storage,
		"a slot written before the clear must not survive it")
}

// Reading through an overlay that holds a clear and a later write, only the later write shows: every
// other slot reads as unset, including slots the view beneath still holds.
func TestOverlayServesOnlyTheWritesAfterAClear(t *testing.T) {
	addr := common.HexToAddress("0x1")
	a, b, c := common.HexToHash("0xa"), common.HexToHash("0xb"), common.HexToHash("0xc")
	base := fixedStateReader{storage: map[storageChangeKey]common.Hash{
		{address: addr, key: a}: common.HexToHash("0x1"),
		{address: addr, key: b}: common.HexToHash("0x2"),
		{address: addr, key: c}: common.HexToHash("0x3"),
	}}

	overlay := newPendingOverlay(base, &StateChangeSet{
		StorageClears: []common.Address{addr},
		Storage:       []StorageChange{{Address: addr, Key: b, Value: common.HexToHash("0x22")}},
	})
	require.Equal(t, common.Hash{}, overlay.GetState(addr, a))
	require.Equal(t, common.HexToHash("0x22"), overlay.GetState(addr, b))
	require.Equal(t, common.Hash{}, overlay.GetState(addr, c))
}

func TestOverlayReportsDeletedCodeAsAbsent(t *testing.T) {
	addr := common.HexToAddress("0x1")
	base := fixedStateReader{code: map[common.Address][]byte{addr: {0x01}}}

	overlay := newPendingOverlay(base, &StateChangeSet{
		Code: []CodeChange{{Address: addr, Delete: true}},
	})
	require.Nil(t, overlay.GetCode(addr))
}

// A changeset is cloned because the block result it came from returns to a pool and is reset.
func TestCloneSurvivesTheBlockResultBeingReused(t *testing.T) {
	addr := common.HexToAddress("0x1")
	original := &StateChangeSet{
		Balances: []BalanceChange{{Address: addr, Balance: big.NewInt(5)}},
		Code:     []CodeChange{{Address: addr, Code: []byte{0x01}}},
	}
	clone := original.clone()

	original.Balances[0].Balance.SetInt64(999)
	original.Code[0].Code[0] = 0xff
	original.Balances = original.Balances[:0]

	require.Equal(t, big.NewInt(5), clone.Balances[0].Balance)
	require.Equal(t, []byte{0x01}, clone.Code[0].Code)
}
