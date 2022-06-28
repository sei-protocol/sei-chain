package wasmbinding

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	OracleRoute = "oracle"
)

type SeiQueryWrapper struct {
	// specifies which module handler should handle the query
	Route string `json:"route,omitempty"`
	// The query data that should be parsed into the module query
	QueryData json.RawMessage `json:"query_data,omitempty"`
}

func CustomQuerier(qp *QueryPlugin) func(ctx sdk.Context, request json.RawMessage) ([]byte, error) {
	return func(ctx sdk.Context, request json.RawMessage) ([]byte, error) {
		var contractQuery SeiQueryWrapper
		// TODO: debugging, remove this
		ctx.Logger().Error("received a custom query")
		if err := json.Unmarshal(request, &contractQuery); err != nil {
			return nil, sdkerrors.Wrap(err, "sei query")
		}
		switch contractQuery.Route {
		case OracleRoute:
			// TODO: debugging, remove this
			ctx.Logger().Error("parse route: oracle query")
			bz, _ := json.Marshal(contractQuery)
			ctx.Logger().Error(string(bz))
			return qp.HandleOracleQuery(ctx, contractQuery.QueryData)
		default:
			return nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Query Route"}
		}
	}
}

// TODO: do we need this stuff from osmosis implementation? maybe remove
// ConvertSdkCoinsToWasmCoins converts sdk type coins to wasm vm type coins
func ConvertSdkCoinsToWasmCoins(coins []sdk.Coin) wasmvmtypes.Coins {
	var toSend wasmvmtypes.Coins
	for _, coin := range coins {
		c := ConvertSdkCoinToWasmCoin(coin)
		toSend = append(toSend, c)
	}
	return toSend
}

// ConvertSdkCoinToWasmCoin converts a sdk type coin to a wasm vm type coin
func ConvertSdkCoinToWasmCoin(coin sdk.Coin) wasmvmtypes.Coin {
	return wasmvmtypes.Coin{
		Denom: coin.Denom,
		// Note: gamm tokens have 18 decimal places, so 10^22 is common, no longer in u64 range
		Amount: coin.Amount.String(),
	}
}
