package giga

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

var _ gigatypes.StateView = (*historicalView)(nil)

// historicalView serves the Giga read API at a past height from the EVM state store's history. The
// EVM accessors read the same logical keys the executor writes, so they answer what those keys held
// after the view's block.
type historicalView struct {
	height int64

	// read returns the EVM key's value at height, nil when it did not exist then.
	read func(key []byte) ([]byte, error)

	// release frees whatever the view pins. Guarded by closeOnce.
	release   func()
	closeOnce sync.Once
}

func (v *historicalView) GetBlockHeight() int64 {
	return v.height
}

// Get serves only the EVM module, the one the EVM state store keeps the history of.
func (v *historicalView) Get(module string, key []byte) ([]byte, bool) {
	if module != keys.EVMStoreKey {
		panic(fmt.Sprintf("giga: the EVM state store keeps no history of module %q", module))
	}
	value, err := v.read(key)
	if err != nil {
		// A view has no recoverable errors: a read it cannot serve is fatal to its caller.
		panic(fmt.Sprintf("giga: historical read of %s/%x at height %d: %v", module, key, v.height, err))
	}
	return value, value != nil
}

func (v *historicalView) Close() {
	v.closeOnce.Do(v.release)
}

// evm reads the EVM key of kind for keyBytes.
func (v *historicalView) evm(kind keys.EVMKeyKind, keyBytes []byte) ([]byte, bool) {
	return v.Get(keys.EVMStoreKey, keys.BuildEVMKey(kind, keyBytes))
}

func (v *historicalView) AccountExists(addr gigatypes.Address) bool {
	_, ok := v.ReadAccount(addr)
	return ok
}

func (v *historicalView) GetStorage(addr gigatypes.Address, slot gigatypes.Hash) gigatypes.Hash {
	value, _ := v.evm(keys.EVMKeyStorage, append(addr[:], slot[:]...))
	return common.BytesToHash(value)
}

func (v *historicalView) GetBalance(addr gigatypes.Address) gigatypes.Hash {
	value, _ := v.evm(keys.EVMKeyBalance, addr[:])
	return common.BytesToHash(value)
}

func (v *historicalView) GetNonce(addr gigatypes.Address) uint64 {
	value, found := v.evm(keys.EVMKeyNonce, addr[:])
	if !found || len(value) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func (v *historicalView) GetCodeSize(addr gigatypes.Address) int {
	return len(v.GetCode(addr))
}

func (v *historicalView) GetCodeHash(addr gigatypes.Address) gigatypes.Hash {
	account, ok := v.ReadAccount(addr)
	if !ok {
		return gigatypes.Hash{}
	}
	return account.CodeHash
}

func (v *historicalView) GetCode(addr gigatypes.Address) []byte {
	value, _ := v.evm(keys.EVMKeyCode, addr[:])
	return value
}

// ReadAccount reads the three account fields at the view's height. An account exists when any of
// them is non-zero, as the state commit store keeps a row only for such an account; one with no code
// hash stored has no code.
func (v *historicalView) ReadAccount(addr gigatypes.Address) (gigatypes.Account, bool) {
	balance, _ := v.evm(keys.EVMKeyBalance, addr[:])
	nonce, _ := v.evm(keys.EVMKeyNonce, addr[:])
	codeHash, hasCodeHash := v.evm(keys.EVMKeyCodeHash, addr[:])
	account := gigatypes.Account{
		Balance:  common.BytesToHash(balance),
		CodeHash: gigatypes.EmptyCodeHash,
	}
	if len(nonce) == 8 {
		account.Nonce = binary.BigEndian.Uint64(nonce)
	}
	if hasCodeHash {
		account.CodeHash = common.BytesToHash(codeHash)
	}
	if account.Balance == (gigatypes.Hash{}) && account.Nonce == 0 && !hasCodeHash {
		return gigatypes.Account{}, false
	}
	return account, true
}
