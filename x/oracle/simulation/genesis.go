package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

// Simulation parameter constants
const (
	votePeriodKey               = "vote_period"
	voteThresholdKey            = "vote_threshold"
	rewardBandKey               = "reward_band"
	rewardDistributionWindowKey = "reward_distribution_window"
	slashFractionKey            = "slash_fraction"
	slashWindowKey              = "slash_window"
	minValidPerWindowKey        = "min_valid_per_window"
)

// GenVotePeriod randomized VotePeriod
func GenVotePeriod(r *rand.Rand) uint64 {
	return uint64(1 + r.Intn(100))
}

// GenVoteThreshold randomized VoteThreshold
func GenVoteThreshold(r *rand.Rand) sdk.Dec {
	return sdk.NewDecWithPrec(333, 3).Add(sdk.NewDecWithPrec(int64(r.Intn(333)), 3))
}

// GenRewardBand randomized RewardBand
func GenRewardBand(r *rand.Rand) sdk.Dec {
	return sdk.ZeroDec().Add(sdk.NewDecWithPrec(int64(r.Intn(100)), 3))
}

// GenSlashFraction randomized SlashFraction
func GenSlashFraction(r *rand.Rand) sdk.Dec {
	return sdk.ZeroDec().Add(sdk.NewDecWithPrec(int64(r.Intn(100)), 3))
}

// GenSlashWindow randomized SlashWindow
func GenSlashWindow(r *rand.Rand) uint64 {
	return uint64(100 + r.Intn(100000))
}

// GenMinValidPerWindow randomized MinValidPerWindow
func GenMinValidPerWindow(r *rand.Rand) sdk.Dec {
	return sdk.ZeroDec().Add(sdk.NewDecWithPrec(int64(r.Intn(500)), 3))
}

// RandomizedGenState generates a random GenesisState for oracle
func RandomizedGenState(simState *module.SimulationState) {
	var votePeriod uint64
	simState.AppParams.GetOrGenerate(
		simState.Cdc, votePeriodKey, &votePeriod, simState.Rand,
		func(r *rand.Rand) { votePeriod = GenVotePeriod(r) },
	)

	var voteThreshold sdk.Dec
	simState.AppParams.GetOrGenerate(
		simState.Cdc, voteThresholdKey, &voteThreshold, simState.Rand,
		func(r *rand.Rand) { voteThreshold = GenVoteThreshold(r) },
	)

	var rewardBand sdk.Dec
	simState.AppParams.GetOrGenerate(
		simState.Cdc, rewardBandKey, &rewardBand, simState.Rand,
		func(r *rand.Rand) { rewardBand = GenRewardBand(r) },
	)

	var slashFraction sdk.Dec
	simState.AppParams.GetOrGenerate(
		simState.Cdc, slashFractionKey, &slashFraction, simState.Rand,
		func(r *rand.Rand) { slashFraction = GenSlashFraction(r) },
	)

	var slashWindow uint64
	simState.AppParams.GetOrGenerate(
		simState.Cdc, slashWindowKey, &slashWindow, simState.Rand,
		func(r *rand.Rand) { slashWindow = GenSlashWindow(r) },
	)

	var minValidPerWindow sdk.Dec
	simState.AppParams.GetOrGenerate(
		simState.Cdc, minValidPerWindowKey, &minValidPerWindow, simState.Rand,
		func(r *rand.Rand) { minValidPerWindow = GenMinValidPerWindow(r) },
	)

	oracleGenesis := types.NewGenesisState(
		types.Params{
			VotePeriod:    votePeriod,
			VoteThreshold: voteThreshold,
			RewardBand:    rewardBand,
			Whitelist: types.DenomList{
				{Name: utils.MicroSeiDenom},
				{Name: utils.MicroAtomDenom},
			},
			SlashFraction:     slashFraction,
			SlashWindow:       slashWindow,
			MinValidPerWindow: minValidPerWindow,
		},
		[]types.ExchangeRateTuple{},
		[]types.FeederDelegation{},
		[]types.PenaltyCounter{},
		[]types.AggregateExchangeRatePrevote{},
		[]types.AggregateExchangeRateVote{},
	)

	bz, err := json.MarshalIndent(&oracleGenesis.Params, "", " ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Selected randomly generated oracle parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(oracleGenesis)
}
