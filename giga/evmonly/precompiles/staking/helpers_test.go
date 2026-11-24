package staking

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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

// testParams bond two validators, unbond over 100 seconds, and leave the
// voting power ratio unenforced.
var testParams = chainparams.Staking{
	UnbondingTime:                      100 * second,
	MaxValidators:                      2,
	MaxEntries:                         7,
	MaxVotingPowerRatio:                dec.One(),
	MaxVotingPowerEnforcementThreshold: new(big.Int),
}

// testGasLimit is the gas limit of the harness's blocks.
const testGasLimit = 100_000_000

type harness struct {
	t       *testing.T
	staking Precompile
	state   *memState
	time    uint64
	number  uint64
	// callGas is the gas each call has to pay through its context's meter.
	callGas uint64
	// surcharge is the gas the last call paid through its context's meter.
	surcharge uint64
	// failures are the last EndBlock's failures.
	failures []error
}

// meter is a GasMeter that pays from left and adds what it pays to used.
type meter struct{ left, used uint64 }

func (m *meter) UseGas(gas uint64) bool {
	if gas > m.left {
		return false
	}
	m.left -= gas
	m.used += gas
	return true
}

// newHarness starts staking as newGenesisHarness does, then runs the first
// EndBlock, which bonds val1 and val2.
func newHarness(t *testing.T, params chainparams.Staking) *harness {
	h := newGenesisHarness(t, params)
	h.endBlock()
	require.Empty(t, h.failures)
	return h
}

// newGenesisHarness writes params and adds val1, val2 and val3 self-delegating
// 30, 20 and 10 SEI, before any EndBlock.
func newGenesisHarness(t *testing.T, params chainparams.Staking) *harness {
	h := &harness{t: t, state: newMemState(), time: startTime, callGas: math.MaxUint64}
	require.NoError(t, SetParams(h.state, params))
	for i, op := range []common.Address{val1, val2, val3} {
		self := sei(int64(30 - 10*i))
		h.state.AddBalance(op, self)
		require.NoError(t, h.staking.AddGenesisValidator(h.block(), h.state, GenesisValidator{
			Operator:          op,
			ConsensusPubkey:   common.BytesToHash(op[:]),
			SelfDelegation:    self,
			MinSelfDelegation: sei(1),
		}))
	}
	return h
}

func (h *harness) block() precompiles.BlockContext {
	return precompiles.BlockContext{Number: h.number, UnixNano: h.time, GasLimit: testGasLimit}
}

// endBlock runs EndBlock at the current block, which closes no epoch, then
// starts the next block one second later.
func (h *harness) endBlock() {
	h.endBlockWith(h.block())
}

// endEpoch ends the current block as the last block of epoch.
func (h *harness) endEpoch(epoch uint64) {
	block := h.block()
	block.ClosingEpoch = utils.Some(epoch)
	h.endBlockWith(block)
}

// endCurrentEpoch ends the current block as the last block of the epoch in
// progress.
func (h *harness) endCurrentEpoch() {
	h.endEpoch(h.store().currentEpoch())
}

func (h *harness) endBlockWith(block precompiles.BlockContext) {
	failures, err := h.staking.EndBlock(block, h.state)
	require.NoError(h.t, err)
	h.failures = failures
	h.number++
	h.time += second
}

// govern changes staking's param key to value in the params precompile, as a
// passed proposal does.
func (h *harness) govern(key string, value *big.Int) {
	require.NoError(h.t, chainparams.Apply(h.state, []chainparams.Change{{Subspace: "staking", Key: key, Value: common.BigToHash(value).Bytes()}}))
}

// advance ends blocks until it has ended one at least d after the current
// block, the last of them closing the epoch in progress, keeping every block's
// failures.
func (h *harness) advance(d uint64) {
	var failures []error
	for end := h.time + d; h.time < end; {
		h.endBlock()
		failures = append(failures, h.failures...)
	}
	h.endCurrentEpoch()
	h.failures = append(failures, h.failures...)
}

type callOpts struct {
	value        *big.Int
	readOnly     bool
	delegateCall bool
}

// call runs method as caller, as run does.
func (h *harness) call(caller common.Address, opts callOpts, method string, args ...interface{}) error {
	input, err := ABI.Pack(method, args...)
	require.NoError(h.t, err)
	return h.run(caller, opts, input)
}

// run runs input as caller, requiring a transaction that succeeds to return
// true.
func (h *harness) run(caller common.Address, opts callOpts, input []byte) error {
	values, err := h.exec(caller, opts, input)
	if err == nil && !ABI.Methods[methodName(input)].IsConstant() {
		require.Equal(h.t, []interface{}{true}, values)
	}
	return err
}

// query runs view method in a static call and returns its results.
func (h *harness) query(method string, args ...interface{}) ([]interface{}, error) {
	input, err := ABI.Pack(method, args...)
	require.NoError(h.t, err)
	return h.exec(common.Address{}, callOpts{readOnly: true}, input)
}

// exec runs input as caller and returns its results. Like the EVM, it credits
// value to the precompile before running it, and undoes the call's writes
// when it fails.
func (h *harness) exec(caller common.Address, opts callOpts, input []byte) ([]interface{}, error) {
	snap := h.state.Snapshot()
	if opts.value != nil {
		require.NoError(h.t, h.state.SubBalance(caller, opts.value))
		h.state.AddBalance(Address, opts.value)
	}
	gas := &meter{left: h.callGas}
	out, err := h.staking.Run(h.context(caller, opts, gas), input)
	h.surcharge = gas.used
	if err != nil {
		h.state.RevertToSnapshot(snap)
	}
	if errors.Is(err, vm.ErrExecutionReverted) {
		reason, unpackErr := abi.UnpackRevert(out)
		require.NoError(h.t, unpackErr)
		return nil, fmt.Errorf("%w: %s", err, reason)
	}
	if err != nil {
		return nil, err
	}
	values, err := ABI.Methods[methodName(input)].Outputs.Unpack(out)
	require.NoError(h.t, err)
	return values, nil
}

// context returns the context of a call by caller at the current block, paying
// through gas.
func (h *harness) context(caller common.Address, opts callOpts, gas precompiles.GasMeter) *precompiles.Context {
	return &precompiles.Context{
		Caller:        caller,
		Address:       Address,
		ApparentValue: opts.value,
		ReadOnly:      opts.readOnly,
		DelegateCall:  opts.delegateCall,
		Gas:           gas,
		Block:         h.block(),
		State:         h.state,
		Logs:          h.state,
	}
}

func methodName(input []byte) string {
	m, err := ABI.MethodById(input[:4])
	if err != nil {
		panic(err)
	}
	return m.Name
}

// fund gives addr amount.
func (h *harness) fund(addr common.Address, amount *big.Int) {
	h.state.AddBalance(addr, amount)
}

// delegate funds delegator with amount and delegates it to op, applying the
// staged delegation at once.
func (h *harness) delegate(delegator, op common.Address, amount *big.Int) error {
	return h.inEffect(h.stageDelegate(delegator, op, amount))
}

// undelegate undelegates amount from delegator's delegation to op, applying the
// staged undelegation at once.
func (h *harness) undelegate(delegator, op common.Address, amount *big.Int) error {
	return h.inEffect(h.stageUndelegate(delegator, op, amount))
}

// stageDelegate funds delegator with amount and stages delegating it to op.
func (h *harness) stageDelegate(delegator, op common.Address, amount *big.Int) error {
	h.fund(delegator, amount)
	return h.call(delegator, callOpts{value: amount}, DelegateMethod, op)
}

func (h *harness) stageUndelegate(delegator, op common.Address, amount *big.Int) error {
	return h.call(delegator, callOpts{}, UndelegateMethod, op, amount)
}

// inEffect returns err, the error of a call that stages operations, or when
// it is nil applies every staged operation at the current block, as the end of
// its epoch would, and returns the failures.
func (h *harness) inEffect(err error) error {
	if err != nil {
		return err
	}
	var failures []error
	s := h.store()
	h.staking.applyStaged(h.block(), s, s.params(), math.MaxUint64, &failures)
	return errors.Join(failures...)
}

// stagedTotals returns each delegation's staged totals, keyed by delegator and
// validator, how many staged delegations are to each validator, and their
// tokens, counted from the staged queue.
func (h *harness) stagedTotals() (map[[2]common.Address]stagedDelegation, map[common.Address]uint64, *big.Int) {
	s, q := h.store(), h.store().stagedQueue()
	totals := map[[2]common.Address]stagedDelegation{}
	inbound := map[common.Address]uint64{}
	tokens := new(big.Int)
	for i := q.head(); i < q.tail(); i++ {
		switch op := s.stagedOperationAt(q.entry(i)); op.kind {
		case stagedDelegate:
			tokens.Add(tokens, op.amount)
			inbound[op.validator]++
		case stagedUndelegate:
			key := [2]common.Address{op.delegator, op.validator}
			t, ok := totals[key]
			if !ok {
				t.sharesOut = new(big.Int)
			}
			t.sharesOut = new(big.Int).Add(t.sharesOut, op.amount)
			t.undelegations++
			totals[key] = t
		}
	}
	return totals, inbound, tokens
}

// formatStaged formats staged's totals for comparison.
func formatStaged(staged stagedDelegation) string {
	shares := "0"
	if staged.sharesOut != nil {
		shares = staged.sharesOut.String()
	}
	return fmt.Sprintf("%s shares out, %d undelegations", shares, staged.undelegations)
}

func (h *harness) store() store {
	return newStore(h.state)
}

func (h *harness) validator(op common.Address) validator {
	v, ok := h.store().validator(op)
	require.True(h.t, ok, "validator %s exists", op)
	return v
}

func (h *harness) shares(delegator, op common.Address) *big.Int {
	shares, _ := h.store().delegation(delegator, op)
	return shares
}

func (h *harness) lastValidators() []validatorPower {
	set, err := h.store().lastValidators()
	require.NoError(h.t, err)
	return set
}

func (h *harness) powerIndex() []common.Address {
	p := h.store().powerIndex()
	out := []common.Address{}
	for i := uint64(0); i < p.len(); i++ {
		out = append(out, p.at(i))
	}
	return out
}

// requireReverts checks that err is a revert whose reason carries want's message.
func requireReverts(t *testing.T, err error, want error, msgAndArgs ...interface{}) {
	t.Helper()
	require.ErrorIs(t, err, vm.ErrExecutionReverted, msgAndArgs...)
	require.ErrorContains(t, err, want.Error(), msgAndArgs...)
}

// requireInvariants checks that each validator's shares are its delegations'
// shares, each delegation's and validator's staged totals are its staged
// operations' and leave it no fewer than zero shares, each escrow holds the
// tokens and balances it should, and the power index holds every unjailed
// validator in order.
// delegators are every account that may delegate.
func (h *harness) requireInvariants(delegators ...common.Address) {
	t, s := h.t, h.store()
	delegators = append(delegators, val1, val2, val3)
	bonded, notBonded := new(big.Int), new(big.Int)
	var unjailed []common.Address
	set := s.validatorSet()
	for i := uint64(0); i < set.Len(); i++ {
		op := solstorage.MemberAddress(set.At(i))
		v := h.validator(op)
		shares := new(big.Int)
		var delegating []common.Address
		for _, d := range delegators {
			shares.Add(shares, h.shares(d, op))
			if _, ok := s.delegation(d, op); ok {
				delegating = append(delegating, d)
			}
		}
		require.Equal(t, v.delegatorShares.String(), shares.String(), "%s's shares are its delegations'", op)
		require.ElementsMatch(t, delegating, addresses(s.delegators(op)), "%s's delegators are the accounts delegating to it", op)
		if v.bonded() {
			bonded.Add(bonded, v.tokens)
		} else {
			notBonded.Add(notBonded, v.tokens)
		}
		if !v.jailed {
			unjailed = append(unjailed, op)
		}
		require.Equal(t, op, s.validatorByPubkey(v.consensusPubkey))
	}
	for _, d := range delegators {
		for _, op := range []common.Address{val1, val2, val3} {
			for _, e := range must(s.unbondingEntries(d, op)) {
				notBonded.Add(notBonded, e.balance)
			}
		}
	}
	require.Equal(t, bonded.String(), h.state.GetBalance(BondedEscrow).String(), "the bonded escrow holds the bonded validators' tokens")
	require.Equal(t, notBonded.String(), h.state.GetBalance(NotBondedEscrow).String(), "the not-bonded escrow holds the other validators' tokens and every unbonding balance")

	totals, inbound, stagedTokens := h.stagedTotals()
	require.Equal(t, stagedTokens.String(), h.state.GetBalance(StagedEscrow).String(), "the staged escrow holds the staged delegations' tokens")
	for _, d := range delegators {
		for _, op := range []common.Address{val1, val2, val3} {
			want, staged := totals[[2]common.Address{d, op}], s.stagedDelegation(d, op)
			require.Equal(t, formatStaged(want), formatStaged(staged), "%s's staged totals for %s", d, op)
			require.LessOrEqual(t, staged.sharesOut.Cmp(h.shares(d, op)), 0, "%s stages no more shares out of %s than it holds", d, op)
		}
	}
	for _, op := range []common.Address{val1, val2, val3} {
		require.Equal(t, inbound[op], s.stagedInbound(op), "the staged operations moving stake to %s", op)
	}

	index := h.powerIndex()
	require.ElementsMatch(t, unjailed, index, "the power index holds every unjailed validator")
	for i := 1; i < len(index); i++ {
		a, b := index[i-1], index[i]
		require.True(t, ranksBefore(s.validatorPower(a), a, s.validatorPower(b), b), "the power index is in rank order: %v", index)
	}
}

// must returns v, panicking on err, for test reads that cannot fail.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// addresses returns the members of an address set, in set order.
func addresses(set solstorage.Set) []common.Address {
	out := make([]common.Address, set.Len())
	for i := range out {
		out[i] = solstorage.MemberAddress(set.At(uint64(i)))
	}
	return out
}
