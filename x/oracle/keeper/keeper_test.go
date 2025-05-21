package keeper

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func TestExchangeRate(t *testing.T) {
	input := CreateTestInput(t)

	cnyExchangeRate := sdk.NewDecWithPrec(839, int64(OracleDecPrecision)).MulInt64(utils.MicroUnit)
	gbpExchangeRate := sdk.NewDecWithPrec(4995, int64(OracleDecPrecision)).MulInt64(utils.MicroUnit)
	krwExchangeRate := sdk.NewDecWithPrec(2838, int64(OracleDecPrecision)).MulInt64(utils.MicroUnit)

	// Set & get rates
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroSeiDenom, cnyExchangeRate)
	rate, lastUpdate, _, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroSeiDenom)
	require.NoError(t, err)
	require.Equal(t, cnyExchangeRate, rate)
	require.Equal(t, sdk.ZeroInt(), lastUpdate)

	input.Ctx = input.Ctx.WithBlockHeight(3)
	ts := time.Now()
	input.Ctx = input.Ctx.WithBlockTime(ts)

	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroEthDenom, gbpExchangeRate)
	rate, lastUpdate, lastUpdateTimestamp, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroEthDenom)
	require.NoError(t, err)
	require.Equal(t, gbpExchangeRate, rate)
	require.Equal(t, sdk.NewInt(3), lastUpdate)
	require.Equal(t, ts.UnixMilli(), lastUpdateTimestamp)

	input.Ctx = input.Ctx.WithBlockHeight(15)
	laterTS := ts.Add(time.Hour)
	input.Ctx = input.Ctx.WithBlockTime(laterTS)

	// verify behavior works with event too
	input.OracleKeeper.SetBaseExchangeRateWithEvent(input.Ctx, utils.MicroAtomDenom, krwExchangeRate)
	rate, lastUpdate, lastUpdateTimestamp, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.NoError(t, err)
	require.Equal(t, krwExchangeRate, rate)
	require.Equal(t, sdk.NewInt(15), lastUpdate)
	require.Equal(t, laterTS.UnixMilli(), lastUpdateTimestamp)
	require.True(t, func() bool {
		expectedEvent := sdk.NewEvent(types.EventTypeExchangeRateUpdate,
			sdk.NewAttribute(types.AttributeKeyDenom, utils.MicroAtomDenom),
			sdk.NewAttribute(types.AttributeKeyExchangeRate, krwExchangeRate.String()),
		)
		events := input.Ctx.EventManager().Events()
		for _, event := range events {

			if event.Type == expectedEvent.Type {
				for i, attr := range event.Attributes {
					if (attr.Index != expectedEvent.Attributes[i].Index) || (string(attr.Key) != string(expectedEvent.Attributes[i].Key)) || (string(attr.Value) != string(expectedEvent.Attributes[i].Value)) {
						return false
					}
				}
				return true
			}
		}
		return false
	}())

	input.OracleKeeper.DeleteBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	_, _, _, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.Error(t, err)

	numExchangeRates := 0
	handler := func(denom string, exchangeRate types.OracleExchangeRate) (stop bool) {
		numExchangeRates = numExchangeRates + 1
		return false
	}
	input.OracleKeeper.IterateBaseExchangeRates(input.Ctx, handler)

	require.Equal(t, 2, numExchangeRates)

	// eth removed
	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroSeiDenom)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)
	// should remove eth
	input.OracleKeeper.RemoveExcessFeeds(input.Ctx)

	numExchangeRates = 0
	input.OracleKeeper.IterateBaseExchangeRates(input.Ctx, handler)
	require.Equal(t, 1, numExchangeRates)

}

func TestIterateSeiExchangeRates(t *testing.T) {
	input := CreateTestInput(t)

	cnyExchangeRate := sdk.NewDecWithPrec(839, int64(OracleDecPrecision)).MulInt64(utils.MicroUnit)
	gbpExchangeRate := sdk.NewDecWithPrec(4995, int64(OracleDecPrecision)).MulInt64(utils.MicroUnit)
	krwExchangeRate := sdk.NewDecWithPrec(2838, int64(OracleDecPrecision)).MulInt64(utils.MicroUnit)

	// Set & get rates
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroSeiDenom, cnyExchangeRate)
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroEthDenom, gbpExchangeRate)
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom, krwExchangeRate)

	input.OracleKeeper.IterateBaseExchangeRates(input.Ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		switch denom {
		case utils.MicroSeiDenom:
			require.Equal(t, cnyExchangeRate, rate.ExchangeRate)
		case utils.MicroEthDenom:
			require.Equal(t, gbpExchangeRate, rate.ExchangeRate)
		case utils.MicroAtomDenom:
			require.Equal(t, krwExchangeRate, rate.ExchangeRate)
		}
		return false
	})
}

func TestRewardPool(t *testing.T) {
	input := CreateTestInput(t)

	fees := sdk.NewCoins(sdk.NewCoin(utils.MicroEthDenom, sdk.NewInt(1000)))
	acc := input.AccountKeeper.GetModuleAccount(input.Ctx, types.ModuleName)
	err := FundAccount(input, acc.GetAddress(), fees)
	if err != nil {
		panic(err) // never occurs
	}

	KFees := input.OracleKeeper.GetRewardPool(input.Ctx, utils.MicroEthDenom)
	require.Equal(t, fees[0], KFees)
}

func TestParams(t *testing.T) {
	input := CreateTestInput(t)

	// Test default params setting
	input.OracleKeeper.SetParams(input.Ctx, types.DefaultParams())
	params := input.OracleKeeper.GetParams(input.Ctx)
	require.NotNil(t, params)

	// Test custom params setting
	votePeriod := uint64(10)
	voteThreshold := sdk.NewDecWithPrec(33, 2)
	oracleRewardBand := sdk.NewDecWithPrec(1, 2)
	slashFraction := sdk.NewDecWithPrec(1, 2)
	slashWindow := uint64(1000)
	minValidPerWindow := sdk.NewDecWithPrec(1, 4)
	whitelist := types.DenomList{
		{Name: utils.MicroEthDenom},
		{Name: utils.MicroAtomDenom},
	}

	// Should really test validateParams, but skipping because obvious
	newParams := types.Params{
		VotePeriod:        votePeriod,
		VoteThreshold:     voteThreshold,
		RewardBand:        oracleRewardBand,
		Whitelist:         whitelist,
		SlashFraction:     slashFraction,
		SlashWindow:       slashWindow,
		MinValidPerWindow: minValidPerWindow,
	}
	input.OracleKeeper.SetParams(input.Ctx, newParams)

	storedParams := input.OracleKeeper.GetParams(input.Ctx)
	require.NotNil(t, storedParams)
	require.Equal(t, storedParams, newParams)
}

func TestFeederDelegation(t *testing.T) {
	input := CreateTestInput(t)

	// Test default getters and setters
	delegate := input.OracleKeeper.GetFeederDelegation(input.Ctx, ValAddrs[0])
	require.Equal(t, Addrs[0], delegate)

	input.OracleKeeper.SetFeederDelegation(input.Ctx, ValAddrs[0], Addrs[1])
	delegate = input.OracleKeeper.GetFeederDelegation(input.Ctx, ValAddrs[0])
	require.Equal(t, Addrs[1], delegate)
}

func TestIterateFeederDelegations(t *testing.T) {
	input := CreateTestInput(t)

	// Test default getters and setters
	delegate := input.OracleKeeper.GetFeederDelegation(input.Ctx, ValAddrs[0])
	require.Equal(t, Addrs[0], delegate)

	input.OracleKeeper.SetFeederDelegation(input.Ctx, ValAddrs[0], Addrs[1])

	var delegators []sdk.ValAddress
	var delegates []sdk.AccAddress
	input.OracleKeeper.IterateFeederDelegations(input.Ctx, func(delegator sdk.ValAddress, delegate sdk.AccAddress) (stop bool) {
		delegators = append(delegators, delegator)
		delegates = append(delegates, delegate)
		return false
	})

	require.Equal(t, 1, len(delegators))
	require.Equal(t, 1, len(delegates))
	require.Equal(t, ValAddrs[0], delegators[0])
	require.Equal(t, Addrs[1], delegates[0])
}

func TestVotePenaltyCounter(t *testing.T) {
	input := CreateTestInput(t)

	// Test default getters and setters
	counter := input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, ValAddrs[0])
	require.Equal(t, uint64(0), counter.MissCount)
	require.Equal(t, uint64(0), counter.AbstainCount)
	require.Equal(t, uint64(0), counter.SuccessCount)
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCount(input.Ctx, ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetAbstainCount(input.Ctx, ValAddrs[0]))

	missCounter := uint64(10)
	input.OracleKeeper.SetVotePenaltyCounter(input.Ctx, ValAddrs[0], missCounter, 0, 0)
	counter = input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, ValAddrs[0])
	require.Equal(t, missCounter, counter.MissCount)
	require.Equal(t, uint64(0), counter.AbstainCount)
	require.Equal(t, uint64(0), counter.SuccessCount)
	require.Equal(t, missCounter, input.OracleKeeper.GetMissCount(input.Ctx, ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetAbstainCount(input.Ctx, ValAddrs[0]))

	input.OracleKeeper.SetVotePenaltyCounter(input.Ctx, ValAddrs[0], missCounter, missCounter, missCounter)
	counter = input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, ValAddrs[0])
	require.Equal(t, missCounter, counter.MissCount)
	require.Equal(t, missCounter, counter.AbstainCount)
	require.Equal(t, missCounter, counter.SuccessCount)
	require.Equal(t, missCounter, input.OracleKeeper.GetMissCount(input.Ctx, ValAddrs[0]))
	require.Equal(t, missCounter, input.OracleKeeper.GetAbstainCount(input.Ctx, ValAddrs[0]))

	input.OracleKeeper.DeleteVotePenaltyCounter(input.Ctx, ValAddrs[0])
	counter = input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, ValAddrs[0])
	require.Equal(t, uint64(0), counter.MissCount)
	require.Equal(t, uint64(0), counter.AbstainCount)
	require.Equal(t, uint64(0), counter.SuccessCount)
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCount(input.Ctx, ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetAbstainCount(input.Ctx, ValAddrs[0]))

	// test increments
	input.OracleKeeper.IncrementSuccessCount(input.Ctx, ValAddrs[0])
	input.OracleKeeper.IncrementMissCount(input.Ctx, ValAddrs[0])
	input.OracleKeeper.IncrementMissCount(input.Ctx, ValAddrs[0])
	input.OracleKeeper.IncrementAbstainCount(input.Ctx, ValAddrs[0])
	input.OracleKeeper.IncrementAbstainCount(input.Ctx, ValAddrs[0])
	input.OracleKeeper.IncrementAbstainCount(input.Ctx, ValAddrs[0])
	counter = input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, ValAddrs[0])
	require.Equal(t, uint64(2), counter.MissCount)
	require.Equal(t, uint64(3), counter.AbstainCount)
	require.Equal(t, uint64(1), counter.SuccessCount)
	require.Equal(t, uint64(2), input.OracleKeeper.GetMissCount(input.Ctx, ValAddrs[0]))
	require.Equal(t, uint64(3), input.OracleKeeper.GetAbstainCount(input.Ctx, ValAddrs[0]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetSuccessCount(input.Ctx, ValAddrs[0]))
}

func TestIterateMissCounters(t *testing.T) {
	input := CreateTestInput(t)

	// Test default getters and setters
	counter := input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, ValAddrs[0])
	require.Equal(t, uint64(0), counter.MissCount)
	require.Equal(t, uint64(0), counter.MissCount)

	missCounter := uint64(10)
	input.OracleKeeper.SetVotePenaltyCounter(input.Ctx, ValAddrs[1], missCounter, missCounter, 0)

	var operators []sdk.ValAddress
	var votePenaltyCounters types.VotePenaltyCounters
	input.OracleKeeper.IterateVotePenaltyCounters(input.Ctx, func(delegator sdk.ValAddress, votePenaltyCounter types.VotePenaltyCounter) (stop bool) {
		operators = append(operators, delegator)
		votePenaltyCounters = append(votePenaltyCounters, votePenaltyCounter)
		return false
	})

	require.Equal(t, 1, len(operators))
	require.Equal(t, 1, len(votePenaltyCounters))
	require.Equal(t, ValAddrs[1], operators[0])
	require.Equal(t, missCounter, votePenaltyCounters[0].MissCount)
}

func TestAggregateVoteAddDelete(t *testing.T) {
	input := CreateTestInput(t)

	aggregateVote := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{
		{Denom: "foo", ExchangeRate: sdk.NewDec(-1)},
		{Denom: "foo", ExchangeRate: sdk.NewDec(0)},
		{Denom: "foo", ExchangeRate: sdk.NewDec(1)},
	}, sdk.ValAddress(Addrs[0]))
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, sdk.ValAddress(Addrs[0]), aggregateVote)

	KVote, err := input.OracleKeeper.GetAggregateExchangeRateVote(input.Ctx, sdk.ValAddress(Addrs[0]))
	require.NoError(t, err)
	require.Equal(t, aggregateVote, KVote)

	input.OracleKeeper.DeleteAggregateExchangeRateVote(input.Ctx, sdk.ValAddress(Addrs[0]))
	_, err = input.OracleKeeper.GetAggregateExchangeRateVote(input.Ctx, sdk.ValAddress(Addrs[0]))
	require.Error(t, err)
}

func TestAggregateVoteIterate(t *testing.T) {
	input := CreateTestInput(t)

	aggregateVote1 := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{
		{Denom: "foo", ExchangeRate: sdk.NewDec(-1)},
		{Denom: "foo", ExchangeRate: sdk.NewDec(0)},
		{Denom: "foo", ExchangeRate: sdk.NewDec(1)},
	}, sdk.ValAddress(Addrs[0]))
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, sdk.ValAddress(Addrs[0]), aggregateVote1)

	aggregateVote2 := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{
		{Denom: "foo", ExchangeRate: sdk.NewDec(-1)},
		{Denom: "foo", ExchangeRate: sdk.NewDec(0)},
		{Denom: "foo", ExchangeRate: sdk.NewDec(1)},
	}, sdk.ValAddress(Addrs[1]))
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, sdk.ValAddress(Addrs[1]), aggregateVote2)

	i := 0
	bigger := bytes.Compare(address.MustLengthPrefix(Addrs[0]), address.MustLengthPrefix(Addrs[1]))
	input.OracleKeeper.IterateAggregateExchangeRateVotes(input.Ctx, func(voter sdk.ValAddress, p types.AggregateExchangeRateVote) (stop bool) {
		if (i == 0 && bigger == -1) || (i == 1 && bigger == 1) {
			require.Equal(t, aggregateVote1, p)
			require.Equal(t, voter.String(), p.Voter)
		} else {
			require.Equal(t, aggregateVote2, p)
			require.Equal(t, voter.String(), p.Voter)
		}

		i++
		return false
	})
}

func TestVoteTargetGetSet(t *testing.T) {
	input := CreateTestInput(t)

	voteTargets := map[string]types.Denom{
		utils.MicroEthDenom:  {Name: utils.MicroEthDenom},
		utils.MicroUsdcDenom: {Name: utils.MicroUsdcDenom},
		utils.MicroAtomDenom: {Name: utils.MicroAtomDenom},
		utils.MicroSeiDenom:  {Name: utils.MicroSeiDenom},
	}

	for denom := range voteTargets {
		input.OracleKeeper.SetVoteTarget(input.Ctx, denom)
		denomInfo, err := input.OracleKeeper.GetVoteTarget(input.Ctx, denom)
		require.NoError(t, err)
		require.Equal(t, voteTargets[denom], denomInfo)
	}

	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	for denom := range voteTargets {
		_, err := input.OracleKeeper.GetVoteTarget(input.Ctx, denom)
		require.Error(t, err)
	}
}

func TestValidateFeeder(t *testing.T) {
	// initial setup
	input := CreateTestInput(t)
	addr, val := ValAddrs[0], ValPubKeys[0]
	addr1, val1 := ValAddrs[1], ValPubKeys[1]
	amt := sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction)
	sh := staking.NewHandler(input.StakingKeeper)
	ctx := input.Ctx

	// Validator created
	_, err := sh(ctx, NewTestMsgCreateValidator(addr, val, amt))
	require.NoError(t, err)
	_, err = sh(ctx, NewTestMsgCreateValidator(addr1, val1, amt))
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)

	require.Equal(
		t, input.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr)),
		sdk.NewCoins(sdk.NewCoin(input.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt, input.StakingKeeper.Validator(ctx, addr).GetBondedTokens())
	require.Equal(
		t, input.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(addr1)),
		sdk.NewCoins(sdk.NewCoin(input.StakingKeeper.GetParams(ctx).BondDenom, InitTokens.Sub(amt))),
	)
	require.Equal(t, amt, input.StakingKeeper.Validator(ctx, addr1).GetBondedTokens())

	require.NoError(t, input.OracleKeeper.ValidateFeeder(input.Ctx, sdk.AccAddress(addr), sdk.ValAddress(addr)))
	require.NoError(t, input.OracleKeeper.ValidateFeeder(input.Ctx, sdk.AccAddress(addr1), sdk.ValAddress(addr1)))

	// delegate works
	input.OracleKeeper.SetFeederDelegation(input.Ctx, sdk.ValAddress(addr), sdk.AccAddress(addr1))
	require.NoError(t, input.OracleKeeper.ValidateFeeder(input.Ctx, sdk.AccAddress(addr1), sdk.ValAddress(addr)))
	require.Error(t, input.OracleKeeper.ValidateFeeder(input.Ctx, sdk.AccAddress(Addrs[2]), sdk.ValAddress(addr)))

	// only active validators can do oracle votes
	validator, found := input.StakingKeeper.GetValidator(input.Ctx, sdk.ValAddress(addr))
	require.True(t, found)
	validator.Status = stakingtypes.Unbonded
	input.StakingKeeper.SetValidator(input.Ctx, validator)
	require.Error(t, input.OracleKeeper.ValidateFeeder(input.Ctx, sdk.AccAddress(addr1), sdk.ValAddress(addr)))
}

func TestPriceSnapshotGetSet(t *testing.T) {
	input := CreateTestInput(t)

	priceSnapshots := types.PriceSnapshots{
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(11),
				LastUpdate:   sdk.NewInt(20),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(12),
				LastUpdate:   sdk.NewInt(20),
			}),
		}, 1),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(21),
				LastUpdate:   sdk.NewInt(30),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(22),
				LastUpdate:   sdk.NewInt(30),
			}),
		}, 2),
	}

	input.OracleKeeper.SetPriceSnapshot(input.Ctx, priceSnapshots[0])
	input.OracleKeeper.SetPriceSnapshot(input.Ctx, priceSnapshots[1])

	snapshot := input.OracleKeeper.GetPriceSnapshot(input.Ctx, 1)
	require.Equal(t, priceSnapshots[0], snapshot)

	snapshot = input.OracleKeeper.GetPriceSnapshot(input.Ctx, 2)
	require.Equal(t, priceSnapshots[1], snapshot)
}

func TestPriceSnapshotAdd(t *testing.T) {
	input := CreateTestInput(t)

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(3500, 0))
	priceSnapshots := types.PriceSnapshots{
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(11),
				LastUpdate:   sdk.NewInt(20),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(12),
				LastUpdate:   sdk.NewInt(20),
			}),
		}, 50),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(21),
				LastUpdate:   sdk.NewInt(30),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(22),
				LastUpdate:   sdk.NewInt(30),
			}),
		}, 100),
	}
	expectedSnapshots := priceSnapshots

	for i := range priceSnapshots {
		input.OracleKeeper.AddPriceSnapshot(input.Ctx, priceSnapshots[i])
	}
	totalSnapshots := 0
	input.OracleKeeper.IteratePriceSnapshots(input.Ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		// assert that all the timestamps are correct
		require.Equal(t, expectedSnapshots[totalSnapshots], snapshot)
		totalSnapshots++
		return false
	})
	require.Equal(t, 2, totalSnapshots)

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(3660, 0))

	newSnapshot := types.NewPriceSnapshot(
		types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(31),
				LastUpdate:   sdk.NewInt(40),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(32),
				LastUpdate:   sdk.NewInt(40),
			}),
		},
		3660,
	)

	input.OracleKeeper.AddPriceSnapshot(input.Ctx, newSnapshot)

	expectedSnapshots = append(expectedSnapshots, newSnapshot)

	totalSnapshots = 0
	input.OracleKeeper.IteratePriceSnapshots(input.Ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		// assert that all the timestamps are correct
		require.Equal(t, expectedSnapshots[totalSnapshots], snapshot)
		totalSnapshots++
		return false
	})
	require.Equal(t, 3, totalSnapshots)

	// test iterate
	expectedTimestamps := []int64{50, 100, 3660}
	input.OracleKeeper.IteratePriceSnapshots(input.Ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		// assert that all the timestamps are correct
		require.Equal(t, expectedTimestamps[0], snapshot.SnapshotTimestamp)
		expectedTimestamps = expectedTimestamps[1:]
		return false
	})

	// test iterate reverse
	expectedTimestampsReverse := []int64{3660, 100, 50}
	input.OracleKeeper.IteratePriceSnapshotsReverse(input.Ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		// assert that all the timestamps are correct
		require.Equal(t, expectedTimestampsReverse[0], snapshot.SnapshotTimestamp)
		expectedTimestampsReverse = expectedTimestampsReverse[1:]
		return false
	})

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(10000, 0))

	newSnapshot2 := types.NewPriceSnapshot(
		types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(41),
				LastUpdate:   sdk.NewInt(50),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(42),
				LastUpdate:   sdk.NewInt(50),
			}),
		},
		10000,
	)
	input.OracleKeeper.AddPriceSnapshot(input.Ctx, newSnapshot2)

	expectedSnapshots = append(expectedSnapshots, newSnapshot2)
	expectedSnapshots = expectedSnapshots[2:]
	expectedTimestamps = []int64{3660, 10000}

	totalSnapshots = 0
	input.OracleKeeper.IteratePriceSnapshots(input.Ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		// assert that all the timestamps are correct
		require.Equal(t, expectedSnapshots[totalSnapshots], snapshot)
		require.Equal(t, expectedTimestamps[totalSnapshots], snapshot.SnapshotTimestamp)
		totalSnapshots++
		return false
	})
	require.Equal(t, 2, totalSnapshots)
}

func TestCalculateTwaps(t *testing.T) {
	input := CreateTestInput(t)

	_, err := input.OracleKeeper.CalculateTwaps(input.Ctx, 3600)
	require.Error(t, err)
	require.Equal(t, types.ErrNoTwapData, err)

	priceSnapshots := types.PriceSnapshots{
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(40),
				LastUpdate:   sdk.NewInt(1800),
			}),
		}, 1200),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(10),
				LastUpdate:   sdk.NewInt(3600),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(20),
				LastUpdate:   sdk.NewInt(3600),
			}),
		}, 3600),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(20),
				LastUpdate:   sdk.NewInt(4500),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(40),
				LastUpdate:   sdk.NewInt(4500),
			}),
		}, 4500),
	}
	for _, snap := range priceSnapshots {
		input.OracleKeeper.SetPriceSnapshot(input.Ctx, snap)
	}
	input.Ctx = input.Ctx.WithBlockTime(time.Unix(5400, 0))
	twaps, err := input.OracleKeeper.CalculateTwaps(input.Ctx, 3600)
	require.NoError(t, err)
	require.Equal(t, 2, len(twaps))
	atomTwap := twaps[0]
	ethTwap := twaps[1]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(3600), atomTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(35), atomTwap.Twap)

	require.Equal(t, utils.MicroEthDenom, ethTwap.Denom)
	// we expect each to have a lookback of 1800 instead of 3600 because the first interval data is missing
	require.Equal(t, int64(1800), ethTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(15), ethTwap.Twap)

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(6000, 0))

	// we still expect the out of range data point from 1200 to be kept for calculating TWAP for the full interval
	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(types.PriceSnapshotItems{
		types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
			ExchangeRate: sdk.NewDec(30),
			LastUpdate:   sdk.NewInt(6000),
		}),
	}, 6000))

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(6600, 0))

	twaps, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 3600)
	require.NoError(t, err)
	require.Equal(t, 2, len(twaps))
	atomTwap = twaps[0]
	ethTwap = twaps[1]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(3600), atomTwap.LookbackSeconds)

	expectedTwap, _ := sdk.NewDecFromStr("33.333333333333333333")
	require.Equal(t, expectedTwap, atomTwap.Twap)

	// microeth is included even though its not in the latest snapshot because its in one of the intermediate ones
	require.Equal(t, utils.MicroEthDenom, ethTwap.Denom)
	// we expect each to have a lookback of 3000 instead of 3600 because the first interval data is missing
	require.Equal(t, int64(3000), ethTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(17), ethTwap.Twap)

	// test with shorter lookback
	// microeth is not in this one because it's not in the snapshot used for TWAP calculation
	twaps, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 300)
	require.NoError(t, err)
	require.Equal(t, 1, len(twaps))
	atomTwap = twaps[0]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(300), atomTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(30), atomTwap.Twap)

	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(types.PriceSnapshotItems{
		types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
			ExchangeRate: sdk.NewDec(20),
			LastUpdate:   sdk.NewInt(6000),
		}),
	}, 6600))

	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(types.PriceSnapshotItems{
		types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
			ExchangeRate: sdk.NewDec(20),
			LastUpdate:   sdk.NewInt(6900),
		}),
	}, 6900))

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(6900, 0))

	// the older interval weight should be appropriately shorted to the start of the lookback interval,
	// so the TWAP should be 25 instead of 26.666 because the 20 and 30 are weighted 50-50 instead of 33.3-66.6
	twaps, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 600)
	require.NoError(t, err)
	require.Equal(t, 2, len(twaps))
	atomTwap = twaps[0]
	ethTwap = twaps[1]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(600), atomTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(25), atomTwap.Twap)

	require.Equal(t, utils.MicroEthDenom, ethTwap.Denom)
	require.Equal(t, int64(0), ethTwap.LookbackSeconds)
	require.Equal(t, sdk.ZeroDec(), ethTwap.Twap)

	// test error when lookback too large
	_, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 3700)
	require.Error(t, err)
	require.Equal(t, types.ErrInvalidTwapLookback, err)

	// test error when lookback is 0
	_, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 0)
	require.Error(t, err)
	require.Equal(t, types.ErrInvalidTwapLookback, err)
}

func TestCalculateTwapsWithUnsupportedDenom(t *testing.T) {
	input := CreateTestInput(t)

	_, err := input.OracleKeeper.CalculateTwaps(input.Ctx, 3600)
	require.Error(t, err)
	require.Equal(t, types.ErrNoTwapData, err)

	priceSnapshots := types.PriceSnapshots{
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(40),
				LastUpdate:   sdk.NewInt(1800),
			}),
		}, 1200),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(10),
				LastUpdate:   sdk.NewInt(3600),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(20),
				LastUpdate:   sdk.NewInt(3600),
			}),
		}, 3600),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(20),
				LastUpdate:   sdk.NewInt(4500),
			}),
			types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(40),
				LastUpdate:   sdk.NewInt(4500),
			}),
		}, 4500),
	}
	for _, snap := range priceSnapshots {
		input.OracleKeeper.SetPriceSnapshot(input.Ctx, snap)
	}
	// eth removed
	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(5400, 0))
	twaps, err := input.OracleKeeper.CalculateTwaps(input.Ctx, 3600)
	require.NoError(t, err)
	require.Equal(t, 1, len(twaps))
	atomTwap := twaps[0]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(3600), atomTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(35), atomTwap.Twap)

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(6000, 0))

	// we still expect the out of range data point from 1200 to be kept for calculating TWAP for the full interval
	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(types.PriceSnapshotItems{
		types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
			ExchangeRate: sdk.NewDec(30),
			LastUpdate:   sdk.NewInt(6000),
		}),
	}, 6000))

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(6600, 0))

	twaps, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 3600)
	require.NoError(t, err)
	require.Equal(t, 1, len(twaps))
	atomTwap = twaps[0]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(3600), atomTwap.LookbackSeconds)

	expectedTwap, _ := sdk.NewDecFromStr("33.333333333333333333")
	require.Equal(t, expectedTwap, atomTwap.Twap)

	// test with shorter lookback
	// microeth is not in this one because it's not in the snapshot used for TWAP calculation
	twaps, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 300)
	require.NoError(t, err)
	require.Equal(t, 1, len(twaps))
	atomTwap = twaps[0]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(300), atomTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(30), atomTwap.Twap)

	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(types.PriceSnapshotItems{
		types.NewPriceSnapshotItem(utils.MicroAtomDenom, types.OracleExchangeRate{
			ExchangeRate: sdk.NewDec(20),
			LastUpdate:   sdk.NewInt(6000),
		}),
	}, 6600))

	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(types.PriceSnapshotItems{
		types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
			ExchangeRate: sdk.NewDec(20),
			LastUpdate:   sdk.NewInt(6900),
		}),
	}, 6900))

	input.Ctx = input.Ctx.WithBlockTime(time.Unix(6900, 0))

	// the older interval weight should be appropriately shorted to the start of the lookback interval,
	// so the TWAP should be 25 instead of 26.666 because the 20 and 30 are weighted 50-50 instead of 33.3-66.6
	twaps, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 600)
	require.NoError(t, err)
	require.Equal(t, 1, len(twaps))
	atomTwap = twaps[0]
	require.Equal(t, utils.MicroAtomDenom, atomTwap.Denom)
	require.Equal(t, int64(600), atomTwap.LookbackSeconds)
	require.Equal(t, sdk.NewDec(25), atomTwap.Twap)

	// test error when lookback too large
	_, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 3700)
	require.Error(t, err)
	require.Equal(t, types.ErrInvalidTwapLookback, err)

	// test error when lookback is 0
	_, err = input.OracleKeeper.CalculateTwaps(input.Ctx, 0)
	require.Error(t, err)
	require.Equal(t, types.ErrInvalidTwapLookback, err)
}

func TestSpamPreventionCounter(t *testing.T) {
	input := CreateTestInput(t)

	require.NoError(t, input.OracleKeeper.CheckAndSetSpamPreventionCounter(input.Ctx, sdk.ValAddress(Addrs[0])))
	require.Error(t, input.OracleKeeper.CheckAndSetSpamPreventionCounter(input.Ctx, sdk.ValAddress(Addrs[0])))

	input.Ctx = input.Ctx.WithBlockHeight(3)

	require.NoError(t, input.OracleKeeper.CheckAndSetSpamPreventionCounter(input.Ctx, sdk.ValAddress(Addrs[0])))
	require.NoError(t, input.OracleKeeper.CheckAndSetSpamPreventionCounter(input.Ctx, sdk.ValAddress(Addrs[1])))
}
