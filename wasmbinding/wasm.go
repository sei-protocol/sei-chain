package wasmbinding

import (
	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
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
	_ *authkeeper.AccountKeeper,
	router wasmkeeper.MessageRouter,
	channelKeeper wasmtypes.ChannelKeeper,
	capabilityKeeper wasmtypes.CapabilityKeeper,
	bankKeeper wasmtypes.Burner,
	unpacker codectypes.AnyUnpacker,
	portSource wasmtypes.ICS20TransferPortSource,
	aclKeeper aclkeeper.Keeper,
) []wasmkeeper.Option {
	dexHandler := dexwasm.NewDexWasmQueryHandler(dex)
	oracleHandler := oraclewasm.NewOracleWasmQueryHandler(oracle)
	epochHandler := epochwasm.NewEpochWasmQueryHandler(epoch)
	tokenfactoryHandler := tokenfactorywasm.NewTokenFactoryWasmQueryHandler(tokenfactory)
	wasmQueryPlugin := NewQueryPlugin(oracleHandler, dexHandler, epochHandler, tokenfactoryHandler)

	queryPluginOpt := wasmkeeper.WithQueryPlugins(&wasmkeeper.QueryPlugins{
		Custom: CustomQuerier(wasmQueryPlugin),
	})
	messengerHandlerOpt := wasmkeeper.WithMessageHandler(
		CustomMessageHandler(router, channelKeeper, capabilityKeeper, bankKeeper, unpacker, portSource, aclKeeper),
	)

	return []wasm.Option{
		queryPluginOpt,
		messengerHandlerOpt,
	}
}
