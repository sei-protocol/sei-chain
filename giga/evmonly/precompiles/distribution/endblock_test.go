package distribution

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// distribute runs distribution's EndBlock at block and returns its failures.
func (h *harness) distribute(block precompiles.BlockContext) []error {
	failures, err := Precompile{}.EndBlock(block, h.state)
	require.NoError(h.t, err)
	return failures
}

// closing returns the current block as the last of the epoch in progress.
func (h *harness) closing() precompiles.BlockContext {
	block := h.block()
	block.ClosingEpoch = utils.Some(h.epoch)
	return block
}

func TestEndBlockWaitsForParams(t *testing.T) {
	state := newMemState()
	failures, err := Precompile{}.EndBlock(precompiles.BlockContext{ClosingEpoch: utils.Some[uint64](0)}, state)
	require.NoError(t, err)
	require.Empty(t, failures)
	require.Empty(t, state.storage[Address], "nothing is written")
}

func TestFirstEndBlockStartsSettlementAtItsEpoch(t *testing.T) {
	h := newHarness(t)
	h.epoch = 3
	require.Empty(t, h.distribute(h.block()))
	require.Equal(t, dao, h.store().epochDAO())
	require.Equal(t, uint64(3), h.store().nextSettlementEpoch())
}

func TestBaseFeesGoToTheEpochsDAO(t *testing.T) {
	h := newHarness(t)
	h.state.AddBalance(FeeCollector, big.NewInt(1000))
	block := h.block()
	block.BaseFee, block.GasUsed = big.NewInt(7), 100
	require.Empty(t, h.distribute(block))
	require.Equal(t, big.NewInt(700), h.balance(dao))
	require.Equal(t, big.NewInt(300), h.balance(FeeCollector))

	failures := h.distribute(block)
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], errBalanceShort)
	require.Equal(t, big.NewInt(300), h.balance(FeeCollector), "nothing moves when the fee collector is short")
}

// TestSettlementPaysTheCommitteeAndTheDAO settles 11 SEI and a wei of tips and
// a 10 SEI mint over two validators of equal power.
func TestSettlementPaysTheCommitteeAndTheDAO(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, WriteReleaseSchedule(h.state, []ReleasePeriod{{StartTime: h.time, EndTime: h.time + 100*second, Amount: ether(100)}}, h.time))
	require.Empty(t, h.distribute(h.block()))
	tips := new(big.Int).Add(ether(11), big.NewInt(1))
	h.state.AddBalance(FeeCollector, tips)
	h.time += 10 * second

	require.Empty(t, h.distribute(h.closing()))
	daoTips := new(big.Int).Div(ether(11), big.NewInt(2))
	share := new(big.Int).Div(new(big.Int).Mul(ether(31), big.NewInt(1)), big.NewInt(4))
	require.Equal(t, new(big.Int).Add(daoTips, big.NewInt(1)), h.balance(dao), "half the tips, and the wei no share took")
	require.Equal(t, new(big.Int).Mul(share, big.NewInt(2)), h.balance(DistributionEscrow))
	require.Zero(t, h.balance(FeeCollector).Sign(), "the fee collector ends empty")
	require.Equal(t, share, h.owed(val1, val1), "half of 5.5 SEI of tips and the 10 SEI mint")
	require.Equal(t, share, h.owed(val2, val2))
	require.Equal(t, uint64(1), h.store().nextSettlementEpoch())
	h.requireBacked(val1, val2)

	h.state.AddBalance(FeeCollector, ether(1))
	require.Empty(t, h.distribute(h.closing()))
	require.Equal(t, ether(1), h.balance(FeeCollector), "an epoch is settled once")
}

func TestDAOChangeTakesEffectTheNextEpoch(t *testing.T) {
	h := newHarness(t)
	require.Empty(t, h.distribute(h.block()))
	other := common.HexToAddress("0x0b0e")
	require.NoError(t, chainparams.Apply(h.state, []chainparams.Change{{Subspace: "distribution", Key: "daoaddress", Value: common.BytesToHash(other[:]).Bytes()}}))
	h.state.AddBalance(FeeCollector, big.NewInt(2))

	require.Empty(t, h.distribute(h.closing()))
	require.Equal(t, big.NewInt(2), h.balance(dao), "the epoch keeps the DAO it started with")
	require.Equal(t, other, h.store().epochDAO())

	h.epoch++
	h.state.AddBalance(FeeCollector, big.NewInt(5))
	block := h.block()
	block.BaseFee, block.GasUsed = big.NewInt(1), 5
	require.Empty(t, h.distribute(block))
	require.Equal(t, big.NewInt(5), h.balance(other))
}

func TestFailedSettlementWritesNothingAndRollsOver(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, WriteReleaseSchedule(h.state, []ReleasePeriod{{StartTime: h.time, EndTime: h.time + 100*second, Amount: ether(100)}}, h.time))
	require.Empty(t, h.distribute(h.block()))
	h.state.AddBalance(FeeCollector, ether(2))
	h.time += 10 * second

	// A bonded set longer than any MaxValidators allows does not add up.
	lengthSlot := common.Hash{}
	stored := h.state.GetState(staking.Address, lengthSlot)
	h.state.SetState(staking.Address, lengthSlot, common.BigToHash(big.NewInt(1000)))
	failures := h.distribute(h.closing())
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], precompiles.ErrDegraded)
	require.Equal(t, ether(2), h.balance(FeeCollector))
	require.Zero(t, h.balance(DistributionEscrow).Sign())
	require.Equal(t, uint64(0), h.store().nextSettlementEpoch())
	require.Zero(t, h.store().mintState().periodEnd, "the mint is not taken")

	h.state.SetState(staking.Address, lengthSlot, stored)
	h.epoch++
	h.time += 10 * second
	require.Empty(t, h.distribute(h.closing()))
	require.Zero(t, h.balance(FeeCollector).Sign(), "the next settlement pays both epochs")
	require.Equal(t, uint64(2), h.store().nextSettlementEpoch())
	require.Equal(t, ether(22), new(big.Int).Add(h.balance(dao), h.balance(DistributionEscrow)),
		"both epochs' tips and 20 seconds of the mint")
	h.requireBacked(val1, val2)
}

func TestUncreditedMembersShareGoesToTheDAO(t *testing.T) {
	h := newHarness(t)
	require.Empty(t, h.distribute(h.block()))
	h.store().deleteCurrentRewards(val2)
	h.state.AddBalance(FeeCollector, ether(4))

	failures := h.distribute(h.closing())
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], errNoValidatorRecords)
	require.Equal(t, ether(1), h.balance(DistributionEscrow), "val1's half of the pool")
	require.Equal(t, ether(3), h.balance(dao), "half the tips and val2's share")
}
