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
		Accounts: []AccountChange{{
			Address: addr,
			Account: AccountState{Balance: big.NewInt(99), Nonce: 99, Code: []byte{0x99}},
			Prior:   AccountState{Balance: big.NewInt(1), Nonce: 1, Code: []byte{0x01}},
		}},
		Storage: []StorageChange{{Address: addr, Key: slot, Value: common.HexToHash("0x99")}},
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

func TestOverlayReportsDeletedCodeAsAbsent(t *testing.T) {
	addr := common.HexToAddress("0x1")
	base := fixedStateReader{code: map[common.Address][]byte{addr: {0x01}}}

	overlay := newPendingOverlay(base, &StateChangeSet{
		Accounts: []AccountChange{{Address: addr, Prior: AccountState{Code: []byte{0x01}}}},
	})
	require.Nil(t, overlay.GetCode(addr))
}
