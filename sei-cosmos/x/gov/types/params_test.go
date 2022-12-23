package types_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/stretchr/testify/require"
)

func TestTallyParamsGetThreshold(t *testing.T) {
	testcases := []struct {
		name                   string
		tallyParams            types.TallyParams
		expectedQuorumValue    sdk.Dec
		expectedThresholdValue sdk.Dec
		isExpedited            bool
	}{
		{
			name:                   "default expedited",
			tallyParams:            types.DefaultTallyParams(),
			expectedQuorumValue:    sdk.NewDecWithPrec(667, 3),
			expectedThresholdValue: sdk.NewDecWithPrec(667, 3),
			isExpedited:            true,
		},
		{
			name:                   "default not expedited",
			tallyParams:            types.DefaultTallyParams(),
			expectedQuorumValue:    sdk.NewDecWithPrec(334, 3),
			expectedThresholdValue: sdk.NewDecWithPrec(5, 1),
			isExpedited:            false,
		},
		{
			name:                   "custom expedited",
			tallyParams:            types.NewTallyParams(types.DefaultQuorum, sdk.NewDecWithPrec(877, 3), types.DefaultThreshold, sdk.NewDecWithPrec(777, 3), types.DefaultVetoThreshold),
			expectedQuorumValue:    sdk.NewDecWithPrec(877, 3),
			expectedThresholdValue: sdk.NewDecWithPrec(777, 3),
			isExpedited:            true,
		},
		{
			name:                   "default not expedited",
			tallyParams:            types.NewTallyParams(sdk.NewDecWithPrec(5, 1), types.DefaultExpeditedQuorum, sdk.NewDecWithPrec(6, 1), types.DefaultExpeditedThreshold, types.DefaultVetoThreshold),
			expectedQuorumValue:    sdk.NewDecWithPrec(5, 1),
			expectedThresholdValue: sdk.NewDecWithPrec(6, 1),
			isExpedited:            false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedQuorumValue, tc.tallyParams.GetQuorum(tc.isExpedited))
			require.Equal(t, tc.expectedThresholdValue, tc.tallyParams.GetThreshold(tc.isExpedited))
		})
	}
}

func TestVotingParamsGetVotingTime(t *testing.T) {
	testcases := []struct {
		name          string
		votingParams  types.VotingParams
		expectedValue time.Duration
		isExpedited   bool
	}{
		{
			name:          "default expedited",
			votingParams:  types.DefaultVotingParams(),
			expectedValue: types.DefaultExpeditedPeriod,
			isExpedited:   true,
		},
		{
			name:          "default not expedited",
			votingParams:  types.DefaultVotingParams(),
			expectedValue: types.DefaultPeriod,
			isExpedited:   false,
		},
		{
			name:          "custom expedited",
			votingParams:  types.NewVotingParams(types.DefaultPeriod, time.Hour),
			expectedValue: time.Hour,
			isExpedited:   true,
		},
		{
			name:          "default not expedited",
			votingParams:  types.NewVotingParams(time.Hour, types.DefaultExpeditedPeriod),
			expectedValue: time.Hour,
			isExpedited:   false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedValue, tc.votingParams.GetVotingPeriod(tc.isExpedited), tc.name)
		})
	}
}

func TestDepositParamsGetCoins(t *testing.T) {
	testcases := []struct {
		name          string
		depositParams types.DepositParams
		expectedValue sdk.Coins
		isExpedited   bool
	}{
		{
			name:          "default expedited",
			depositParams: types.DefaultDepositParams(),
			expectedValue: sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, types.DefaultMinExpeditedDepositTokens)),
			isExpedited:   true,
		},
		{
			name:          "default not expedited",
			depositParams: types.DefaultDepositParams(),
			expectedValue: sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, types.DefaultMinDepositTokens)),
			isExpedited:   false,
		},
		{
			name: "custom expedited",
			depositParams: types.NewDepositParams(
				sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1500000))),
				sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1600000))),
				types.DefaultPeriod),
			expectedValue: sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1600000))),
			isExpedited:   true,
		},
		{
			name: "default not expedited",
			depositParams: types.NewDepositParams(
				sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1500000))),
				sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1600000))),
				types.DefaultPeriod),
			expectedValue: sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(1500000))),
			isExpedited:   false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedValue, tc.depositParams.GetMinimumDeposit(tc.isExpedited), tc.name)
		})
	}
}
