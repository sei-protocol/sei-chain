package wasmbinding

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	dexbindings "github.com/sei-protocol/sei-chain/x/dex/client/wasm/bindings"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochbindings "github.com/sei-protocol/sei-chain/x/epoch/client/wasm/bindings"
	epochtypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebindings "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorybindings "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm/bindings"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

type QueryPlugin struct {
	oracleHandler       oraclewasm.OracleWasmQueryHandler
	dexHandler          dexwasm.DexWasmQueryHandler
	epochHandler        epochwasm.EpochWasmQueryHandler
	tokenfactoryHandler tokenfactorywasm.TokenFactoryWasmQueryHandler
}

// NewQueryPlugin returns a reference to a new QueryPlugin.
func NewQueryPlugin(oh *oraclewasm.OracleWasmQueryHandler, dh *dexwasm.DexWasmQueryHandler, eh *epochwasm.EpochWasmQueryHandler, th *tokenfactorywasm.TokenFactoryWasmQueryHandler) *QueryPlugin {
	return &QueryPlugin{
		oracleHandler:       *oh,
		dexHandler:          *dh,
		epochHandler:        *eh,
		tokenfactoryHandler: *th,
	}
}

func (qp QueryPlugin) HandleOracleQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery oraclebindings.SeiOracleQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, oracletypes.ErrParsingOracleQuery
	}
	switch {
	case parsedQuery.ExchangeRates != nil:
		res, err := qp.oracleHandler.GetExchangeRates(ctx)
		if err != nil {
			return nil, oracletypes.ErrGettingExchangeRates
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, oracletypes.ErrEncodingExchangeRates
		}

		return bz, nil
	case parsedQuery.OracleTwaps != nil:
		res, err := qp.oracleHandler.GetOracleTwaps(ctx, parsedQuery.OracleTwaps)
		if err != nil {
			return nil, oracletypes.ErrGettingOralceTwaps
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, oracletypes.ErrEncodingOralceTwaps
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Oracle Query"}
	}
}

func (qp QueryPlugin) HandleDexQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery dexbindings.SeiDexQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, dextypes.ErrParsingSeiDexQuery
	}
	switch {
	case parsedQuery.DexTwaps != nil:
		res, err := qp.dexHandler.GetDexTwap(ctx, parsedQuery.DexTwaps)
		if err != nil {
			return nil, dextypes.ErrGettingDexTwaps
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingDexTwaps
		}

		return bz, nil
	case parsedQuery.GetOrders != nil:
		res, err := qp.dexHandler.GetOrders(ctx, parsedQuery.GetOrders)
		if err != nil {
			return nil, dextypes.ErrGettingOrders
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingOrders
		}

		return bz, nil
	case parsedQuery.GetOrderByID != nil:
		res, err := qp.dexHandler.GetOrderByID(ctx, parsedQuery.GetOrderByID)
		if err != nil {
			return nil, dextypes.ErrGettingOrderByID
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingOrder
		}

		return bz, nil
	case parsedQuery.GetOrderSimulation != nil:
		res, err := qp.dexHandler.GetOrderSimulation(ctx, parsedQuery.GetOrderSimulation)
		if err != nil {
			return nil, dextypes.ErrEncodingOrderSimulation
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingOrderSimulation
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Dex Query"}
	}
}

func (qp QueryPlugin) HandleEpochQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery epochbindings.SeiEpochQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, epochtypes.ErrParsingSeiEpochQuery
	}
	switch {
	case parsedQuery.Epoch != nil:
		res, err := qp.epochHandler.GetEpoch(ctx, parsedQuery.Epoch)
		if err != nil {
			return nil, epochtypes.ErrGettingEpoch
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, epochtypes.ErrEncodingEpoch
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Epoch Query"}
	}
}

func (qp QueryPlugin) HandleTokenFactoryQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery tokenfactorybindings.SeiTokenFactoryQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, tokenfactorytypes.ErrParsingSeiTokenFactoryQuery
	}
	switch {
	case parsedQuery.CreatorInDenomFeeWhitelist != nil:
		res, err := qp.tokenfactoryHandler.GetCreatorInDenomFeeWhitelist(ctx, parsedQuery.CreatorInDenomFeeWhitelist)
		if err != nil {
			return nil, tokenfactorytypes.ErrQueryingCreatorInDenomFeeWhitelist
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, tokenfactorytypes.ErrEncodingDenomFeeWhitelist
		}

		return bz, nil
	case parsedQuery.GetDenomFeeWhitelist != nil:
		res, err := qp.tokenfactoryHandler.GetDenomCreationFeeWhitelist(ctx)
		if err != nil {
			return nil, tokenfactorytypes.ErrGettingDenomCreationFeeWhitelist
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, tokenfactorytypes.ErrEncodingDenomCreationFeeWhitelist
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei TokenFactory Query"}
	}
}
