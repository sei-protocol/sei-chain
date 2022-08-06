package wasmbinding

import (
	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochkeeper "github.com/sei-protocol/sei-chain/x/epoch/keeper"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
)

func RegisterCustomPlugins(
	oracle *oraclekeeper.Keeper,
	dex *dexkeeper.Keeper,
	epoch *epochkeeper.Keeper,
) []wasmkeeper.Option {
	dexHandler := dexwasm.NewDexWasmQueryHandler(dex)
	oracleHandler := oraclewasm.NewOracleWasmQueryHandler(oracle)
	epochHandler := epochwasm.NewEpochWasmQueryHandler(epoch)
	wasmQueryPlugin := NewQueryPlugin(oracleHandler, dexHandler, epochHandler)

	queryPluginOpt := wasmkeeper.WithQueryPlugins(&wasmkeeper.QueryPlugins{
		Custom: CustomQuerier(wasmQueryPlugin),
	})

	return []wasm.Option{
		queryPluginOpt,
	}
}
