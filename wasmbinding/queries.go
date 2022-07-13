package wasmbinding

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	dexbindings "github.com/sei-protocol/sei-chain/x/dex/client/wasm/bindings"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochbindings "github.com/sei-protocol/sei-chain/x/epoch/client/wasm/bindings"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebindings "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
)

type QueryPlugin struct {
	oracleHandler oraclewasm.OracleWasmQueryHandler
	dexHandler    dexwasm.DexWasmQueryHandler
	epochHandler  epochwasm.EpochWasmQueryHandler
}

// NewQueryPlugin returns a reference to a new QueryPlugin.
func NewQueryPlugin(oh *oraclewasm.OracleWasmQueryHandler, dh *dexwasm.DexWasmQueryHandler, eh *epochwasm.EpochWasmQueryHandler) *QueryPlugin {
	return &QueryPlugin{
		oracleHandler: *oh,
		dexHandler:    *dh,
		epochHandler:  *eh,
	}
}

func (qp QueryPlugin) HandleOracleQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery oraclebindings.SeiOracleQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, sdkerrors.Wrap(err, "Error parsing SeiOracleQuery")
	}
	switch {
	case parsedQuery.ExchangeRates != nil:
		res, err := qp.oracleHandler.GetExchangeRates(ctx)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error while getting Exchange Rates")
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error encoding exchange rates as JSON")
		}

		return bz, nil
	case parsedQuery.OracleTwaps != nil:
		res, err := qp.oracleHandler.GetOracleTwaps(ctx, parsedQuery.OracleTwaps)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error while getting Oracle Twaps")
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error encoding oracle twaps as JSON")
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Oracle Query"}
	}
}

func (qp QueryPlugin) HandleDexQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery dexbindings.SeiDexQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, sdkerrors.Wrap(err, "Error parsing SeiDexQuery")
	}
	switch {
	case parsedQuery.DexTwaps != nil:
		res, err := qp.dexHandler.GetDexTwap(ctx, parsedQuery.DexTwaps)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error while getting dex Twaps")
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error encoding dex twaps as JSON")
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Dex Query"}
	}
}

func (qp QueryPlugin) HandleEpochQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery epochbindings.SeiEpochQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, sdkerrors.Wrap(err, "Error parsing SeiEpochQuery")
	}
	switch {
	case parsedQuery.Epoch != nil:
		res, err := qp.epochHandler.GetEpoch(ctx, parsedQuery.Epoch)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error while getting epoch")
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "Error encoding epoch as JSON")
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Epoch Query"}
	}
}
