package params

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// memState is a storage-only precompiles.State.
type memState struct {
	storage map[common.Address]map[common.Hash]common.Hash
}

var _ precompiles.State = (*memState)(nil)

func newMemState() *memState {
	return &memState{storage: map[common.Address]map[common.Hash]common.Hash{}}
}

func (m *memState) GetState(a common.Address, k common.Hash) common.Hash {
	return m.storage[a][k]
}

func (m *memState) SetState(a common.Address, k, v common.Hash) {
	if m.storage[a] == nil {
		m.storage[a] = map[common.Hash]common.Hash{}
	}
	if v == (common.Hash{}) {
		delete(m.storage[a], k)
		return
	}
	m.storage[a][k] = v
}

func (m *memState) GetBalance(common.Address) *big.Int        { panic("unsupported") }
func (m *memState) AddBalance(common.Address, *big.Int)       { panic("unsupported") }
func (m *memState) SubBalance(common.Address, *big.Int) error { panic("unsupported") }
func (m *memState) GetNonce(common.Address) uint64            { panic("unsupported") }
func (m *memState) SetNonce(common.Address, uint64)           { panic("unsupported") }
func (m *memState) GetCode(common.Address) []byte             { panic("unsupported") }

// stateDBView reads storage from a geth StateDB. Only GetState is supported.
type stateDBView struct {
	db vm.StateDB
}

func (v stateDBView) GetState(a common.Address, k common.Hash) common.Hash {
	return v.db.GetState(a, k)
}

func (v stateDBView) SetState(common.Address, common.Hash, common.Hash) { panic("read-only") }
func (v stateDBView) GetBalance(common.Address) *big.Int                { panic("unsupported") }
func (v stateDBView) AddBalance(common.Address, *big.Int)               { panic("unsupported") }
func (v stateDBView) SubBalance(common.Address, *big.Int) error         { panic("unsupported") }
func (v stateDBView) GetNonce(common.Address) uint64                    { panic("unsupported") }
func (v stateDBView) SetNonce(common.Address, uint64)                   { panic("unsupported") }
func (v stateDBView) GetCode(common.Address) []byte                     { panic("unsupported") }
