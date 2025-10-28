package wasmbinding

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type CustomRouter struct {
	wasmkeeper.MessageRouter

	evmKeeper *evmkeeper.Keeper
}

func (r *CustomRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	switch m := msg.(type) {
	case *evmtypes.MsgInternalEVMCall:
		return func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			return r.evmKeeper.HandleInternalEVMCall(ctx, m)
		}
	case *evmtypes.MsgInternalEVMDelegateCall:
		return func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			return r.evmKeeper.HandleInternalEVMDelegateCall(ctx, m)
		}
	default:
		return r.MessageRouter.Handler(msg)
	}
}

// forked from wasm
func CustomMessageHandler(
	router wasmkeeper.MessageRouter,
	channelKeeper wasmtypes.ChannelKeeper,
	capabilityKeeper wasmtypes.CapabilityKeeper,
	bankKeeper wasmtypes.Burner,
	evmKeeper *evmkeeper.Keeper,
	unpacker codectypes.AnyUnpacker,
	portSource wasmtypes.ICS20TransferPortSource,
) wasmkeeper.Messenger {
	encoders := wasmkeeper.DefaultEncoders(unpacker, portSource)
	encoders = encoders.Merge(
		&wasmkeeper.MessageEncoders{
			Custom: CustomEncoder,
		})
	return wasmkeeper.NewMessageHandlerChain(
		wasmkeeper.NewSDKMessageHandler(&CustomRouter{MessageRouter: router, evmKeeper: evmKeeper}, encoders),
		wasmkeeper.NewIBCRawPacketHandler(channelKeeper, capabilityKeeper),
		wasmkeeper.NewBurnCoinMessageHandler(bankKeeper),
	)
}
