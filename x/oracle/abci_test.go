package oracle_test

import (
	"fmt"
	"math"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

func TestOracleThreshold(t *testing.T) {
	input, h := setup(t)
	exchangeRateStr := randomExchangeRate.String() + utils.MicroAtomDenom

	// Case 1.
	// Less than the threshold signs, exchange rate consensus fails
	salt := "1"
	hash := types.GetAggregateVoteHash(salt, exchangeRateStr, keeper.ValAddrs[0])
	prevoteMsg := types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[0], keeper.ValAddrs[0])
	voteMsg := types.NewMsgAggregateExchangeRateVote(salt, exchangeRateStr, keeper.Addrs[0], keeper.ValAddrs[0])

	_, err1 := h(input.Ctx.WithBlockHeight(0), prevoteMsg)
	_, err2 := h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err1)
	require.NoError(t, err2)

	oracle.EndBlocker(input.Ctx.WithBlockHeight(1), input.OracleKeeper)

	_, _, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx.WithBlockHeight(1), utils.MicroAtomDenom)
	require.Error(t, err)

	// Case 2.
	// More than the threshold signs, exchange rate consensus succeeds
	salt = "1"
	hash = types.GetAggregateVoteHash(salt, exchangeRateStr, keeper.ValAddrs[0])
	prevoteMsg = types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[0], keeper.ValAddrs[0])
	voteMsg = types.NewMsgAggregateExchangeRateVote(salt, exchangeRateStr, keeper.Addrs[0], keeper.ValAddrs[0])

	_, err1 = h(input.Ctx.WithBlockHeight(0), prevoteMsg)
	_, err2 = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err1)
	require.NoError(t, err2)

	salt = "2"
	hash = types.GetAggregateVoteHash(salt, exchangeRateStr, keeper.ValAddrs[1])
	prevoteMsg = types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[1], keeper.ValAddrs[1])
	voteMsg = types.NewMsgAggregateExchangeRateVote(salt, exchangeRateStr, keeper.Addrs[1], keeper.ValAddrs[1])

	_, err1 = h(input.Ctx.WithBlockHeight(0), prevoteMsg)
	_, err2 = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err1)
	require.NoError(t, err2)

	salt = "3"
	hash = types.GetAggregateVoteHash(salt, exchangeRateStr, keeper.ValAddrs[2])
	prevoteMsg = types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[2], keeper.ValAddrs[2])
	voteMsg = types.NewMsgAggregateExchangeRateVote(salt, exchangeRateStr, keeper.Addrs[2], keeper.ValAddrs[2])

	_, err1 = h(input.Ctx.WithBlockHeight(0), prevoteMsg)
	_, err2 = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err1)
	require.NoError(t, err2)

	oracle.EndBlocker(input.Ctx.WithBlockHeight(1), input.OracleKeeper)

	rate, lastUpdate, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx.WithBlockHeight(1), utils.MicroAtomDenom)
	require.NoError(t, err)
	require.Equal(t, randomExchangeRate, rate)
	require.Equal(t, int64(1), lastUpdate.Int64())

	// Case 3.
	// Increase voting power of absent validator, exchange rate consensus fails
	val, _ := input.StakingKeeper.GetValidator(input.Ctx, keeper.ValAddrs[2])
	input.StakingKeeper.Delegate(input.Ctx.WithBlockHeight(0), keeper.Addrs[2], stakingAmt.MulRaw(3), stakingtypes.Unbonded, val, false)

	salt = "1"
	hash = types.GetAggregateVoteHash(salt, exchangeRateStr, keeper.ValAddrs[0])
	prevoteMsg = types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[0], keeper.ValAddrs[0])
	voteMsg = types.NewMsgAggregateExchangeRateVote(salt, exchangeRateStr, keeper.Addrs[0], keeper.ValAddrs[0])

	_, err1 = h(input.Ctx.WithBlockHeight(0), prevoteMsg)
	_, err2 = h(input.Ctx.WithBlockHeight(1), voteMsg)
	require.NoError(t, err1)
	require.NoError(t, err2)

	salt = "2"
	hash = types.GetAggregateVoteHash(salt, exchangeRateStr, keeper.ValAddrs[1])
	prevoteMsg = types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[1], keeper.ValAddrs[1])
	voteMsg = types.NewMsgAggregateExchangeRateVote(salt, exchangeRateStr, keeper.Addrs[1], keeper.ValAddrs[1])

	_, err1 = h(input.Ctx.WithBlockHeight(2), prevoteMsg)
	_, err2 = h(input.Ctx.WithBlockHeight(3), voteMsg)
	require.NoError(t, err1)
	require.NoError(t, err2)

	oracle.EndBlocker(input.Ctx.WithBlockHeight(3), input.OracleKeeper)

	rate, lastUpdate, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx.WithBlockHeight(3), utils.MicroAtomDenom)
	require.NoError(t, err)
	require.Equal(t, randomExchangeRate, rate)
	// This should still be an older value due to staleness
	require.Equal(t, int64(1), lastUpdate.Int64())
}

func TestOracleDrop(t *testing.T) {
	input, h := setup(t)

	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom, randomExchangeRate)

	// Account 1, KRW
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)

	// Immediately swap halt after an illiquid oracle vote
	oracle.EndBlocker(input.Ctx, input.OracleKeeper)

	rate, lastUpdate, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.NoError(t, err)
	require.Equal(t, randomExchangeRate, rate)
	// The value should have a stale height
	require.Equal(t, sdk.ZeroInt(), lastUpdate)
}

func TestOracleTally(t *testing.T) {
	input, _ := setup(t)

	ballot := types.ExchangeRateBallot{}
	rates, valAddrs, stakingKeeper := types.GenerateRandomTestCase()
	input.OracleKeeper.StakingKeeper = stakingKeeper
	h := oracle.NewHandler(input.OracleKeeper)
	for i, rate := range rates {

		decExchangeRate := sdk.NewDecWithPrec(int64(rate*math.Pow10(keeper.OracleDecPrecision)), int64(keeper.OracleDecPrecision))
		exchangeRateStr := decExchangeRate.String() + utils.MicroAtomDenom

		salt := fmt.Sprintf("%d", i)
		hash := types.GetAggregateVoteHash(salt, exchangeRateStr, valAddrs[i])
		prevoteMsg := types.NewMsgAggregateExchangeRatePrevote(hash, sdk.AccAddress(valAddrs[i]), valAddrs[i])
		voteMsg := types.NewMsgAggregateExchangeRateVote(salt, exchangeRateStr, sdk.AccAddress(valAddrs[i]), valAddrs[i])

		_, err1 := h(input.Ctx.WithBlockHeight(0), prevoteMsg)
		_, err2 := h(input.Ctx.WithBlockHeight(1), voteMsg)
		require.NoError(t, err1)
		require.NoError(t, err2)

		power := stakingAmt.QuoRaw(utils.MicroUnit).Int64()
		if decExchangeRate.IsZero() {
			power = int64(0)
		}

		vote := types.NewVoteForTally(
			decExchangeRate, utils.MicroAtomDenom, valAddrs[i], power)
		ballot = append(ballot, vote)

		// change power of every three validator
		if i%3 == 0 {
			stakingKeeper.Validators()[i].SetConsensusPower(int64(i + 1))
		}
	}

	validatorClaimMap := make(map[string]types.Claim)
	for _, valAddr := range valAddrs {
		validatorClaimMap[valAddr.String()] = types.Claim{
			Power:     stakingKeeper.Validator(input.Ctx, valAddr).GetConsensusPower(sdk.DefaultPowerReduction),
			Weight:    int64(0),
			WinCount:  int64(0),
			Recipient: valAddr,
		}
	}
	sort.Sort(ballot)
	weightedMedian := ballot.WeightedMedianWithAssertion()
	standardDeviation := ballot.StandardDeviation(weightedMedian)
	maxSpread := weightedMedian.Mul(input.OracleKeeper.RewardBand(input.Ctx).QuoInt64(2))

	if standardDeviation.GT(maxSpread) {
		maxSpread = standardDeviation
	}

	expectedValidatorClaimMap := make(map[string]types.Claim)
	for _, valAddr := range valAddrs {
		expectedValidatorClaimMap[valAddr.String()] = types.Claim{
			Power:     stakingKeeper.Validator(input.Ctx, valAddr).GetConsensusPower(sdk.DefaultPowerReduction),
			Weight:    int64(0),
			WinCount:  int64(0),
			Recipient: valAddr,
		}
	}

	for _, vote := range ballot {
		if (vote.ExchangeRate.GTE(weightedMedian.Sub(maxSpread)) &&
			vote.ExchangeRate.LTE(weightedMedian.Add(maxSpread))) ||
			!vote.ExchangeRate.IsPositive() {
			key := vote.Voter.String()
			claim := expectedValidatorClaimMap[key]
			claim.Weight += vote.Power
			claim.WinCount++
			expectedValidatorClaimMap[key] = claim
		}
	}

	tallyMedian := oracle.Tally(input.Ctx, ballot, input.OracleKeeper.RewardBand(input.Ctx), validatorClaimMap)

	require.Equal(t, validatorClaimMap, expectedValidatorClaimMap)
	require.Equal(t, tallyMedian.MulInt64(100).TruncateInt(), weightedMedian.MulInt64(100).TruncateInt())
}

func TestOracleTallyTiming(t *testing.T) {
	input, h := setup(t)

	// all the keeper.Addrs vote for the block ... not last period block yet, so tally fails
	for i := range keeper.Addrs[:2] {
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, i)
	}

	params := input.OracleKeeper.GetParams(input.Ctx)
	params.VotePeriod = 10 // set vote period to 10 for now, for convenience
	input.OracleKeeper.SetParams(input.Ctx, params)
	require.Equal(t, 0, int(input.Ctx.BlockHeight()))

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	_, _, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.Error(t, err)

	input.Ctx = input.Ctx.WithBlockHeight(int64(params.VotePeriod - 1))

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	_, _, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.NoError(t, err)
}

func TestInvalidVotesSlashing(t *testing.T) {
	input, h := setup(t)
	params := input.OracleKeeper.GetParams(input.Ctx)
	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)

	votePeriodsPerWindow := sdk.NewDec(int64(input.OracleKeeper.SlashWindow(input.Ctx))).QuoInt64(int64(input.OracleKeeper.VotePeriod(input.Ctx))).TruncateInt64()
	slashFraction := input.OracleKeeper.SlashFraction(input.Ctx)
	minValidPerWindow := input.OracleKeeper.MinValidPerWindow(input.Ctx)

	for i := uint64(0); i < uint64(sdk.OneDec().Sub(minValidPerWindow).MulInt64(votePeriodsPerWindow).TruncateInt64()); i++ {
		input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

		// Account 1, KRW
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)

		// Account 2, KRW, miss vote
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate.Add(sdk.NewDec(100000000000000))}}, 1)

		// Account 3, KRW
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

		oracle.EndBlocker(input.Ctx, input.OracleKeeper)
		require.Equal(t, i+1, input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	}

	validator := input.StakingKeeper.Validator(input.Ctx, keeper.ValAddrs[1])
	require.Equal(t, stakingAmt, validator.GetBondedTokens())

	// one more miss vote will inccur keeper.ValAddrs[1] slashing
	// Account 1, KRW
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)

	// Account 2, KRW, miss vote
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate.Add(sdk.NewDec(100000000000000))}}, 1)

	// Account 3, KRW
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

	input.Ctx = input.Ctx.WithBlockHeight(votePeriodsPerWindow - 1)
	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	validator = input.StakingKeeper.Validator(input.Ctx, keeper.ValAddrs[1])
	require.Equal(t, sdk.OneDec().Sub(slashFraction).MulInt(stakingAmt).TruncateInt(), validator.GetBondedTokens())
}

func TestWhitelistSlashing(t *testing.T) {
	input, h := setup(t)

	votePeriodsPerWindow := sdk.NewDec(int64(input.OracleKeeper.SlashWindow(input.Ctx))).QuoInt64(int64(input.OracleKeeper.VotePeriod(input.Ctx))).TruncateInt64()
	slashFraction := input.OracleKeeper.SlashFraction(input.Ctx)
	minValidPerWindow := input.OracleKeeper.MinValidPerWindow(input.Ctx)

	for i := uint64(0); i < uint64(sdk.OneDec().Sub(minValidPerWindow).MulInt64(votePeriodsPerWindow).TruncateInt64()); i++ {
		input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

		// Account 2, KRW
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 1)
		// Account 3, KRW
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

		oracle.EndBlocker(input.Ctx, input.OracleKeeper)
		require.Equal(t, i+1, input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	}

	validator := input.StakingKeeper.Validator(input.Ctx, keeper.ValAddrs[0])
	require.Equal(t, stakingAmt, validator.GetBondedTokens())

	// one more miss vote will inccur Account 1 slashing

	// Account 2, KRW
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 1)
	// Account 3, KRW
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

	input.Ctx = input.Ctx.WithBlockHeight(votePeriodsPerWindow - 1)
	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	validator = input.StakingKeeper.Validator(input.Ctx, keeper.ValAddrs[0])
	require.Equal(t, sdk.OneDec().Sub(slashFraction).MulInt(stakingAmt).TruncateInt(), validator.GetBondedTokens())
}

func TestNotPassedBallotSlashing(t *testing.T) {
	input, h := setup(t)
	params := input.OracleKeeper.GetParams(input.Ctx)
	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)

	input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

	// Account 1, KRW
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[2]))
}

func TestNotPassedBallotSlashingInvalidVotes(t *testing.T) {
	input, h := setupN(t, 7)
	params := input.OracleKeeper.GetParams(input.Ctx)
	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)

	input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

	// Account 1
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)
	// Account 2
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 1)
	// Account 3
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate.Add(sdk.NewDec(100000000000000))}}, 2)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)

	// 4-7 should be missed due to not voting
	// 3 should be missed due to out of bounds
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[2]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[3]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[4]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[5]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[6]))
}

func TestInvalidVoteOnAssetUnderThresholdMisses(t *testing.T) {
	input, h := setupN(t, 7)
	params := input.OracleKeeper.GetParams(input.Ctx)
	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}, {Name: utils.MicroEthDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroEthDenom)

	input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

	// Account 1
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: randomExchangeRate}}, 0)
	// Account 2
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: randomExchangeRate}}, 1)
	// Account 3
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: randomExchangeRate}}, 2)

	// rest of accounts
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: randomExchangeRate}}, 3)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: randomExchangeRate}}, 4)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 5)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 6)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	endBlockerHeight := input.Ctx.BlockHeight()

	// 6 and 7 should be missed due to not voting on second asset
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[2]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[3]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[4]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[5]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[6]))

	input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

	rate, lastUpdate, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.NoError(t, err)
	require.Equal(t, randomExchangeRate, rate)
	require.Equal(t, endBlockerHeight, lastUpdate.Int64())

	rate, lastUpdate, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroEthDenom)
	require.NoError(t, err)
	require.Equal(t, randomExchangeRate, rate)
	require.Equal(t, endBlockerHeight, lastUpdate.Int64())

	input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

	// Account 1
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: anotherRandomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: anotherRandomExchangeRate}}, 0)
	// Account 2
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: anotherRandomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: anotherRandomExchangeRate}}, 1)
	// Account 3
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: anotherRandomExchangeRate}, {Denom: utils.MicroEthDenom, Amount: anotherRandomExchangeRate.Add(sdk.NewDec(100000000000000))}}, 2)

	// rest of accounts meet threshold only for one asset
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: anotherRandomExchangeRate}}, 3)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: anotherRandomExchangeRate}}, 4)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: anotherRandomExchangeRate}}, 5)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: anotherRandomExchangeRate}}, 6)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	newEndBlockerHeight := input.Ctx.BlockHeight()

	// 4-7 should be missed due to not voting on second asset
	// 3 should have missed due to out of bounds value even though it didnt meet voting threshold
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[2]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[3]))
	require.Equal(t, uint64(1), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[4]))
	require.Equal(t, uint64(2), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[5]))
	require.Equal(t, uint64(2), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[6]))

	input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

	rate, lastUpdate, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.NoError(t, err)
	require.Equal(t, anotherRandomExchangeRate, rate)
	require.Equal(t, newEndBlockerHeight, lastUpdate.Int64())

	// the old value should be persisted because asset didnt meet ballot threshold
	rate, lastUpdate, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroEthDenom)
	require.NoError(t, err)
	require.Equal(t, randomExchangeRate, rate)
	// block height should be old
	require.Equal(t, endBlockerHeight, lastUpdate.Int64())
}

func TestAbstainSlashing(t *testing.T) {
	input, h := setup(t)
	params := input.OracleKeeper.GetParams(input.Ctx)
	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)

	votePeriodsPerWindow := sdk.NewDec(int64(input.OracleKeeper.SlashWindow(input.Ctx))).QuoInt64(int64(input.OracleKeeper.VotePeriod(input.Ctx))).TruncateInt64()
	minValidPerWindow := input.OracleKeeper.MinValidPerWindow(input.Ctx)
	slashFraction := input.OracleKeeper.SlashFraction(input.Ctx)

	limit := uint64(sdk.OneDec().Sub(minValidPerWindow).MulInt64(votePeriodsPerWindow).TruncateInt64())
	for i := uint64(0); i <= limit; i++ {
		input.Ctx = input.Ctx.WithBlockHeight(input.Ctx.BlockHeight() + 1)

		// Account 1, KRW
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)

		// Account 2, KRW, abstain vote - should count as miss
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: sdk.ZeroDec()}}, 1)

		// Account 3, KRW
		makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

		oracle.EndBlocker(input.Ctx, input.OracleKeeper)
		require.Equal(t, uint64(i+1%limit), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	}

	input.Ctx = input.Ctx.WithBlockHeight(votePeriodsPerWindow - 1)
	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	validator := input.StakingKeeper.Validator(input.Ctx, keeper.ValAddrs[1])
	require.Equal(t, sdk.OneDec().Sub(slashFraction).MulInt(stakingAmt).TruncateInt(), validator.GetBondedTokens())
}

func TestVoteTargets(t *testing.T) {
	input, h := setup(t)
	params := input.OracleKeeper.GetParams(input.Ctx)
	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}, {Name: utils.MicroAtomDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)

	// KRW
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 1)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)

	// no missing current
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[2]))

	// vote targets are {KRW, SDR}
	require.Equal(t, []string{utils.MicroAtomDenom}, input.OracleKeeper.GetVoteTargets(input.Ctx))

	_, err := input.OracleKeeper.GetVoteTarget(input.Ctx, utils.MicroAtomDenom)
	require.NoError(t, err)

	// delete SDR
	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)

	// KRW, missing
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 1)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)

	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[2]))

	// SDR must be deleted
	require.Equal(t, []string{utils.MicroAtomDenom}, input.OracleKeeper.GetVoteTargets(input.Ctx))

	_, err = input.OracleKeeper.GetVoteTarget(input.Ctx, "undefined")
	require.Error(t, err)

	params.Whitelist = types.DenomList{{Name: utils.MicroAtomDenom}}
	input.OracleKeeper.SetParams(input.Ctx, params)

	// KRW, no missing
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 0)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 1)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: randomExchangeRate}}, 2)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)

	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[0]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[1]))
	require.Equal(t, uint64(0), input.OracleKeeper.GetMissCounter(input.Ctx, keeper.ValAddrs[2]))

	_, err = input.OracleKeeper.GetVoteTarget(input.Ctx, utils.MicroAtomDenom)
	require.NoError(t, err)
}

func TestAbstainWithSmallStakingPower(t *testing.T) {
	input, h := setupWithSmallVotingPower(t)

	input.OracleKeeper.ClearVoteTargets(input.Ctx)
	input.OracleKeeper.SetVoteTarget(input.Ctx, utils.MicroAtomDenom)
	makeAggregatePrevoteAndVote(t, input, h, 0, sdk.DecCoins{{Denom: utils.MicroAtomDenom, Amount: sdk.ZeroDec()}}, 0)

	oracle.EndBlocker(input.Ctx, input.OracleKeeper)
	_, _, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.Error(t, err)
}

func makeAggregatePrevoteAndVote(t *testing.T, input keeper.TestInput, h sdk.Handler, height int64, rates sdk.DecCoins, idx int) {
	// Account 1, SDR
	salt := "1"
	hash := types.GetAggregateVoteHash(salt, rates.String(), keeper.ValAddrs[idx])

	prevoteMsg := types.NewMsgAggregateExchangeRatePrevote(hash, keeper.Addrs[idx], keeper.ValAddrs[idx])
	_, err := h(input.Ctx.WithBlockHeight(height), prevoteMsg)
	require.NoError(t, err)

	voteMsg := types.NewMsgAggregateExchangeRateVote(salt, rates.String(), keeper.Addrs[idx], keeper.ValAddrs[idx])
	_, err = h(input.Ctx.WithBlockHeight(height+1), voteMsg)
	require.NoError(t, err)
}
