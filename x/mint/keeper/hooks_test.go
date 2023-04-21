package keeper_test

import (
	"context"
	"testing"
	"time"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func getGenesisTime() time.Time {
	return time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)
}

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
	seiApp := keepertest.TestApp()
	ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(seiApp.GetKey(types.StoreKey))))

	genesisTime := time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)

	tokenReleaseSchedle := []minttypes.ScheduledTokenRelease{
		{
			StartDate: genesisTime.AddDate(1, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			EndDate: genesisTime.AddDate(1, 60, 0).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 2500000,
		},
		{
			StartDate: genesisTime.AddDate(2, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			EndDate: genesisTime.AddDate(3, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 1250000,
		},
	}
	mintParams := minttypes.NewParams(
		"usei",
		tokenReleaseSchedle,
	)

	seiApp.MintKeeper.SetParams(ctx, mintParams)

	header := tmproto.Header{Height: seiApp.LastBlockHeight() + 1}
	seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})

	// Year 1
	currTime := genesisTime.AddDate(1, 0, 0)
	currEpoch := getEpoch(genesisTime, currTime)
	presupply := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)

	// Run hooks
	seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
	seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
	mintParams = seiApp.MintKeeper.GetParams(ctx)

	// Year 1
	mintedCoinYear1 := seiApp.MintKeeper.GetMinter(ctx).GetLastMintAmountCoin()
	postsupplyYear1 := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	require.True(t, postsupplyYear1.IsEqual(presupply.Add(mintedCoinYear1)))
	require.Equal(t, mintedCoinYear1.Amount.Int64(), int64(2500000))

	// Year 2
	currTime = currTime.AddDate(1, 0, 0)
	currEpoch = getEpoch(genesisTime, currTime)

	// Run hooks
	seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
	seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
	mintParams = seiApp.MintKeeper.GetParams(ctx)

	mintedCoinYear2 := seiApp.MintKeeper.GetMinter(ctx).GetLastMintAmountCoin()
	postsupplyYear2 := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	require.True(t, postsupplyYear2.IsEqual(postsupplyYear1.Add(mintedCoinYear2)))
	require.Equal(t, mintedCoinYear2.Amount.Int64(), int64(1250000))
}

func TestNoEpochPassedNoDistribution(t *testing.T) {
	seiApp := keepertest.TestApp()
	ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(seiApp.GetKey(types.StoreKey))))

	header := tmproto.Header{Height: seiApp.LastBlockHeight() + 1}
	seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})
	// Get mint params
	mintParams := seiApp.MintKeeper.GetParams(ctx)
	genesisTime := time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)
	presupply := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	startLastMintAmount := seiApp.MintKeeper.GetMinter(ctx).GetLastMintAmountCoin()
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
	endLastMintAmount := seiApp.MintKeeper.GetMinter(ctx).GetLastMintAmountCoin()
	require.True(t, startLastMintAmount.Equal(endLastMintAmount))
}
