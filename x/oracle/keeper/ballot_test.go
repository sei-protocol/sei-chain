package keeper

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

func TestOrganizeAggregate(t *testing.T) {
	input := CreateTestInput(t)

	power := int64(100)
	amt := sdk.TokensFromConsensusPower(power, sdk.DefaultPowerReduction)
	sh := staking.NewHandler(input.StakingKeeper)
	ctx := input.Ctx

	// Validator created
	_, err := sh(ctx, NewTestMsgCreateValidator(ValAddrs[0], ValPubKeys[0], amt))
	require.NoError(t, err)
	_, err = sh(ctx, NewTestMsgCreateValidator(ValAddrs[1], ValPubKeys[1], amt))
	require.NoError(t, err)
	_, err = sh(ctx, NewTestMsgCreateValidator(ValAddrs[2], ValPubKeys[2], amt))
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)

	sdrBallot := types.ExchangeRateBallot{
		types.NewVoteForTally(sdk.NewDec(17), utils.MicroSeiDenom, ValAddrs[0], power),
		types.NewVoteForTally(sdk.NewDec(10), utils.MicroSeiDenom, ValAddrs[1], power),
		types.NewVoteForTally(sdk.NewDec(6), utils.MicroSeiDenom, ValAddrs[2], power),
	}
	krwBallot := types.ExchangeRateBallot{
		types.NewVoteForTally(sdk.NewDec(1000), utils.MicroAtomDenom, ValAddrs[0], power),
		types.NewVoteForTally(sdk.NewDec(1300), utils.MicroAtomDenom, ValAddrs[1], power),
		types.NewVoteForTally(sdk.NewDec(2000), utils.MicroAtomDenom, ValAddrs[2], power),
	}

	for i := range sdrBallot {
		input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, ValAddrs[i],
			types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{
				{Denom: sdrBallot[i].Denom, ExchangeRate: sdrBallot[i].ExchangeRate},
				{Denom: krwBallot[i].Denom, ExchangeRate: krwBallot[i].ExchangeRate},
			}, ValAddrs[i]))
	}

	// organize votes by denom
	ballotMap := input.OracleKeeper.OrganizeBallotByDenom(input.Ctx, map[string]types.Claim{
		ValAddrs[0].String(): {
			Power:     power,
			WinCount:  0,
			Recipient: ValAddrs[0],
		},
		ValAddrs[1].String(): {
			Power:     power,
			WinCount:  0,
			Recipient: ValAddrs[1],
		},
		ValAddrs[2].String(): {
			Power:     power,
			WinCount:  0,
			Recipient: ValAddrs[2],
		},
	})

	// sort each ballot for comparison
	sort.Sort(sdrBallot)
	sort.Sort(krwBallot)
	sort.Sort(ballotMap[utils.MicroSeiDenom])
	sort.Sort(ballotMap[utils.MicroAtomDenom])

	require.Equal(t, sdrBallot, ballotMap[utils.MicroSeiDenom])
	require.Equal(t, krwBallot, ballotMap[utils.MicroAtomDenom])
}

func TestClearBallots(t *testing.T) {
	input := CreateTestInput(t)

	power := int64(100)
	amt := sdk.TokensFromConsensusPower(power, sdk.DefaultPowerReduction)
	sh := staking.NewHandler(input.StakingKeeper)
	ctx := input.Ctx

	// Validator created
	_, err := sh(ctx, NewTestMsgCreateValidator(ValAddrs[0], ValPubKeys[0], amt))
	require.NoError(t, err)
	_, err = sh(ctx, NewTestMsgCreateValidator(ValAddrs[1], ValPubKeys[1], amt))
	require.NoError(t, err)
	_, err = sh(ctx, NewTestMsgCreateValidator(ValAddrs[2], ValPubKeys[2], amt))
	require.NoError(t, err)
	staking.EndBlocker(ctx, input.StakingKeeper)

	sdrBallot := types.ExchangeRateBallot{
		types.NewVoteForTally(sdk.NewDec(17), utils.MicroSeiDenom, ValAddrs[0], power),
		types.NewVoteForTally(sdk.NewDec(10), utils.MicroSeiDenom, ValAddrs[1], power),
		types.NewVoteForTally(sdk.NewDec(6), utils.MicroSeiDenom, ValAddrs[2], power),
	}
	krwBallot := types.ExchangeRateBallot{
		types.NewVoteForTally(sdk.NewDec(1000), utils.MicroAtomDenom, ValAddrs[0], power),
		types.NewVoteForTally(sdk.NewDec(1300), utils.MicroAtomDenom, ValAddrs[1], power),
		types.NewVoteForTally(sdk.NewDec(2000), utils.MicroAtomDenom, ValAddrs[2], power),
	}

	for i := range sdrBallot {
		input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, ValAddrs[i],
			types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{
				{Denom: sdrBallot[i].Denom, ExchangeRate: sdrBallot[i].ExchangeRate},
				{Denom: krwBallot[i].Denom, ExchangeRate: krwBallot[i].ExchangeRate},
			}, ValAddrs[i]))
	}

	input.OracleKeeper.ClearBallots(input.Ctx, 5)

	voteCounter := 0
	input.OracleKeeper.IterateAggregateExchangeRateVotes(input.Ctx, func(_ sdk.ValAddress, _ types.AggregateExchangeRateVote) bool {
		voteCounter++
		return false
	})
	require.Equal(t, voteCounter, 0)
}

func TestApplyWhitelist(t *testing.T) {
	input := CreateTestInput(t)

	input.OracleKeeper.ApplyWhitelist(input.Ctx, types.DenomList{
		types.Denom{
			Name: "uatom",
		},
		types.Denom{
			Name: "uusdc",
		},
	}, map[string]types.Denom{})

	_, err := input.OracleKeeper.GetVoteTarget(input.Ctx, "uatom")
	require.NoError(t, err)

	_, err = input.OracleKeeper.GetVoteTarget(input.Ctx, "uusdc")
	require.NoError(t, err)

	metadata, ok := input.BankKeeper.GetDenomMetaData(input.Ctx, "uatom")
	require.True(t, ok)
	require.Equal(t, metadata.Base, "uatom")
	require.Equal(t, metadata.Display, "atom")
	require.Equal(t, len(metadata.DenomUnits), 3)
	require.Equal(t, metadata.Description, "atom")

	metadata, ok = input.BankKeeper.GetDenomMetaData(input.Ctx, "uusdc")
	require.True(t, ok)
	require.Equal(t, metadata.Base, "uusdc")
	require.Equal(t, metadata.Display, "usdc")
	require.Equal(t, len(metadata.DenomUnits), 3)
	require.Equal(t, metadata.Description, "usdc")
}
