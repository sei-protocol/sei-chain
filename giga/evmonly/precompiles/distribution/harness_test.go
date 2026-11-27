package distribution

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	second    = uint64(time.Second)
	startTime = 1_700_000_000 * second
)

var (
	carol = common.HexToAddress("0x00000000000000000000000000000000000ca201")
	dao   = common.HexToAddress("0x00000000000000000000000000000000000da0da")
)

// testStakingParams bond up to ten validators and unbond over 100 seconds.
var testStakingParams = chainparams.Staking{
	UnbondingTime:                      100 * second,
	MaxValidators:                      10,
	MaxEntries:                         7,
	MaxVotingPowerRatio:                dec.One(),
	MaxVotingPowerEnforcementThreshold: new(big.Int),
}

var testParams = chainparams.Distribution{WithdrawAddrEnabled: true, DAOAddress: dao}

// harness runs staking, with distribution's hooks, and distribution over one
// state, a block at a time.
type harness struct {
	t       *testing.T
	state   *memState
	staking staking.Precompile
	number  uint64
	time    uint64
	// epoch is the consensus epoch the current block is in.
	epoch uint64
}

// newHarness writes the params, adds val1 and val2 self-delegating 100 SEI
// each, and runs the first EndBlock, which bonds them.
func newHarness(t *testing.T) *harness {
	h := &harness{t: t, state: newMemState(), staking: staking.Precompile{Hooks: Hooks{}}, number: 1, time: startTime}
	require.NoError(t, staking.SetParams(h.state, testStakingParams))
	require.NoError(t, chainparams.WriteDistribution(h.state, testParams))
	for _, op := range []common.Address{val1, val2} {
		h.state.AddBalance(op, ether(100))
		require.NoError(t, h.staking.AddGenesisValidator(h.block(), h.state, staking.GenesisValidator{
			Operator:          op,
			ConsensusPubkey:   common.BytesToHash(op[:]),
			SelfDelegation:    ether(100),
			MinSelfDelegation: ether(1),
		}))
	}
	h.endBlock()
	return h
}

func (h *harness) store() store {
	return newStore(h.state)
}

func (h *harness) block() precompiles.BlockContext {
	return precompiles.BlockContext{Number: h.number, UnixNano: h.time, GasLimit: 100_000_000, Epoch: utils.Some(h.epoch)}
}

// endBlock runs staking's EndBlock at the current block, which closes no
// epoch, and starts the next block.
func (h *harness) endBlock() {
	h.endBlockWith(h.block())
}

// endEpoch ends the current block as the last of the epoch in progress.
func (h *harness) endEpoch() {
	block := h.block()
	block.ClosingEpoch = utils.Some(h.epoch)
	h.endBlockWith(block)
	h.epoch++
}

// applyStaged ends the epochs it takes for the operations staged in the
// current one to take effect.
func (h *harness) applyStaged() {
	h.endEpoch()
	h.endEpoch()
}

func (h *harness) endBlockWith(block precompiles.BlockContext) {
	failures, err := h.staking.EndBlock(block, h.state)
	require.NoError(h.t, err)
	require.Empty(h.t, failures)
	h.number++
	h.time += second
}

// settle credits amount to val's validator, as a settlement does, at val's
// commission rate.
func (h *harness) settle(val common.Address, amount *big.Int) {
	v, ok := staking.ReadValidatorStake(h.state, val)
	require.True(h.t, ok)
	h.settleAt(val, amount, v.CommissionRate)
}

// settleAt credits amount to val's validator at commission rate.
func (h *harness) settleAt(val common.Address, amount, rate *big.Int) {
	h.state.AddBalance(DistributionEscrow, amount)
	require.NoError(h.t, h.store().allocate(val, amount, rate))
}

// meter is a GasMeter with gas to spare.
type meter struct{}

func (meter) UseGas(uint64) bool { return true }

// stake runs staking method as caller, sending value, and requires it to
// succeed.
func (h *harness) stake(caller common.Address, value *big.Int, method string, args ...interface{}) {
	input, err := staking.ABI.Pack(method, args...)
	require.NoError(h.t, err)
	if value != nil {
		h.state.AddBalance(caller, value)
		require.NoError(h.t, h.state.SubBalance(caller, value))
		h.state.AddBalance(staking.Address, value)
	}
	_, err = h.staking.Run(&precompiles.Context{
		Caller:        caller,
		Address:       staking.Address,
		ApparentValue: value,
		Gas:           meter{},
		Block:         h.block(),
		State:         h.state,
		Logs:          h.state,
	}, input)
	require.NoError(h.t, err)
}

// delegate stages delegating amount from delegator to val and applies it.
func (h *harness) delegate(delegator, val common.Address, amount *big.Int) {
	h.stake(delegator, amount, staking.DelegateMethod, val)
	h.applyStaged()
}

// undelegate stages undelegating amount from delegator's delegation to val
// and applies it.
func (h *harness) undelegate(delegator, val common.Address, amount *big.Int) {
	h.stake(delegator, nil, staking.UndelegateMethod, val, amount)
	h.applyStaged()
}

type callOpts struct {
	value        *big.Int
	readOnly     bool
	delegateCall bool
}

// call runs distribution method as caller and returns its results. Like the
// EVM, it undoes the call's writes when it fails.
func (h *harness) call(caller common.Address, method string, args ...interface{}) ([]interface{}, error) {
	input, err := ABI.Pack(method, args...)
	require.NoError(h.t, err)
	return h.run(caller, callOpts{readOnly: ABI.Methods[method].IsConstant()}, input)
}

// mustCall runs a distribution transaction as caller and requires it to
// succeed.
func (h *harness) mustCall(caller common.Address, method string, args ...interface{}) {
	out, err := h.call(caller, method, args...)
	require.NoError(h.t, err)
	require.Equal(h.t, []interface{}{true}, out)
}

// query runs a distribution view method and requires it to succeed.
func (h *harness) query(method string, args ...interface{}) []interface{} {
	out, err := h.call(common.Address{}, method, args...)
	require.NoError(h.t, err)
	return out
}

func (h *harness) run(caller common.Address, opts callOpts, input []byte) ([]interface{}, error) {
	snap := h.state.Snapshot()
	out, err := Precompile{}.Run(&precompiles.Context{
		Caller:        caller,
		Address:       Address,
		ApparentValue: opts.value,
		ReadOnly:      opts.readOnly,
		DelegateCall:  opts.delegateCall,
		GasRemaining:  math.MaxUint64,
		Block:         h.block(),
		State:         h.state,
		Logs:          h.state,
	}, input)
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
	m, err := ABI.MethodById(input[:4])
	require.NoError(h.t, err)
	values, err := m.Outputs.Unpack(out)
	require.NoError(h.t, err)
	return values, nil
}

// owed returns the rewards delegator's delegation to val is owed.
func (h *harness) owed(delegator, val common.Address) *big.Int {
	return h.query(DelegationRewardsMethod, delegator, val)[0].(*big.Int)
}

// requireBacked requires the escrow to hold exactly every validator's
// outstanding rewards and the community pool.
func (h *harness) requireBacked(vals ...common.Address) {
	held := new(big.Int).Set(h.store().communityPool())
	for _, val := range vals {
		held.Add(held, h.store().outstandingRewards(val))
	}
	require.Equal(h.t, held, h.state.GetBalance(DistributionEscrow), "the escrow backs every outstanding reward")
}

// balance returns a's balance.
func (h *harness) balance(a common.Address) *big.Int {
	return h.state.GetBalance(a)
}

// pct returns n percent as a 1e18-scaled fraction.
func pct(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e16))
}
