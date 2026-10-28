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

	accounts map[common.Address]AccountState
	storage  map[storageChangeKey]common.Hash
}

// newPendingOverlay indexes changes for lookup. It returns base unchanged when there is nothing to
// overlay, so a caller pays nothing for the first block or after a commit has caught up.
func newPendingOverlay(base StateReader, changes *StateChangeSet) StateReader {
	if changes == nil || changes.IsEmpty() {
		return base
	}
	o := &pendingOverlay{
		base:     base,
		accounts: make(map[common.Address]AccountState, len(changes.Accounts)),
		storage:  make(map[storageChangeKey]common.Hash, len(changes.Storage)),
	}
	for _, change := range changes.Accounts {
		o.accounts[change.Address] = change.Account
	}
	for _, change := range changes.Storage {
		if change.Delete {
			o.storage[storageChangeKey{address: change.Address, key: change.Key}] = common.Hash{}
			continue
		}
		o.storage[storageChangeKey{address: change.Address, key: change.Key}] = change.Value
	}
	return o
}

func (o *pendingOverlay) GetBalance(addr common.Address) *big.Int {
	if account, ok := o.accounts[addr]; ok {
		return cloneBig(account.Balance)
	}
	return o.base.GetBalance(addr)
}

func (o *pendingOverlay) GetNonce(addr common.Address) uint64 {
	if account, ok := o.accounts[addr]; ok {
		return account.Nonce
	}
	return o.base.GetNonce(addr)
}

func (o *pendingOverlay) GetCode(addr common.Address) []byte {
	if account, ok := o.accounts[addr]; ok {
		return cloneBytes(account.Code)
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
		return AccountState{Balance: cloneBig(account.Balance), Nonce: account.Nonce, Code: cloneBytes(account.Code)}, common.Hash{}, true
	}
	if reader, ok := o.base.(baseAccountReader); ok {
		return reader.ReadAccount(addr)
	}
	return AccountState{}, common.Hash{}, false
}
