package wasmbinding

import (
	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochkeeper "github.com/sei-protocol/sei-chain/x/epoch/keeper"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorykeeper "github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
)

func RegisterCustomPlugins(
	oracle *oraclekeeper.Keeper,
	dex *dexkeeper.Keeper,
	epoch *epochkeeper.Keeper,
	tokenfactory *tokenfactorykeeper.Keeper,
	accountKeeper *authkeeper.AccountKeeper,
	router wasmkeeper.MessageRouter,
) []wasmkeeper.Option {
	dexHandler := dexwasm.NewDexWasmQueryHandler(dex)
	oracleHandler := oraclewasm.NewOracleWasmQueryHandler(oracle)
	epochHandler := epochwasm.NewEpochWasmQueryHandler(epoch)
	tokenfactoryHandler := tokenfactorywasm.NewTokenFactoryWasmQueryHandler(tokenfactory)
	wasmQueryPlugin := NewQueryPlugin(oracleHandler, dexHandler, epochHandler, tokenfactoryHandler)

	queryPluginOpt := wasmkeeper.WithQueryPlugins(&wasmkeeper.QueryPlugins{
		Custom: CustomQuerier(wasmQueryPlugin),
	})
	messengerDecoratorOpt := wasmkeeper.WithMessageHandlerDecorator(
		CustomMessageDecorator(router, accountKeeper),
	)

	return []wasm.Option{
		queryPluginOpt,
		messengerDecoratorOpt,
	}
}
