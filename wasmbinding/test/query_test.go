package wasmbinding

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochbinding "github.com/sei-protocol/sei-chain/x/epoch/client/wasm/bindings"
	epochtypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmwasm "github.com/sei-protocol/sei-chain/x/evm/client/wasm"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebinding "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorybinding "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm/bindings"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/stretchr/testify/require"
)

func SetupWasmbindingTest(t *testing.T) (*app.TestWrapper, func(ctx sdk.Context, request json.RawMessage) ([]byte, error)) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)

	oh := oraclewasm.NewOracleWasmQueryHandler(&testWrapper.App.OracleKeeper)
	eh := epochwasm.NewEpochWasmQueryHandler(&testWrapper.App.EpochKeeper)
	th := tokenfactorywasm.NewTokenFactoryWasmQueryHandler(&testWrapper.App.TokenFactoryKeeper)
	evmh := evmwasm.NewEVMQueryHandler(&testWrapper.App.EvmKeeper)
	qp := wasmbinding.NewQueryPlugin(oh, eh, th, evmh, testWrapper.App.StakingKeeper)
	return testWrapper, wasmbinding.CustomQuerier(qp)
}

func TestWasmUnknownQuery(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	oracle_req := oraclebinding.SeiOracleQuery{}
	queryData, err := json.Marshal(oracle_req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}
	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, oracletypes.ErrUnknownSeiOracleQuery)

	epoch_req := epochbinding.SeiEpochQuery{}
	queryData, err = json.Marshal(epoch_req)
	require.NoError(t, err)
	query = wasmbinding.SeiQueryWrapper{Route: wasmbinding.EpochRoute, QueryData: queryData}
	rawQuery, err = json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.Error(t, err)
	require.Equal(t, err, epochtypes.ErrUnknownSeiEpochQuery)
}

func TestWasmGetOracleExchangeRates(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := oraclebinding.SeiOracleQuery{ExchangeRates: &oracletypes.QueryExchangeRatesRequest{}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.ErrorIs(t, err, oracletypes.ErrOracleDeprecated)
}

func TestWasmGetOracleTwaps(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := oraclebinding.SeiOracleQuery{OracleTwaps: &oracletypes.QueryTwapsRequest{LookbackSeconds: 200}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.OracleRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	_, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.ErrorIs(t, err, oracletypes.ErrOracleDeprecated)
}

func TestWasmGetEpoch(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	req := epochbinding.SeiEpochQuery{
		Epoch: &epochtypes.QueryEpochRequest{},
	}

	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.EpochRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(45).WithBlockTime(time.Unix(12500, 0))
	testWrapper.App.EpochKeeper.SetEpoch(testWrapper.Ctx, epochtypes.Epoch{
		GenesisTime:           time.Unix(1000, 0).UTC(),
		EpochDuration:         time.Minute,
		CurrentEpoch:          uint64(69),
		CurrentEpochStartTime: time.Unix(12345, 0).UTC(),
		CurrentEpochHeight:    int64(40),
	})

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes epochtypes.QueryEpochResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	epoch := parsedRes.Epoch
	require.Equal(t, time.Unix(1000, 0).UTC(), epoch.GenesisTime)
	require.Equal(t, time.Minute, epoch.EpochDuration)
	require.Equal(t, uint64(69), epoch.CurrentEpoch)
	require.Equal(t, time.Unix(12345, 0).UTC(), epoch.CurrentEpochStartTime)
	require.Equal(t, int64(40), epoch.CurrentEpochHeight)
}

func TestWasmGetDenomAuthorityMetadata(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	denom := fmt.Sprintf("factory/%s/test", app.TestUser)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))
	// Create denom
	testWrapper.App.TokenFactoryKeeper.CreateDenom(testWrapper.Ctx, app.TestUser, "test")
	authorityMetadata := tokenfactorytypes.DenomAuthorityMetadata{
		Admin: app.TestUser,
	}

	// Setup tfk query
	req := tokenfactorybinding.SeiTokenFactoryQuery{DenomAuthorityMetadata: &tokenfactorytypes.QueryDenomAuthorityMetadataRequest{Denom: denom}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.TokenFactoryRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes tokenfactorytypes.QueryDenomAuthorityMetadataResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Equal(t, tokenfactorytypes.QueryDenomAuthorityMetadataResponse{AuthorityMetadata: authorityMetadata}, parsedRes)
}

func TestWasmGetDenomsFromCreator(t *testing.T) {
	testWrapper, customQuerier := SetupWasmbindingTest(t)

	denom1 := fmt.Sprintf("factory/%s/test1", app.TestUser)
	denom2 := fmt.Sprintf("factory/%s/test2", app.TestUser)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(11).WithBlockTime(time.Unix(3600, 0))

	// No denoms created initially
	req := tokenfactorybinding.SeiTokenFactoryQuery{DenomsFromCreator: &tokenfactorytypes.QueryDenomsFromCreatorRequest{Creator: app.TestUser}}
	queryData, err := json.Marshal(req)
	require.NoError(t, err)
	query := wasmbinding.SeiQueryWrapper{Route: wasmbinding.TokenFactoryRoute, QueryData: queryData}

	rawQuery, err := json.Marshal(query)
	require.NoError(t, err)

	res, err := customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes tokenfactorytypes.QueryDenomsFromCreatorResponse
	err = json.Unmarshal(res, &parsedRes)
	require.NoError(t, err)
	require.Empty(t, parsedRes.Denoms)

	// Add first denom
	testWrapper.App.TokenFactoryKeeper.CreateDenom(testWrapper.Ctx, app.TestUser, "test1")

	res, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes2 tokenfactorytypes.QueryDenomsFromCreatorResponse
	err = json.Unmarshal(res, &parsedRes2)
	require.NoError(t, err)
	require.Equal(t, []string{denom1}, parsedRes2.Denoms)

	// Add second denom
	testWrapper.App.TokenFactoryKeeper.CreateDenom(testWrapper.Ctx, app.TestUser, "test2")

	res, err = customQuerier(testWrapper.Ctx, rawQuery)
	require.NoError(t, err)

	var parsedRes3 tokenfactorytypes.QueryDenomsFromCreatorResponse
	err = json.Unmarshal(res, &parsedRes3)
	require.NoError(t, err)
	require.Equal(t, []string{denom1, denom2}, parsedRes3.Denoms)

}

func MockQueryPlugins() wasmkeeper.QueryPlugins {
	return wasmkeeper.QueryPlugins{
		Bank: func(ctx sdk.Context, request *wasmvmtypes.BankQuery) ([]byte, error) { return []byte{}, nil },
		IBC: func(ctx sdk.Context, caller sdk.AccAddress, request *wasmvmtypes.IBCQuery) ([]byte, error) {
			return []byte{}, nil
		},
		Custom: func(ctx sdk.Context, request json.RawMessage) ([]byte, error) {
			return []byte{}, nil
		},
		Stargate: func(ctx sdk.Context, request *wasmvmtypes.StargateQuery) ([]byte, error) { return []byte{}, nil },
		Staking:  func(ctx sdk.Context, request *wasmvmtypes.StakingQuery) ([]byte, error) { return []byte{}, nil },
		Wasm:     func(ctx sdk.Context, request *wasmvmtypes.WasmQuery) ([]byte, error) { return []byte{}, nil },
	}
}
