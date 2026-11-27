package distribution

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

func TestGenesisValidatorStartsItsRecords(t *testing.T) {
	h := newHarness(t)
	s := h.store()
	requireCurrent(t, s, val1, 2, new(big.Int), "the self-delegation closed period 1")
	requireHistorical(t, s, val1, 0, new(big.Int), 0, "period 0 lost its last reference when period 1 closed")
	requireHistorical(t, s, val1, 1, new(big.Int), 2, "period 1 is the open period's previous and the self-delegation's start")
	require.Equal(t, startingInfo{previousPeriod: 1, height: 1, stake: ether(100)}, s.startingInfo(val1, val1))
	require.Zero(t, s.outstandingRewards(val1).Sign())
}

// TestRewardsFollowStake is the design's example: a validator's settlements
// are shared by stake, and a delegation joining later earns only from the
// settlements after it.
func TestRewardsFollowStake(t *testing.T) {
	h := newHarness(t)
	h.delegate(alice, val1, ether(300))
	h.settle(val1, ether(40))
	require.Equal(t, ether(10), h.owed(val1, val1))
	require.Equal(t, ether(30), h.owed(alice, val1))

	h.delegate(bob, val1, ether(400))
	require.Zero(t, h.owed(bob, val1).Sign(), "bob joins after the first settlement")
	require.Equal(t, ether(30), h.owed(alice, val1))

	h.settle(val1, ether(80))
	require.Equal(t, ether(20), h.owed(val1, val1))
	require.Equal(t, ether(60), h.owed(alice, val1))
	require.Equal(t, ether(40), h.owed(bob, val1))
	require.Equal(t, []interface{}{Rewards{
		Rewards: []DelegationReward{{Validator: val1, Amount: ether(60)}},
		Total:   ether(60),
	}}, toRewards(h.query(RewardsMethod, alice)))
	h.requireBacked(val1, val2)
}

func TestStakeChangePaysWhatTheOldStakeEarned(t *testing.T) {
	h := newHarness(t)
	h.delegate(alice, val1, ether(300))
	h.settle(val1, ether(40))

	h.stake(alice, nil, staking.UndelegateMethod, val1, ether(100))
	h.endEpoch()
	h.settle(val1, ether(40))
	require.Zero(t, h.balance(alice).Sign(), "nothing is paid before the undelegation takes effect")
	h.endEpoch()
	require.Equal(t, ether(60), h.balance(alice), "both settlements before the change are paid at the old stake")
	require.Zero(t, h.owed(alice, val1).Sign())

	h.settle(val1, ether(30))
	require.Equal(t, ether(20), h.owed(alice, val1), "200 of 300 staked")
	require.Equal(t, ether(30), h.owed(val1, val1), "10 and 10 from the first two settlements, 10 of this one")
	h.requireBacked(val1, val2)
}

func TestDelegationStartedInTheSettlingBlockIsOwedItsShare(t *testing.T) {
	h := newHarness(t)
	h.delegate(alice, val1, ether(300))
	h.mustCall(alice, WithdrawDelegationRewardsMethod, val1)
	require.Equal(t, h.number, h.store().startingInfo(val1, alice).height)
	h.settle(val1, ether(40))
	require.Equal(t, ether(30), h.owed(alice, val1), "the settlement comes after the withdrawal that restarted the delegation")
	h.mustCall(alice, WithdrawDelegationRewardsMethod, val1)
	require.Equal(t, ether(30), h.balance(alice))
}

func TestZeroTokenValidatorSendsItsOpenPeriodToTheDAO(t *testing.T) {
	h := newHarness(t)
	h.settle(val1, ether(40))
	s := h.store()
	before := s.historicalRewards(val1, 1).cumulativeRewardRatio
	period, err := s.incrementValidatorPeriod(val1, new(big.Int))
	require.NoError(t, err)
	require.Equal(t, uint64(2), period)
	require.Equal(t, ether(40), h.balance(dao))
	require.Zero(t, s.outstandingRewards(val1).Sign())
	require.Equal(t, before, s.historicalRewards(val1, 2).cumulativeRewardRatio, "no tokens, no ratio")
	h.requireBacked(val1, val2)
}

func TestAllocateSplitsCommission(t *testing.T) {
	h := newHarness(t)
	h.settleAt(val1, big.NewInt(101), pct(50))
	s := h.store()
	require.Equal(t, big.NewInt(50), s.accumulatedCommission(val1), "commission is truncated")
	require.Equal(t, big.NewInt(51), s.currentRewards(val1).rewards)
	require.Equal(t, big.NewInt(101), s.outstandingRewards(val1))
	require.ErrorIs(t, s.allocate(val1, big.NewInt(1), pct(101)), errInvalidFraction)
	require.ErrorIs(t, s.allocate(alice, big.NewInt(1), pct(1)), errNoValidatorRecords)
}

// TestSlashEventLowersStake checks a slash event V2 recorded: the periods
// after it pay the delegation on its slashed stake.
func TestSlashEventLowersStake(t *testing.T) {
	h := newHarness(t)
	h.delegate(alice, val1, ether(300))
	h.settle(val1, ether(40))

	s := h.store()
	v, _ := staking.ReadValidatorStake(h.state, val1)
	period, err := s.incrementValidatorPeriod(val1, v.Tokens)
	require.NoError(t, err)
	require.NoError(t, s.incrementReferenceCount(val1, period))
	require.NoError(t, s.appendSlashEvent(val1, slashEvent{height: h.number, validatorPeriod: period, fraction: pct(50)}))
	h.endBlock()

	h.settle(val1, ether(40))
	require.Equal(t, ether(45), h.owed(alice, val1), "30 before the slash, and 15 on half the stake after")
	h.mustCall(alice, WithdrawDelegationRewardsMethod, val1)
	require.Equal(t, ether(45), h.balance(alice))
	h.requireBacked(val1, val2)
}

func TestRemovedValidatorPaysCommissionAndRetiresTheRest(t *testing.T) {
	h := newHarness(t)
	h.settleAt(val2, new(big.Int).Add(ether(3), big.NewInt(1)), pct(50))
	half := new(big.Int).Div(ether(3), big.NewInt(2))
	require.Equal(t, half, h.store().accumulatedCommission(val2))

	h.undelegate(val2, val2, ether(100))
	require.Equal(t, half, h.balance(val2), "the self-delegation's rewards, its ratio truncated")
	lastClosed := h.store().currentRewards(val2).period - 1
	h.time += 200 * second
	h.endEpoch()

	_, ok := staking.ReadValidatorStake(h.state, val2)
	require.False(t, ok, "val2 finished unbonding with nothing staked, so staking removed it")
	s := h.store()
	requireCurrent(t, s, val2, 0, new(big.Int), "its records are deleted")
	requireHistorical(t, s, val2, lastClosed, new(big.Int), 0, "its records are deleted")
	require.Zero(t, s.outstandingRewards(val2).Sign())
	require.Zero(t, s.accumulatedCommission(val2).Sign())
	want := new(big.Int).Add(ether(100), ether(3))
	require.Equal(t, want, h.balance(val2), "its unbonded stake, its rewards and its commission")
	require.Equal(t, big.NewInt(1), h.balance(dao), "the wei the ratio truncated goes to the DAO")
	h.requireBacked(val1, val2)
}

func TestRetireKeepsFundsWithoutADAO(t *testing.T) {
	state := newMemState()
	s := newStore(state)
	state.AddBalance(DistributionEscrow, big.NewInt(7))
	require.NoError(t, s.retire(big.NewInt(7)))
	require.Equal(t, big.NewInt(7), s.communityPool())
	require.Equal(t, big.NewInt(7), state.GetBalance(DistributionEscrow))

	require.NoError(t, chainparams.WriteDistribution(state, testParams))
	require.NoError(t, s.retire(big.NewInt(3)))
	require.Equal(t, big.NewInt(3), state.GetBalance(dao), "the params' DAO, before an epoch fixes one")
	other := common.HexToAddress("0x0b0e")
	s.setEpochDAO(other)
	require.NoError(t, s.retire(big.NewInt(4)))
	require.Equal(t, big.NewInt(4), state.GetBalance(other), "the epoch's DAO")
	require.ErrorIs(t, s.retire(big.NewInt(1)), errEscrowShort)
}

func TestReferenceCountsStayInRange(t *testing.T) {
	s := newStore(newMemState())
	require.ErrorIs(t, s.decrementReferenceCount(val1, 0), errReferenceCount)
	for range 3 {
		require.NoError(t, s.incrementReferenceCount(val1, 0))
	}
	require.ErrorIs(t, s.incrementReferenceCount(val1, 0), errReferenceCount)
	for range 3 {
		require.NoError(t, s.decrementReferenceCount(val1, 0))
	}
	requireHistorical(t, s, val1, 0, new(big.Int), 0, "the last reference deletes it")
}

// toRewards converts rewards' unpacked result to Rewards.
func toRewards(out []interface{}) []interface{} {
	r := out[0].(struct {
		Rewards []struct {
			Validator common.Address `json:"validator"`
			Amount    *big.Int       `json:"amount"`
		} `json:"rewards"`
		Total *big.Int `json:"total"`
	})
	got := Rewards{Rewards: []DelegationReward{}, Total: r.Total}
	for _, e := range r.Rewards {
		got.Rewards = append(got.Rewards, DelegationReward{Validator: e.Validator, Amount: e.Amount})
	}
	return []interface{}{got}
}

// requireCurrent requires val's current rewards to be rewards in period.
func requireCurrent(t *testing.T, s store, val common.Address, period uint64, rewards *big.Int, msg string) {
	t.Helper()
	cur := s.currentRewards(val)
	require.Equal(t, period, cur.period, msg)
	require.Zero(t, rewards.Cmp(cur.rewards), msg)
}

// requireHistorical requires val's historical rewards for period to be ratio
// with refs references.
func requireHistorical(t *testing.T, s store, val common.Address, period uint64, ratio *big.Int, refs uint32, msg string) {
	t.Helper()
	h := s.historicalRewards(val, period)
	require.Equal(t, refs, h.referenceCount, msg)
	require.Zero(t, ratio.Cmp(h.cumulativeRewardRatio), msg)
}
