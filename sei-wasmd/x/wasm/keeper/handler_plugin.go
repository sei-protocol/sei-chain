package keeper

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// msgEncoder is an extension point to customize encodings
type msgEncoder interface {
	// Encode converts wasmvm message to n cosmos message types
	Encode(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) ([]sdk.Msg, error)
}

// MessageRouter ADR 031 request type routing
type MessageRouter interface {
	Handler(msg sdk.Msg) baseapp.MsgServiceHandler
}

// SDKMessageHandler can handles messages that can be encoded into sdk.Message types and routed.
type SDKMessageHandler struct {
	router   MessageRouter
	encoders msgEncoder
}

func NewDefaultMessageHandler(
	router MessageRouter,
	bankKeeper types.Burner,
	unpacker codectypes.AnyUnpacker,
	customEncoders ...*MessageEncoders,
) Messenger {
	encoders := DefaultEncoders(unpacker)
	for _, e := range customEncoders {
		encoders = encoders.Merge(e)
	}
	return NewMessageHandlerChain(
		NewSDKMessageHandler(router, encoders),
		NewBurnCoinMessageHandler(bankKeeper),
	)
}

func NewSDKMessageHandler(router MessageRouter, encoders msgEncoder) SDKMessageHandler {
	return SDKMessageHandler{
		router:   router,
		encoders: encoders,
	}
}

func (h SDKMessageHandler) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
	sdkMsgs, err := h.encoders.Encode(ctx, contractAddr, contractIBCPortID, msg, info, codeInfo)
	if err != nil {
		return nil, nil, err
	}
	for _, sdkMsg := range sdkMsgs {
		res, err := h.handleSdkMessage(ctx, contractAddr, sdkMsg)
		if err != nil {
			return nil, nil, err
		}
		// append data
		data = append(data, res.Data)
		// append events
		sdkEvents := make([]sdk.Event, len(res.Events))
		for i := range res.Events {
			sdkEvents[i] = sdk.Event(res.Events[i])
		}
		events = append(events, sdkEvents...)
	}
	return
}

func (h SDKMessageHandler) handleSdkMessage(ctx sdk.Context, contractAddr sdk.Address, msg sdk.Msg) (*sdk.Result, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	// make sure this account can send it
	for _, acct := range msg.GetSigners() {
		if !acct.Equals(contractAddr) {
			return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "contract doesn't have permission")
		}
	}

	// find the handler and execute it
	if handler := h.router.Handler(msg); handler != nil {
		// ADR 031 request type routing
		msgResult, err := handler(ctx, msg)
		return msgResult, err
	}
	// legacy sdk.Msg routing
	// Assuming that the app developer has migrated all their Msgs to
	// proto messages and has registered all `Msg services`, then this
	// path should never be called, because all those Msgs should be
	// registered within the `msgServiceRouter` already.
	return nil, sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "can't route message %+v", msg)
}

// MessageHandlerChain defines a chain of handlers that are called one by one until it can be handled.
type MessageHandlerChain struct {
	handlers []Messenger
}

func NewMessageHandlerChain(first Messenger, others ...Messenger) *MessageHandlerChain {
	r := &MessageHandlerChain{handlers: append([]Messenger{first}, others...)}
	for i := range r.handlers {
		if r.handlers[i] == nil {
			panic(fmt.Sprintf("handler must not be nil at position : %d", i))
		}
	}
	return r
}

// DispatchMsg dispatch message and calls chained handlers one after another in
// order to find the right one to process given message. If a handler cannot
// process given message (returns ErrUnknownMsg), its result is ignored and the
// next handler is executed.
func (m MessageHandlerChain) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) ([]sdk.Event, [][]byte, error) {
	for _, h := range m.handlers {
		events, data, err := h.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg, info, codeInfo)
		switch {
		case err == nil:
			return events, data, nil
		case errors.Is(err, types.ErrUnknownMsg):
			continue
		default:
			return events, data, err
		}
	}
	return nil, nil, sdkerrors.Wrap(types.ErrUnknownMsg, "no handler found")
}

var _ Messenger = MessageHandlerFunc(nil)

// MessageHandlerFunc is a helper to construct a function based message handler.
type MessageHandlerFunc func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) (events []sdk.Event, data [][]byte, err error)

// DispatchMsg delegates dispatching of provided message into the MessageHandlerFunc.
func (m MessageHandlerFunc) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
	return m(ctx, contractAddr, contractIBCPortID, msg, info, codeInfo)
}

// NewBurnCoinMessageHandler handles wasmvm.BurnMsg messages
func NewBurnCoinMessageHandler(burner types.Burner) MessageHandlerFunc {
	return func(ctx sdk.Context, contractAddr sdk.AccAddress, _ string, msg wasmvmtypes.CosmosMsg, _ wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
		if msg.Bank != nil && msg.Bank.Burn != nil {
			coins, err := ConvertWasmCoinsToSdkCoins(msg.Bank.Burn.Amount)
			if err != nil {
				return nil, nil, err
			}
			if err := burner.SendCoinsFromAccountToModule(ctx, contractAddr, types.ModuleName, coins); err != nil {
				return nil, nil, sdkerrors.Wrap(err, "transfer to module")
			}
			if err := burner.BurnCoins(ctx, types.ModuleName, coins); err != nil {
				return nil, nil, sdkerrors.Wrap(err, "burn coins")
			}
			logger.Info("Burned", "amount", coins)
			return nil, nil, nil
		}
		return nil, nil, types.ErrUnknownMsg
	}
}
