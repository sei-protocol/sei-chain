package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestParamsEqual(t *testing.T) {
	p1 := DefaultParams()
	err := p1.Validate()
	require.NoError(t, err)

	// minus vote period
	p1.VotePeriod = 0
	err = p1.Validate()
	require.Error(t, err)

	// small vote threshold
	p2 := DefaultParams()
	p2.VoteThreshold = sdk.ZeroDec()
	err = p2.Validate()
	require.Error(t, err)

	// negative reward band
	p3 := DefaultParams()
	p3.RewardBand = sdk.NewDecWithPrec(-1, 2)
	err = p3.Validate()
	require.Error(t, err)

	// negative slash fraction
	p4 := DefaultParams()
	p4.SlashFraction = sdk.NewDec(-1)
	err = p4.Validate()
	require.Error(t, err)

	// negative min valid per window
	p5 := DefaultParams()
	p5.MinValidPerWindow = sdk.NewDec(-1)
	err = p5.Validate()
	require.Error(t, err)

	// small slash window
	p6 := DefaultParams()
	p6.SlashWindow = 0
	err = p6.Validate()
	require.Error(t, err)

	// small distribution window
	p7 := DefaultParams()
	p7.RewardDistributionWindow = 0
	err = p7.Validate()
	require.Error(t, err)

	// non-positive tobin tax
	p8 := DefaultParams()
	p8.Whitelist[0].TobinTax = sdk.NewDec(-1)
	err = p8.Validate()
	require.Error(t, err)

	// empty name
	p9 := DefaultParams()
	p9.Whitelist[0].Name = ""
	err = p9.Validate()
	require.Error(t, err)

	p10 := DefaultParams()
	require.NotNil(t, p10.ParamSetPairs())
	require.NotNil(t, p10.String())
}
