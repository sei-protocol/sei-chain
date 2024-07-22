package wasmtesting

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type MsgHandler = func(ctx sdk.Context, req sdk.Msg) (sdk.Context, *sdk.Result, error)

// MockMessageRouter mock for testing
type MockMessageRouter struct {
	HandlerFn func(msg sdk.Msg) MsgHandler
}

// Handler is the entry point
func (m MockMessageRouter) Handler(msg sdk.Msg) MsgHandler {
	if m.HandlerFn == nil {
		panic("not expected to be called")
	}
	return m.HandlerFn(msg)
}

// MessageRouterFunc convenient type to match the keeper.MessageRouter interface
type MessageRouterFunc func(msg sdk.Msg) MsgHandler

// Handler is the entry point
func (m MessageRouterFunc) Handler(msg sdk.Msg) MsgHandler {
	return m(msg)
}
