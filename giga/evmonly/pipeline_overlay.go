package evmonly

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// pendingOverlay serves the state a committed block produced before its commit has landed, reading
// through to the view beneath it for everything that block did not touch.
//
// It exists so a block can execute while the previous one is still being written. A StateView is a
// point-in-time snapshot that "never observes writes made after the view was opened", so a view
// taken before that commit cannot answer for it; the overlay supplies exactly the difference.
//
// It is read-only and safe for concurrent readers once built, which is what lets the OCC workers
// share it. Its maps are shared for the whole block, so it hands out copies: a caller that mutated
// a returned balance or code in place would corrupt the pending state for every other reader.
type pendingOverlay struct {
	base StateReader

	accounts map[common.Address]overlayAccount
	storage  map[storageChangeKey]common.Hash
}

// overlayAccount is one account the pending block changed. balanceChanged reports
// whether that block wrote the balance; an unchanged balance is not in the commit.
type overlayAccount struct {
	state          AccountState
	balanceChanged bool
}

// accountRow reports whether an account row is stored. A missing-account fallback
// is not a row.
type accountRow interface {
	HasAccount(common.Address) bool
}

// newPendingOverlay indexes changes for lookup. It returns base unchanged when there is nothing to
// overlay, so a caller pays nothing for the first block or after a commit has caught up.
func newPendingOverlay(base StateReader, changes *StateChangeSet) StateReader {
	if changes == nil || changes.IsEmpty() {
		return base
	}
	accounts, storage := indexChanges(changes)
	return &pendingOverlay{base: base, accounts: accounts, storage: storage}
}

// indexChanges maps changes' accounts by address and their storage by address and slot.
func indexChanges(changes *StateChangeSet) (map[common.Address]overlayAccount, map[storageChangeKey]common.Hash) {
	accounts := make(map[common.Address]overlayAccount, len(changes.Accounts))
	storage := make(map[storageChangeKey]common.Hash, len(changes.Storage))
	for _, change := range changes.Accounts {
		accounts[change.Address] = overlayAccount{state: change.Account, balanceChanged: change.BalanceChanged()}
	}
	for _, change := range changes.Storage {
		if change.Delete {
			storage[storageChangeKey{address: change.Address, key: change.Key}] = common.Hash{}
			continue
		}
		storage[storageChangeKey{address: change.Address, key: change.Key}] = change.Value
	}
	return accounts, storage
}

// pendingIndex is a committing block's changes, indexed on a goroutine of its own for the overlay the
// next block reads through. A nil pendingIndex overlays nothing.
type pendingIndex struct {
	changes *StateChangeSet
	// ready is closed once accounts and storage are built.
	ready    chan struct{}
	accounts map[common.Address]overlayAccount
	storage  map[storageChangeKey]common.Hash
}

// newPendingIndex starts indexing changes, which must not be modified afterwards.
func newPendingIndex(changes *StateChangeSet) *pendingIndex {
	idx := &pendingIndex{changes: changes, ready: make(chan struct{})}
	go func() {
		defer close(idx.ready)
		idx.accounts, idx.storage = indexChanges(changes)
	}()
	return idx
}

// overlay returns base read through the indexed changes, waiting for the index if it is not built
// yet, and base unchanged when there is nothing to overlay.
func (idx *pendingIndex) overlay(base StateReader) StateReader {
	if idx == nil || idx.changes.IsEmpty() {
		return base
	}
	<-idx.ready
	return &pendingOverlay{base: base, accounts: idx.accounts, storage: idx.storage}
}

func (o *pendingOverlay) GetBalance(addr common.Address) *big.Int {
	if account, ok := o.accounts[addr]; ok {
		return cloneBig(o.accountBalance(addr, account))
	}
	return o.base.GetBalance(addr)
}

// accountBalance returns the balance a later block reads. It is 0 when the pending
// block left the balance unchanged and the view has no account row.
func (o *pendingOverlay) accountBalance(addr common.Address, account overlayAccount) *big.Int {
	if account.balanceChanged || o.baseHasAccount(addr) {
		return account.state.Balance
	}
	return new(big.Int)
}

// baseHasAccount reports whether the view already stores addr. A reader that cannot
// say is treated as storing it.
func (o *pendingOverlay) baseHasAccount(addr common.Address) bool {
	row, ok := o.base.(accountRow)
	return !ok || row.HasAccount(addr)
}

func (o *pendingOverlay) GetNonce(addr common.Address) uint64 {
	if account, ok := o.accounts[addr]; ok {
		return account.state.Nonce
	}
	return o.base.GetNonce(addr)
}

func (o *pendingOverlay) GetCode(addr common.Address) []byte {
	if account, ok := o.accounts[addr]; ok {
		return cloneBytes(account.state.Code)
	}
	return o.base.GetCode(addr)
}

func (o *pendingOverlay) GetState(addr common.Address, key common.Hash) common.Hash {
	if value, ok := o.storage[storageChangeKey{address: addr, key: key}]; ok {
		return value
	}
	return o.base.GetState(addr, key)
}

// ReadAccount satisfies baseAccountReader, so a block reading through an overlay keeps the
// single-row read the view beneath it offers.
func (o *pendingOverlay) ReadAccount(addr common.Address) (AccountState, common.Hash, bool) {
	if account, ok := o.accounts[addr]; ok {
		return AccountState{
			Balance: cloneBig(o.accountBalance(addr, account)),
			Nonce:   account.state.Nonce,
			Code:    cloneBytes(account.state.Code),
		}, common.Hash{}, true
	}
	if reader, ok := o.base.(baseAccountReader); ok {
		return reader.ReadAccount(addr)
	}
	return AccountState{}, common.Hash{}, false
}
