package wasmbinding

import (
	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
)

func RegisterCustomPlugins(
	oracle *oraclekeeper.Keeper,
) []wasmkeeper.Option {
	oracleHandler := oraclewasm.NewOracleWasmQueryHandler(oracle)
	wasmQueryPlugin := NewQueryPlugin(oracleHandler)

	queryPluginOpt := wasmkeeper.WithQueryPlugins(&wasmkeeper.QueryPlugins{
		Custom: CustomQuerier(wasmQueryPlugin),
	})
	// messengerDecoratorOpt := wasmkeeper.WithMessageHandlerDecorator(
	// 	CustomMessageDecorator(gammKeeper, bank, tokenFactory),
	// )

	return []wasm.Option{
		queryPluginOpt,
		// messengerDecoratorOpt,
	}
}
