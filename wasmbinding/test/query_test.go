package wasmbinding

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	dexbinding "github.com/sei-protocol/sei-chain/x/dex/client/wasm/bindings"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebinding "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	oracleutils "github.com/sei-protocol/sei-chain/x/oracle/utils"
	"github.com/stretchr/testify/require"
)

func TestWasmGetOracleExchangeRates(t *testing.T) {
	// START SETUP
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	oh := oraclewasm.NewOracleWasmQueryHandler(&testWrapper.App.OracleKeeper)
	dh := dexwasm.NewDexWasmQueryHandler(&testWrapper.App.DexKeeper)
	qp := wasmbinding.NewQueryPlugin(oh, dh)
	customQuerier := wasmbinding.CustomQuerier(qp)
	// END SETUP

	req := oraclebinding.SeiOracleQuery{ExchangeRates: &oracletypes.QueryExchangeRatesRequest{}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes oracletypes.QueryExchangeRatesResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Equal(t, oracletypes.QueryExchangeRatesResponse{DenomOracleExchangeRatePairs: oracletypes.DenomOracleExchangeRatePairs{}}, parsedRes)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11)
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))

	res, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 oracletypes.QueryExchangeRatesResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	require.Equal(t, oracletypes.QueryExchangeRatesResponse{DenomOracleExchangeRatePairs: oracletypes.DenomOracleExchangeRatePairs{oracletypes.NewDenomOracleExchangeRatePair(oracleutils.MicroAtomDenom, sdk.NewDec(12), sdk.NewInt(11))}}, parsedRes2)
}

func TestWasmGetOracleTwaps(t *testing.T) {
	// START SETUP
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	oh := oraclewasm.NewOracleWasmQueryHandler(&testWrapper.App.OracleKeeper)
	dh := dexwasm.NewDexWasmQueryHandler(&testWrapper.App.DexKeeper)
	qp := wasmbinding.NewQueryPlugin(oh, dh)
	customQuerier := wasmbinding.CustomQuerier(qp)
	// END SETUP

	req := oraclebinding.SeiOracleQuery{OracleTwaps: &oracletypes.QueryTwapsRequest{LookbackSeconds: 200}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	// this should error because there is no snapshots to build twap from
	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))

	priceSnapshot := oracletypes.PriceSnapshot{SnapshotTimestamp: 3600, PriceSnapshotItems: oracletypes.PriceSnapshotItems{
		oracletypes.NewPriceSnapshotItem(oracleutils.MicroAtomDenom, oracletypes.OracleExchangeRate{ExchangeRate: sdk.NewDec(20), LastUpdate: sdk.NewInt(10)}),
	}}
	testWrapper.App.OracleKeeper.AddPriceSnapshot(testWrapper.Ctx, priceSnapshot)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(14).WithBlockTime(time.Unix(3700, 0))

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 oracletypes.QueryTwapsResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	// should be 100 isntead of 200 because thats the oldest data timestamp we have
	require.Equal(t, oracletypes.QueryTwapsResponse{OracleTwaps: oracletypes.OracleTwaps{
		oracletypes.OracleTwap{Denom: oracleutils.MicroAtomDenom, Twap: sdk.NewDec(20), LookbackSeconds: 100},
	}}, parsedRes2)
}

func TestWasmGetDexTwaps(t *testing.T) {
	// START SETUP
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	oh := oraclewasm.NewOracleWasmQueryHandler(&testWrapper.App.OracleKeeper)
	dh := dexwasm.NewDexWasmQueryHandler(&testWrapper.App.DexKeeper)
	qp := wasmbinding.NewQueryPlugin(oh, dh)
	customQuerier := wasmbinding.CustomQuerier(qp)
	// END SETUP

	req := dexbinding.SeiDexQuery{DexTwaps: &dextypes.QueryGetTwapsRequest{
		ContractAddr:    app.TEST_CONTRACT,
		LookbackSeconds: 200,
	}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.DexRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	testWrapper.App.DexKeeper.AddRegisteredPair(
		testWrapper.Ctx,
		app.TEST_CONTRACT,
		dextypes.Pair{PriceDenom: "sei", AssetDenom: "atom"},
	)
	testWrapper.App.DexKeeper.SetPriceState(testWrapper.Ctx, dextypes.Price{
		SnapshotTimestampInSeconds: 3600,
		Price:                      sdk.NewDec(20),
		Pair:                       &dextypes.Pair{PriceDenom: "sei", AssetDenom: "atom"},
	}, app.TEST_CONTRACT, 0)
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(14).WithBlockTime(time.Unix(3700, 0))

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes dextypes.QueryGetTwapsResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Equal(t, 1, len(parsedRes.Twaps))
	twap := *parsedRes.Twaps[0]
	require.Equal(t, "sei", twap.Pair.PriceDenom)
	require.Equal(t, "atom", twap.Pair.AssetDenom)
	require.Equal(t, sdk.NewDec(20), twap.Twap)
}
