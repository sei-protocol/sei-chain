package distribution

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

func TestRecordsRoundTripAndDelete(t *testing.T) {
	state := newMemState()
	s := newStore(state)

	s.setCommunityPool(big.NewInt(1))
	s.setEpochDAO(val2)
	s.setNextSettlementEpoch(10)
	s.setOutstandingRewards(val1, big.NewInt(2))
	s.setStartingInfo(val1, alice, startingInfo{previousPeriod: 3, height: 4, stake: ether(5)})
	s.setHistoricalRewards(val1, 3, historicalRewards{cumulativeRewardRatio: ether(6), referenceCount: 7})
	s.setCurrentRewards(val1, currentRewards{rewards: big.NewInt(8), period: 4})
	s.setAccumulatedCommission(val1, big.NewInt(9))

	require.Equal(t, big.NewInt(1), s.communityPool())
	require.Equal(t, val2, s.epochDAO())
	require.Equal(t, uint64(10), s.nextSettlementEpoch(), "the epoch shares the DAO's slot and keeps it")
	require.Equal(t, big.NewInt(2), s.outstandingRewards(val1))
	require.Equal(t, startingInfo{previousPeriod: 3, height: 4, stake: ether(5)}, s.startingInfo(val1, alice))
	require.Equal(t, historicalRewards{cumulativeRewardRatio: ether(6), referenceCount: 7}, s.historicalRewards(val1, 3))
	require.Equal(t, currentRewards{rewards: big.NewInt(8), period: 4}, s.currentRewards(val1))
	require.Equal(t, big.NewInt(9), s.accumulatedCommission(val1))

	other := s.startingInfo(val2, alice)
	require.Zero(t, other.stake.Sign(), "another validator's record is separate")
	require.Zero(t, other.previousPeriod)
	require.Zero(t, s.historicalRewards(val1, 4).cumulativeRewardRatio.Sign(), "another period's record is separate")

	s.setCommunityPool(new(big.Int))
	s.setEpochDAO(common.Address{})
	s.setNextSettlementEpoch(0)
	s.deleteOutstandingRewards(val1)
	s.deleteStartingInfo(val1, alice)
	s.deleteHistoricalRewards(val1, 3)
	s.deleteCurrentRewards(val1)
	s.deleteAccumulatedCommission(val1)
	require.Empty(t, state.storage[Address], "deleting every record leaves no slot behind")
}

func TestWithdrawAddressDefaultsToTheDelegator(t *testing.T) {
	state := newMemState()
	s := newStore(state)
	require.Equal(t, alice, s.withdrawAddress(alice))
	s.setWithdrawAddress(alice, bob)
	require.Equal(t, bob, s.withdrawAddress(alice))
	s.setWithdrawAddress(alice, alice)
	require.Equal(t, alice, s.withdrawAddress(alice))
	require.Empty(t, state.storage[Address], "setting the delegator itself clears the record")
}

func TestSlashEventsAppendInOrderAndDelete(t *testing.T) {
	state := newMemState()
	s := newStore(state)
	events := []slashEvent{
		{height: 10, validatorPeriod: 2, fraction: big.NewInt(1)},
		{height: 20, validatorPeriod: 3, fraction: big.NewInt(2)},
	}
	for _, e := range events {
		require.NoError(t, s.appendSlashEvent(val1, e))
	}
	n, err := s.slashEventCount(val1)
	require.NoError(t, err)
	require.Equal(t, uint64(len(events)), n)
	for i, e := range events {
		require.Equal(t, e, s.slashEvent(val1, uint64(i)))
	}
	n, err = s.slashEventCount(val2)
	require.NoError(t, err)
	require.Zero(t, n, "another validator's list is separate")

	require.NoError(t, s.deleteSlashEvents(val1))
	require.Empty(t, state.storage[Address])
}

func TestSlashEventsRejectAnImpossibleLength(t *testing.T) {
	s := newStore(newMemState())
	s.SetUint256(slashEventsBase(val1), new(big.Int).Lsh(big.NewInt(1), 64))
	_, err := s.slashEventCount(val1)
	require.ErrorIs(t, err, errInvalidStoredSlashEvents)
	require.ErrorIs(t, s.appendSlashEvent(val1, slashEvent{fraction: big.NewInt(1)}), errInvalidStoredSlashEvents)
	require.ErrorIs(t, s.deleteSlashEvents(val1), errInvalidStoredSlashEvents)
}

func TestParamsReadTheParamsPrecompile(t *testing.T) {
	state := newMemState()
	want := chainparams.Distribution{
		WithdrawAddrEnabled: true,
		DAOAddress:          common.HexToAddress("0xda0"),
	}
	require.NoError(t, chainparams.WriteDistribution(state, want))
	require.Equal(t, want, newStore(state).params())
}

func TestEscrowAddressesArePinned(t *testing.T) {
	require.Equal(t, common.HexToAddress("0xc6098975f3d5aE7CA4361dFfe7dc327093774e94"), DistributionEscrow)
	require.Equal(t, common.HexToAddress("0x27F7B8B8B5A4e71E8E9aA671f4e4031E3773303F"), FeeCollector)
}
