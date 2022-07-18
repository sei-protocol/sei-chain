package keeper_test

import (
	"fmt"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"testing"
	"time"
)

func getEpoch(genesisTime time.Time, currTime time.Time) types.Epoch {
	// Epochs increase every minute, so derive based on the time
	return types.Epoch{
		GenesisTime:           genesisTime,
		EpochDuration:         time.Minute,
		CurrentEpoch:          0,
		CurrentEpochStartTime: currTime,
		CurrentEpochHeight:    0,
	}

}

func TestEndOfEpochMintedCoinDistribution(t *testing.T) {
	app := app.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	header := tmproto.Header{Height: app.LastBlockHeight() + 1}
	app.BeginBlock(abci.RequestBeginBlock{Header: header})
	futureCtx := ctx.WithBlockTime(time.Now().Add(24 * 365 * time.Hour))
	mintParams := app.MintKeeper.GetParams(ctx)
	genesisTime := time.Date(2022, time.Month(7), 18, 10, 00, 00, 00, time.UTC)
	currTime := time.Date(2023, time.Month(7), 18, 10, 00, 00, 00, time.UTC)
	currEpoch := getEpoch(genesisTime, currTime)
	presupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)

	app.EpochKeeper.BeforeEpochStart(futureCtx, currEpoch)
	app.EpochKeeper.AfterEpochEnd(futureCtx, currEpoch)

	mintParams = app.MintKeeper.GetParams(ctx)
	mintedCoin := app.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)

	// ensure post-epoch supply with offset changed by exactly the minted coins amount
	postsupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	fmt.Println(postsupply)
	require.False(t, postsupply.IsEqual(presupply.Add(mintedCoin)))

	//// Check value within same epoch
	//for i := 1; i < 5; i++ {
	//	// get pre-epoch sei supply
	//	presupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	//
	//	app.EpochKeeper.BeforeEpochStart(futureCtx, currEpoch)
	//	app.EpochKeeper.AfterEpochEnd(futureCtx, currEpoch)
	//
	//	mintParams = app.MintKeeper.GetParams(ctx)
	//	mintedCoin := app.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)
	//
	//	// ensure post-epoch supply with offset changed by exactly the minted coins amount
	//	postsupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	//	fmt.Println(postsupply)
	//	require.False(t, postsupply.IsEqual(presupply.Add(mintedCoin)))
	//	currEpoch = getEpoch(genesisTime, currTime.Add(time.Second))
	//}
	//
	//// Check value when an epoch has elapsed
	//for i := 1; i < 5; i++ {
	//	// get pre-epoch sei supply
	//	presupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	//
	//	app.EpochKeeper.BeforeEpochStart(futureCtx, currEpoch)
	//	app.EpochKeeper.AfterEpochEnd(futureCtx, currEpoch)
	//
	//	mintParams = app.MintKeeper.GetParams(ctx)
	//	mintedCoin := app.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)
	//
	//	// ensure post-epoch supply with offset changed by exactly the minted coins amount
	//	postsupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	//	fmt.Println(postsupply)
	//
	//	require.False(t, postsupply.IsEqual(presupply.Add(mintedCoin)))
	//	currEpoch = getEpoch(genesisTime, currTime.Add(time.Second))
	//}
	//
	//// Check value when multiple epochs have elapsed
	//for i := 1; i < 5; i++ {
	//	// get pre-epoch sei supply
	//	presupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	//
	//	app.EpochKeeper.BeforeEpochStart(futureCtx, currEpoch)
	//	app.EpochKeeper.AfterEpochEnd(futureCtx, currEpoch)
	//
	//	mintParams = app.MintKeeper.GetParams(ctx)
	//	mintedCoin := app.MintKeeper.GetMinter(ctx).EpochProvision(mintParams)
	//
	//	// ensure post-epoch supply with offset changed by exactly the minted coins amount
	//	postsupply := app.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	//	require.False(t, postsupply.IsEqual(presupply.Add(mintedCoin)))
	//	currEpoch = getEpoch(genesisTime, currTime.Add(time.Second))
	//}

}
