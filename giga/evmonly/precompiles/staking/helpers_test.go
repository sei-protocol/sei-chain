package staking

import (
	"errors"
	"maps"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// memState is a map-backed precompiles.State and LogSink.
type memState struct {
	balances  map[common.Address]*big.Int
	storage   map[common.Address]map[common.Hash]common.Hash
	logs      []*ethtypes.Log
	snapshots []memSnapshot
}

// memSnapshot is a copy of memState's balances, storage and log count.
type memSnapshot struct {
	balances map[common.Address]*big.Int
	storage  map[common.Address]map[common.Hash]common.Hash
	logs     int
}

var (
	_ precompiles.State   = (*memState)(nil)
	_ precompiles.LogSink = (*memState)(nil)
)

func newMemState() *memState {
	return &memState{
		balances: map[common.Address]*big.Int{},
		storage:  map[common.Address]map[common.Hash]common.Hash{},
	}
}

func (m *memState) Snapshot() int {
	snap := memSnapshot{balances: map[common.Address]*big.Int{}, storage: map[common.Address]map[common.Hash]common.Hash{}, logs: len(m.logs)}
	for a, b := range m.balances {
		snap.balances[a] = new(big.Int).Set(b)
	}
	for a, slots := range m.storage {
		snap.storage[a] = maps.Clone(slots)
	}
	m.snapshots = append(m.snapshots, snap)
	return len(m.snapshots) - 1
}

func (m *memState) RevertToSnapshot(id int) {
	snap := m.snapshots[id]
	m.balances, m.storage, m.logs = snap.balances, snap.storage, m.logs[:snap.logs]
	m.snapshots = m.snapshots[:id]
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
func (m *memState) AddLog(l *ethtypes.Log)          { m.logs = append(m.logs, l) }

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

const (
	second    = uint64(time.Second)
	startTime = 1_700_000_000 * second
)

var (
	val1  = common.HexToAddress("0x0000000000000000000000000000000000000a01")
	val2  = common.HexToAddress("0x0000000000000000000000000000000000000a02")
	val3  = common.HexToAddress("0x0000000000000000000000000000000000000a03")
	alice = common.HexToAddress("0x00000000000000000000000000000000000a11ce")
	bob   = common.HexToAddress("0x0000000000000000000000000000000000000b0b")
)

// sei returns n SEI in wei.
func sei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), powerReduction)
}

// must returns v, panicking on err, for test reads that cannot fail.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
