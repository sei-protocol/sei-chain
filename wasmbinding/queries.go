package wasmbinding

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclebindings "github.com/sei-protocol/sei-chain/x/oracle/client/wasm/bindings"
)

type QueryPlugin struct {
	oracleHandler oraclewasm.OracleWasmQueryHandler
}

// NewQueryPlugin returns a reference to a new QueryPlugin.
func NewQueryPlugin(oh *oraclewasm.OracleWasmQueryHandler) *QueryPlugin {
	return &QueryPlugin{
		oracleHandler: *oh,
	}
}

func (qp QueryPlugin) HandleOracleQuery(ctx sdk.Context, queryData json.RawMessage) ([]byte, error) {
	var parsedQuery oraclebindings.SeiOracleQuery
	if err := json.Unmarshal(queryData, &parsedQuery); err != nil {
		return nil, sdkerrors.Wrap(err, "sei oracle query")
	}
	switch {
	case parsedQuery.ExchangeRates != nil:
		res, err := qp.oracleHandler.GetExchangeRates(ctx)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "sei oracle GetExchangeRates query")
		}
		bz, err := json.Marshal(res)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "sei oracle GetExchangeRates response")
		}

		return bz, nil
	default:
		return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Oracle Query"}
	}
}
