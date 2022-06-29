package wasmbinding

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebinding "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	oracleutils "github.com/sei-protocol/sei-chain/x/oracle/utils"
	"github.com/stretchr/testify/require"
)

func TestWasmGetOracleExchangeRates(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	req := oraclebinding.SeiOracleQuery{ExchangeRates: &oraclebinding.WasmQueryExchangeRatesRequest{}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: "oracle", QueryData: queryData}

	oh := oraclewasm.NewOracleWasmQueryHandler(&testWrapper.App.OracleKeeper)
	qp := wasmbinding.NewQueryPlugin(oh)
	customQuerier := wasmbinding.CustomQuerier(qp)

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes oraclebinding.WasmQueryExchangeRatesResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Equal(t, oraclebinding.WasmQueryExchangeRatesResponse{DenomOracleExchangeRatePairs: oracletypes.DenomOracleExchangeRatePairs(nil)}, parsedRes)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11)
	testWrapper.App.OracleKeeper.SetBaseExchangeRate(testWrapper.Ctx, oracleutils.MicroAtomDenom, sdk.NewDec(12))

	res, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 oraclebinding.WasmQueryExchangeRatesResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	require.Equal(t, oraclebinding.WasmQueryExchangeRatesResponse{DenomOracleExchangeRatePairs: oracletypes.DenomOracleExchangeRatePairs{oracletypes.NewDenomOracleExchangeRatePair(oracleutils.MicroAtomDenom, sdk.NewDec(12), sdk.NewInt(11))}}, parsedRes2)
}
