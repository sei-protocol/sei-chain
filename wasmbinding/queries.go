package wasmbinding

import (
	"encoding/json"

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
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, oracletypes.ErrEncodingExchangeRates
		}

		return bz, nil
	case parsedQuery.OracleTwaps != nil:
		res, err := qp.oracleHandler.GetOracleTwaps(ctx, parsedQuery.OracleTwaps)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, oracletypes.ErrEncodingOracleTwaps
		}

		return bz, nil
	default:
		return nil, oracletypes.ErrUnknownSeiOracleQuery
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
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingDexTwaps
		}

		return bz, nil
	case parsedQuery.GetOrders != nil:
		res, err := qp.dexHandler.GetOrders(ctx, parsedQuery.GetOrders)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingOrders
		}

		return bz, nil
	case parsedQuery.GetOrderByID != nil:
		res, err := qp.dexHandler.GetOrderByID(ctx, parsedQuery.GetOrderByID)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingOrder
		}

		return bz, nil
	case parsedQuery.GetOrderSimulation != nil:
		res, err := qp.dexHandler.GetOrderSimulation(ctx, parsedQuery.GetOrderSimulation)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingOrderSimulation
		}

		return bz, nil
	case parsedQuery.GetLatestPrice != nil:
		res, err := qp.dexHandler.GetLatestPrice(ctx, parsedQuery.GetLatestPrice)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, dextypes.ErrEncodingLatestPrice
		}

		return bz, nil
	default:
		return nil, dextypes.ErrUnknownSeiDexQuery
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
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, epochtypes.ErrEncodingEpoch
		}

		return bz, nil
	default:
		return nil, epochtypes.ErrUnknownSeiEpochQuery
	}
}

func (qp QueryPlugin) HandleTokenFactoryQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery tokenfactorybindings.SeiTokenFactoryQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, tokenfactorytypes.ErrParsingSeiTokenFactoryQuery
	}
	switch {
	case parsedQuery.DenomAuthorityMetadata != nil:
		res, err := qp.tokenfactoryHandler.GetDenomAuthorityMetadata(ctx, parsedQuery.DenomAuthorityMetadata)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, tokenfactorytypes.ErrEncodingDenomAuthorityMetadata
		}

		return bz, nil
	case parsedQuery.DenomsFromCreator != nil:
		res, err := qp.tokenfactoryHandler.GetDenomsFromCreator(ctx, parsedQuery.DenomsFromCreator)
		if err != nil {
			return nil, err
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, tokenfactorytypes.ErrEncodingDenomsFromCreator
		}

		return bz, nil
	default:
		return nil, tokenfactorytypes.ErrUnknownSeiTokenFactoryQuery
	}
}
