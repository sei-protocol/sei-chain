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

func TestPendingIndexOverlaysNothingWhenThereIsNothingPending(t *testing.T) {
	base := fixedStateReader{}
	var none *pendingIndex
	require.Equal(t, StateReader(base), none.overlay(base))
	require.Equal(t, StateReader(base), newPendingIndex(&StateChangeSet{}).overlay(base))
}

// fundedStore is a view over stored account rows whose GetBalance answers a missing
// row with a funded fallback, as the EVM-only app's missing-account state does.
type fundedStore struct {
	fixedStateReader
	rows   map[common.Address]*big.Int
	funded *big.Int
}

func (r fundedStore) GetBalance(addr common.Address) *big.Int {
	if balance, ok := r.rows[addr]; ok {
		return new(big.Int).Set(balance)
	}
	return new(big.Int).Set(r.funded)
}

func (r fundedStore) StoredBalance(addr common.Address) *big.Int {
	if balance, ok := r.rows[addr]; ok {
		return new(big.Int).Set(balance)
	}
	return new(big.Int)
}

// createdWithoutValue is a block that creates a contract at addr and sends it nothing,
// so its recorded balance is whatever the fallback answered.
func createdWithoutValue(addr common.Address, funded *big.Int) *StateChangeSet {
	return &StateChangeSet{
		Accounts: []AccountChange{{
			Address: addr,
			Account: AccountState{Balance: new(big.Int).Set(funded), Nonce: 1, Code: []byte{0x60}},
			Prior:   AccountState{Balance: new(big.Int).Set(funded)},
		}},
	}
}

func TestPipelineOverlayServesTheStoredBalanceTheBlockLeftUnchanged(t *testing.T) {
	addr := common.HexToAddress("0x1")
	funded := new(big.Int).Lsh(big.NewInt(1), 200)
	for name, base := range map[string]fundedStore{
		"commit pending": {funded: funded},
		"commit landed":  {rows: map[common.Address]*big.Int{addr: new(big.Int)}, funded: funded},
	} {
		t.Run(name, func(t *testing.T) {
			overlay := newPendingIndex(createdWithoutValue(addr, funded)).overlay(base)
			require.Equal(t, 0, overlay.GetBalance(addr).Sign())
			require.Equal(t, uint64(1), overlay.GetNonce(addr))
			require.Equal(t, []byte{0x60}, overlay.GetCode(addr))
			account, _, ok := overlay.(baseAccountReader).ReadAccount(addr)
			require.True(t, ok)
			require.Equal(t, 0, account.Balance.Sign())
			require.Equal(t, uint64(1), account.Nonce)
		})
	}
}

func TestPipelineOverlayServesABalanceTheBlockWrote(t *testing.T) {
	addr := common.HexToAddress("0x1")
	funded := new(big.Int).Lsh(big.NewInt(1), 200)
	spent := new(big.Int).Sub(new(big.Int).Set(funded), big.NewInt(21_000))
	overlay := newPendingIndex(&StateChangeSet{
		Accounts: []AccountChange{{
			Address: addr,
			Account: AccountState{Balance: spent, Nonce: 1},
			Prior:   AccountState{Balance: new(big.Int).Set(funded)},
		}},
	}).overlay(fundedStore{funded: funded})

	require.Equal(t, spent, overlay.GetBalance(addr))
	account, _, ok := overlay.(baseAccountReader).ReadAccount(addr)
	require.True(t, ok)
	require.Equal(t, spent, account.Balance)
}

// An overlay of the block's own changes serves what the block's transactions saw.
func TestBlockOverlayServesTheRecordedBalance(t *testing.T) {
	addr := common.HexToAddress("0x1")
	funded := new(big.Int).Lsh(big.NewInt(1), 200)
	overlay := newPendingOverlay(fundedStore{funded: funded}, createdWithoutValue(addr, funded))
	require.Equal(t, funded, overlay.GetBalance(addr))
}

// A block overlay stacked on the pipeline overlay reads the previous block's accounts as it
// would once that commit has landed.
func TestBlockOverlayOnThePipelineOverlayMatchesTheLandedCommit(t *testing.T) {
	created := common.HexToAddress("0x1")
	other := common.HexToAddress("0x2")
	funded := new(big.Int).Lsh(big.NewInt(1), 200)
	thisBlock := &StateChangeSet{Storage: []StorageChange{{Address: other, Key: common.HexToHash("0xaa"), Value: common.HexToHash("0x1")}}}

	pending := newPendingOverlay(newPendingIndex(createdWithoutValue(created, funded)).overlay(fundedStore{funded: funded}), thisBlock)
	landed := newPendingOverlay(fundedStore{rows: map[common.Address]*big.Int{created: new(big.Int)}, funded: funded}, thisBlock)
	require.Equal(t, 0, pending.GetBalance(created).Sign())
	require.Equal(t, landed.GetBalance(created), pending.GetBalance(created))
}

func TestPendingIndexServesTheSameStateAsADirectOverlay(t *testing.T) {
	addr := common.HexToAddress("0x1")
	slot := common.HexToHash("0xaa")
	deleted := common.HexToHash("0xbb")
	base := fixedStateReader{
		balances: map[common.Address]*big.Int{addr: big.NewInt(1)},
		storage: map[storageChangeKey]common.Hash{
			{address: addr, key: slot}:    common.HexToHash("0x1"),
			{address: addr, key: deleted}: common.HexToHash("0x2"),
		},
	}
	changes := &StateChangeSet{
		Accounts: []AccountChange{{Address: addr, Account: AccountState{Balance: big.NewInt(99), Nonce: 5}}},
		Storage: []StorageChange{
			{Address: addr, Key: slot, Value: common.HexToHash("0x99")},
			{Address: addr, Key: deleted, Delete: true},
		},
	}

	direct := newPendingOverlay(base, changes)
	indexed := newPendingIndex(changes).overlay(base)
	require.Equal(t, direct.GetBalance(addr), indexed.GetBalance(addr))
	require.Equal(t, direct.GetNonce(addr), indexed.GetNonce(addr))
	require.Equal(t, direct.GetState(addr, slot), indexed.GetState(addr, slot))
	require.Equal(t, direct.GetState(addr, deleted), indexed.GetState(addr, deleted))
}
