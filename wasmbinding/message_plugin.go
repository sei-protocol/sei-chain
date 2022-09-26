package wasmbinding

import (
	"encoding/json"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/sei-protocol/sei-chain/wasmbinding/bindings"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
)

// CustomMessageDecorator returns decorator for custom CosmWasm bindings messages
func CustomMessageDecorator(
	router wasmkeeper.MessageRouter,
	accountKeeper *authkeeper.AccountKeeper,
) func(wasmkeeper.Messenger) wasmkeeper.Messenger {
	return func(old wasmkeeper.Messenger) wasmkeeper.Messenger {
		return &CustomMessenger{
			router:        router,
			wrapped:       old,
			accountKeeper: accountKeeper,
		}
	}
}

type CustomMessenger struct {
	router        wasmkeeper.MessageRouter
	wrapped       wasmkeeper.Messenger
	accountKeeper *authkeeper.AccountKeeper
}

type SeiWasmMessage struct {
	PlaceOrders  json.RawMessage `json:"place_orders,omitempty"`
	CancelOrders json.RawMessage `json:"cancel_orders,omitempty"`
	CreateDenom  json.RawMessage `json:"create_denom,omitempty"`
	MintTokens   json.RawMessage `json:"mint_tokens,omitempty"`
	BurnTokens   json.RawMessage `json:"burn_tokens,omitempty"`
	ChangeAdmin  json.RawMessage `json:"change_admin,omitempty"`
}

var _ wasmkeeper.Messenger = &CustomMessenger{}

// DispatchMsg executes on the bindingMsgs
func (m *CustomMessenger) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg) ([]sdk.Event, [][]byte, error) {
	if msg.Custom != nil {
		return m.DispatchCustomMsg(ctx, contractAddr, contractIBCPortID, msg)
	}
	return m.wrapped.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg)
}

// DispatchCustomMsg function is forked from wasmd. sdk.Msg will be validated and routed to the corresponding module msg server in this function.
func (m *CustomMessenger) DispatchCustomMsg(
	ctx sdk.Context,
	contractAddr sdk.AccAddress,
	contractIBCPortID string,
	msg wasmvmtypes.CosmosMsg,
) (events []sdk.Event, data [][]byte, err error) {
	var parsedMessage SeiWasmMessage
	if err := json.Unmarshal(msg.Custom, &parsedMessage); err != nil {
		return nil, nil, bindings.ErrParsingSeiWasmMsg
	}

	var sdkMsgs []sdk.Msg
	switch {
	case parsedMessage.PlaceOrders != nil:
		sdkMsgs, err = dexwasm.EncodeDexPlaceOrders(parsedMessage.PlaceOrders, contractAddr)
	case parsedMessage.CancelOrders != nil:
		sdkMsgs, err = dexwasm.EncodeDexCancelOrders(parsedMessage.CancelOrders, contractAddr)
	case parsedMessage.CreateDenom != nil:
		sdkMsgs, err = tokenfactorywasm.EncodeTokenFactoryCreateDenom(parsedMessage.CreateDenom, contractAddr)
	case parsedMessage.MintTokens != nil:
		sdkMsgs, err = tokenfactorywasm.EncodeTokenFactoryMint(parsedMessage.MintTokens, contractAddr)
	case parsedMessage.BurnTokens != nil:
		sdkMsgs, err = tokenfactorywasm.EncodeTokenFactoryBurn(parsedMessage.BurnTokens, contractAddr)
	case parsedMessage.ChangeAdmin != nil:
		sdkMsgs, err = tokenfactorywasm.EncodeTokenFactoryChangeAdmin(parsedMessage.ChangeAdmin, contractAddr)
	default:
		sdkMsgs, err = []sdk.Msg{}, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Wasm Message"}
	}
	if err != nil {
		return nil, nil, err
	}

	for _, sdkMsg := range sdkMsgs {
		res, err := m.handleSdkMessage(ctx, contractAddr, sdkMsg)
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
	return events, data, nil
}

// This function is forked from wasmd. sdk.Msg will be validated and routed to the corresponding module msg server in this function.
func (m *CustomMessenger) handleSdkMessage(ctx sdk.Context, contractAddr sdk.Address, msg sdk.Msg) (*sdk.Result, error) {
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
	if handler := m.router.Handler(msg); handler != nil {
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
