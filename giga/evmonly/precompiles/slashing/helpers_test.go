package slashing

import (
	"errors"
	"maps"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
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
	alice = common.HexToAddress("0x00000000000000000000000000000000000a11ce")
)

// sei returns n SEI in wei.
func sei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000))
}

type harness struct {
	t      *testing.T
	state  *memState
	time   uint64
	number uint64
}

// newHarness starts staking with val1 and val2 self-delegating 10 SEI each,
// at a minimum of 5 SEI, then runs the first EndBlock, which bonds both.
func newHarness(t *testing.T) *harness {
	h := &harness{t: t, state: newMemState(), time: startTime}
	require.NoError(t, staking.SetParams(h.state, staking.DefaultParams()))
	for _, op := range []common.Address{val1, val2} {
		h.state.AddBalance(op, sei(10))
		require.NoError(t, staking.Precompile{}.AddGenesisValidator(h.block(), h.state, staking.GenesisValidator{
			Operator:          op,
			ConsensusPubkey:   common.BytesToHash(op[:]),
			SelfDelegation:    sei(10),
			MinSelfDelegation: sei(5),
		}))
	}
	h.endBlock()
	return h
}

func (h *harness) block() precompiles.BlockContext {
	return precompiles.BlockContext{Number: h.number, UnixNano: h.time}
}

// endBlock runs staking's EndBlock at the current block, then starts the next
// block one second later.
func (h *harness) endBlock() {
	failures, err := staking.Precompile{}.EndBlock(h.block(), h.state)
	require.NoError(h.t, err)
	require.Empty(h.t, failures)
	h.number++
	h.time += second
}

// bonded returns the operators of staking's bonded set.
func (h *harness) bonded() []common.Address {
	set, err := staking.BondedValidators(h.state)
	require.NoError(h.t, err)
	ops := make([]common.Address, len(set))
	for i, v := range set {
		ops[i] = v.Operator
	}
	return ops
}

type callOpts struct {
	value        *big.Int
	readOnly     bool
	delegateCall bool
}

// stake calls staking's method as caller, sending value.
func (h *harness) stake(caller common.Address, value *big.Int, method string, args ...interface{}) {
	input, err := staking.ABI.Pack(method, args...)
	require.NoError(h.t, err)
	if value != nil {
		h.state.AddBalance(caller, value)
	}
	_, err = h.run(staking.Precompile{}, staking.Address, caller, callOpts{value: value}, input)
	require.NoError(h.t, err)
}

// unjail calls slashing's unjail as caller.
func (h *harness) unjail(caller common.Address, opts callOpts) ([]byte, error) {
	input, err := ABI.Pack(UnjailMethod)
	require.NoError(h.t, err)
	return h.run(Precompile{}, Address, caller, opts, input)
}

// run runs contract at addr as caller. Like the EVM, it credits value to the
// contract before running it, and undoes the call's writes when it fails.
func (h *harness) run(contract precompiles.Contract, addr, caller common.Address, opts callOpts, input []byte) ([]byte, error) {
	snap := h.state.Snapshot()
	if opts.value != nil {
		require.NoError(h.t, h.state.SubBalance(caller, opts.value))
		h.state.AddBalance(addr, opts.value)
	}
	out, err := contract.Run(&precompiles.Context{
		Caller:        caller,
		Address:       addr,
		ApparentValue: opts.value,
		ReadOnly:      opts.readOnly,
		DelegateCall:  opts.delegateCall,
		Block:         h.block(),
		State:         h.state,
		Logs:          h.state,
	}, input)
	if err != nil {
		h.state.RevertToSnapshot(snap)
	}
	return out, err
}
