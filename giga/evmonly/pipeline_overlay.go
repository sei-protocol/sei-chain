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

	balances map[common.Address]*big.Int
	nonces   map[common.Address]uint64
	code     map[common.Address][]byte
	storage  map[storageChangeKey]common.Hash
	// Addresses whose storage the block wiped, where a slot the block did not rewrite reads as unset
	// rather than falling through to the view.
	cleared map[common.Address]struct{}
}

// newPendingOverlay indexes changes for lookup. It returns base unchanged when there is nothing to
// overlay, so a caller pays nothing for the first block or after a commit has caught up.
func newPendingOverlay(base StateReader, changes *StateChangeSet) StateReader {
	if changes == nil || changes.IsEmpty() {
		return base
	}
	o := &pendingOverlay{
		base:     base,
		balances: make(map[common.Address]*big.Int, len(changes.Balances)),
		nonces:   make(map[common.Address]uint64, len(changes.Nonces)),
		code:     make(map[common.Address][]byte, len(changes.Code)),
		storage:  make(map[storageChangeKey]common.Hash, len(changes.Storage)),
		cleared:  make(map[common.Address]struct{}, len(changes.StorageClears)),
	}
	for _, change := range changes.Balances {
		o.balances[change.Address] = change.Balance
	}
	for _, change := range changes.Nonces {
		o.nonces[change.Address] = change.Nonce
	}
	for _, change := range changes.Code {
		if change.Delete {
			o.code[change.Address] = nil
			continue
		}
		o.code[change.Address] = change.Code
	}
	for _, addr := range changes.StorageClears {
		o.cleared[addr] = struct{}{}
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
	if balance, ok := o.balances[addr]; ok {
		return cloneBig(balance)
	}
	return o.base.GetBalance(addr)
}

func (o *pendingOverlay) GetNonce(addr common.Address) uint64 {
	if nonce, ok := o.nonces[addr]; ok {
		return nonce
	}
	return o.base.GetNonce(addr)
}

func (o *pendingOverlay) GetCode(addr common.Address) []byte {
	if code, ok := o.code[addr]; ok {
		return cloneBytes(code)
	}
	return o.base.GetCode(addr)
}

func (o *pendingOverlay) GetState(addr common.Address, key common.Hash) common.Hash {
	if value, ok := o.storage[storageChangeKey{address: addr, key: key}]; ok {
		return value
	}
	if _, wiped := o.cleared[addr]; wiped {
		return common.Hash{}
	}
	return o.base.GetState(addr, key)
}

// ReadAccount satisfies baseAccountReader, so a block reading through an overlay keeps the
// single-row read the view beneath it offers.
func (o *pendingOverlay) ReadAccount(addr common.Address) (baseAccount, bool) {
	_, hasBalance := o.balances[addr]
	_, hasNonce := o.nonces[addr]
	_, hasCode := o.code[addr]
	if !hasBalance && !hasNonce && !hasCode {
		if reader, ok := o.base.(baseAccountReader); ok {
			return reader.ReadAccount(addr)
		}
		return baseAccount{}, false
	}
	// Touched by the pending block, so the row beneath is only part of the answer. A base that
	// declines the combined read still has to answer field by field, or a field the pending block
	// left alone would read as zero instead of what the base holds.
	var account baseAccount
	served := false
	if reader, ok := o.base.(baseAccountReader); ok {
		account, served = reader.ReadAccount(addr)
	}
	if !served {
		account = baseAccount{
			Balance: o.base.GetBalance(addr),
			Nonce:   o.base.GetNonce(addr),
			Code:    o.base.GetCode(addr),
		}
	}
	if balance, ok := o.balances[addr]; ok {
		account.Balance = cloneBig(balance)
	}
	if nonce, ok := o.nonces[addr]; ok {
		account.Nonce = nonce
	}
	if code, ok := o.code[addr]; ok {
		account.Code = cloneBytes(code)
	}
	return account, true
}
