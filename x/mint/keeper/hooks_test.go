package keeper_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func getEpoch(genesisTime time.Time, currTime time.Time) types.Epoch {
	// Epochs increase every minute, so derive based on the time
	return types.Epoch{
		GenesisTime:           genesisTime,
		EpochDuration:         time.Minute,
		CurrentEpoch:          uint64(currTime.Sub(genesisTime).Minutes()),
		CurrentEpochStartTime: currTime,
		CurrentEpochHeight:    0,
	}
}

func TestEndOfEpochMintedCoinDistribution(t *testing.T) {
	seiApp := app.Setup(false)
	ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{})
	header := tmproto.Header{Height: seiApp.LastBlockHeight() + 1}
	seiApp.BeginBlock(abci.RequestBeginBlock{Header: header})
	// Get mint params
	mintParams := seiApp.MintKeeper.GetParams(ctx)
	genesisEpochProvisions := mintParams.GenesisEpochProvisions
	reductionFactor := mintParams.ReductionFactor
	genesisTime := time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)

	// Year 1
	currTime := genesisTime.Add(60 * 24 * 365 * time.Minute)
	currEpoch := getEpoch(genesisTime, currTime)
	presupply := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)

	// Run hooks
	seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
	seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)

	mintParams = seiApp.MintKeeper.GetParams(ctx)
	mintedCoinYear1 := seiApp.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)
	// ensure post-epoch supply changed by exactly the minted coins amount
	postsupplyYear1 := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	require.True(t, postsupplyYear1.IsEqual(presupply.Add(mintedCoinYear1)))
	// ensure that the minted amount is genesisEpochProvisions * reductionFactor
	expectedMintedYear1, err := genesisEpochProvisions.Mul(reductionFactor).Float64()
	if err != nil {
		panic(err)
	}
	require.True(t, mintedCoinYear1.Amount.Int64() == int64(expectedMintedYear1))

	// Year 2
	currTime = currTime.Add(60 * 24 * 7 * 365 * time.Minute)
	currEpoch = getEpoch(genesisTime, currTime)

	// Run hooks
	seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
	seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)

	mintedCoinYear2 := seiApp.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)
	// ensure post-epoch supply changed by exactly the minted coins amount
	postsupplyYear2 := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	require.True(t, postsupplyYear2.IsEqual(postsupplyYear1.Add(mintedCoinYear2)))
	// ensure that the minted amount is mintedCoinYear1 * reductionFactor
	expectedMintedYear2, err := mintedCoinYear1.Amount.ToDec().Mul(reductionFactor).Float64()
	if err != nil {
		panic(err)
	}
	require.True(t, mintedCoinYear2.Amount.Int64() == int64(expectedMintedYear2))
}

func TestNoEpochPassedNoDistribution(t *testing.T) {
	seiApp := app.Setup(false)
	ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{})
	header := tmproto.Header{Height: seiApp.LastBlockHeight() + 1}
	seiApp.BeginBlock(abci.RequestBeginBlock{Header: header})
	// Get mint params
	mintParams := seiApp.MintKeeper.GetParams(ctx)
	genesisTime := time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)
	presupply := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	epochProvisions := seiApp.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)
	// Loops through epochs under a year
	for i := 0; i < 60*24*7*52-1; i++ {
		currTime := genesisTime.Add(time.Minute)
		currEpoch := getEpoch(genesisTime, currTime)
		// Run hooks
		seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
		seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
		// Verify supply is the same and no coins have been minted
		currSupply := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
		require.True(t, currSupply.IsEqual(presupply))
	}
	// Ensure that EpochProvision hasn't changed
	endEpochProvisions := seiApp.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)
	require.True(t, epochProvisions.Equal(endEpochProvisions))
}
