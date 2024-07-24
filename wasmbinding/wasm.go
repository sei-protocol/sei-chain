package wasmbinding

import (
	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	epochwasm "github.com/sei-protocol/sei-chain/x/epoch/client/wasm"
	epochkeeper "github.com/sei-protocol/sei-chain/x/epoch/keeper"
	evmwasm "github.com/sei-protocol/sei-chain/x/evm/client/wasm"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	oraclewasm "github.com/sei-protocol/sei-chain/x/oracle/client/wasm"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorykeeper "github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
)

type routerWithContext struct {
	router *baseapp.MsgServiceRouter
}

func (rc routerWithContext) Handler(msg sdk.Msg) wasmkeeper.MsgHandler {
	h := rc.router.Handler(msg)
	return func(ctx sdk.Context, req sdk.Msg) (sdk.Context, *sdk.Result, error) {
		result, err := h(ctx, msg)
		return ctx, result, err
	}
}

func RegisterCustomPlugins(
	oracle *oraclekeeper.Keeper,
	epoch *epochkeeper.Keeper,
	tokenfactory *tokenfactorykeeper.Keeper,
	_ *authkeeper.AccountKeeper,
	router *baseapp.MsgServiceRouter,
	channelKeeper wasmtypes.ChannelKeeper,
	capabilityKeeper wasmtypes.CapabilityKeeper,
	bankKeeper wasmtypes.Burner,
	unpacker codectypes.AnyUnpacker,
	portSource wasmtypes.ICS20TransferPortSource,
	aclKeeper aclkeeper.Keeper,
	evmKeeper *evmkeeper.Keeper,
) []wasmkeeper.Option {
	oracleHandler := oraclewasm.NewOracleWasmQueryHandler(oracle)
	epochHandler := epochwasm.NewEpochWasmQueryHandler(epoch)
	tokenfactoryHandler := tokenfactorywasm.NewTokenFactoryWasmQueryHandler(tokenfactory)
	evmHandler := evmwasm.NewEVMQueryHandler(evmKeeper)
	wasmQueryPlugin := NewQueryPlugin(oracleHandler, epochHandler, tokenfactoryHandler, evmHandler)

	queryPluginOpt := wasmkeeper.WithQueryPlugins(&wasmkeeper.QueryPlugins{
		Custom: CustomQuerier(wasmQueryPlugin),
	})
	routerWithCtx := routerWithContext{router}
	messengerHandlerOpt := wasmkeeper.WithMessageHandler(
		CustomMessageHandler(routerWithCtx, channelKeeper, capabilityKeeper, bankKeeper, evmKeeper, unpacker, portSource, aclKeeper),
	)

	return []wasm.Option{
		queryPluginOpt,
		messengerHandlerOpt,
	}
}
