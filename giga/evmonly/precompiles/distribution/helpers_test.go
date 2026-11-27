package distribution

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// memState is a map-backed precompiles.State.
type memState struct {
	balances map[common.Address]*big.Int
	storage  map[common.Address]map[common.Hash]common.Hash
}

var _ precompiles.State = (*memState)(nil)

func newMemState() *memState {
	return &memState{
		balances: map[common.Address]*big.Int{},
		storage:  map[common.Address]map[common.Hash]common.Hash{},
	}
}

func (m *memState) GetBalance(a common.Address) *big.Int {
	if b, ok := m.balances[a]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (m *memState) AddBalance(a common.Address, v *big.Int) {
	m.balances[a] = new(big.Int).Add(m.GetBalance(a), v)
}

func (m *memState) SubBalance(a common.Address, v *big.Int) error {
	b := m.GetBalance(a)
	if b.Cmp(v) < 0 {
		return errors.New("insufficient balance")
	}
	m.balances[a] = b.Sub(b, v)
	return nil
}

func (m *memState) GetNonce(common.Address) uint64  { return 0 }
func (m *memState) SetNonce(common.Address, uint64) {}
func (m *memState) GetCode(common.Address) []byte   { return nil }
func (m *memState) Failed() bool                    { return false }
func (m *memState) Snapshot() int                   { panic("unsupported") }
func (m *memState) RevertToSnapshot(int)            { panic("unsupported") }

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
func (v stateDBView) Failed() bool                                      { return false }
func (v stateDBView) Snapshot() int                                     { panic("unsupported") }
func (v stateDBView) RevertToSnapshot(int)                              { panic("unsupported") }

var (
	val1  = common.HexToAddress("0x0000000000000000000000000000000000000a01")
	val2  = common.HexToAddress("0x0000000000000000000000000000000000000a02")
	alice = common.HexToAddress("0x00000000000000000000000000000000000a11ce")
	bob   = common.HexToAddress("0x0000000000000000000000000000000000000b0b")
)

// ether returns n whole tokens in wei, or n scaled by 1e18.
func ether(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e18))
}
