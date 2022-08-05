package wasmbinding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexmsgserver "github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const SynchronizationTimeoutInSeconds = 2

type SeiWasmMessage struct {
	PlaceOrders  json.RawMessage `json:"place_orders,omitempty"`
	CancelOrders json.RawMessage `json:"cancel_orders,omitempty"`
}

func CustomMessageDecorator(dexKeeper *dexkeeper.Keeper) func(wasmkeeper.Messenger) wasmkeeper.Messenger {
	return func(old wasmkeeper.Messenger) wasmkeeper.Messenger {
		return &CustomMessenger{
			wrapped:   old,
			dexKeeper: dexKeeper,
		}
	}
}

type CustomMessenger struct {
	wrapped   wasmkeeper.Messenger
	dexKeeper *dexkeeper.Keeper
}

func (m *CustomMessenger) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg) ([]sdk.Event, [][]byte, error) {
	if err := m.synchronize(ctx); err != nil {
		return nil, nil, err
	}
	if msg.Custom != nil {
		var parsedMessage SeiWasmMessage
		if err := json.Unmarshal(msg.Custom, &parsedMessage); err != nil {
			return nil, nil, sdkerrors.Wrap(err, "Error parsing Sei Wasm Message")
		}
		msgServer := dexmsgserver.NewMsgServerImpl(*m.dexKeeper)
		switch {
		case parsedMessage.PlaceOrders != nil:
			return Handle(
				ctx, m.dexKeeper, contractAddr, parsedMessage.PlaceOrders, msgServer.PlaceOrders, dexwasm.EncodeDexPlaceOrders,
				func(m *types.MsgPlaceOrders) string { return m.ContractAddr },
			)
		case parsedMessage.CancelOrders != nil:
			return Handle(
				ctx, m.dexKeeper, contractAddr, parsedMessage.CancelOrders, msgServer.CancelOrders, dexwasm.EncodeDexCancelOrders,
				func(m *types.MsgCancelOrders) string { return m.ContractAddr },
			)
		default:
			return nil, nil, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Wasm Message"}
		}
	}
	return m.wrapped.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg)
}

func (m *CustomMessenger) synchronize(ctx sdk.Context) error {
	executingContract := ctx.Context().Value(contract.CtxKeyExecutingContract)
	if executingContract == nil {
		return nil
	}
	contractAddr, ok := executingContract.(string)
	if !ok {
		return errors.New("invalid executing contract value in context")
	}
	contractInfo, err := m.dexKeeper.GetContract(ctx, contractAddr)
	if err != nil {
		return err
	}
	if contractInfo.Dependencies == nil {
		return nil
	}
	for _, dependency := range contractInfo.Dependencies {
		immediateElderSibling := dependency.ImmediateElderSibling
		if immediateElderSibling == "" {
			continue
		}
		channels := ctx.Context().Value(contract.CtxKeyExecTermSignal)
		if channels == nil {
			return errors.New("no execution terminal signal channels is set in context")
		}
		typedChannels, ok := channels.(datastructures.TypedSyncMap[string, chan struct{}])
		if !ok {
			return errors.New("invalid termination signal channels in context")
		}
		targetChannel, ok := typedChannels.Load(immediateElderSibling)
		if !ok {
			return fmt.Errorf("no termination signal channel for contract %s in context", immediateElderSibling)
		}

		select {
		case <-targetChannel:
			targetChannel <- struct{}{}
		case <-time.After(SynchronizationTimeoutInSeconds * time.Second):
			return fmt.Errorf("timing out waiting for termination of %s", immediateElderSibling)
		}
	}
	return nil
}

func Handle[M sdk.Msg, R any](
	ctx sdk.Context,
	dexKeeper *dexkeeper.Keeper,
	contractAddr sdk.AccAddress,
	rawMsg json.RawMessage,
	serverHandler func(context.Context, M) (R, error),
	encoder func(json.RawMessage, sdk.AccAddress) ([]sdk.Msg, error),
	contractGetter func(M) string,
) ([]sdk.Event, [][]byte, error) {
	msgs, err := encoder(rawMsg, contractAddr)
	if err != nil {
		return nil, nil, err
	}
	for _, msg := range msgs {
		if err := ValidateDependency(ctx, dexKeeper, msg.(M), contractGetter); err != nil {
			return nil, nil, err
		}
	}
	if err := Serve(ctx, serverHandler, msgs); err != nil {
		return nil, nil, err
	}
	return nil, nil, nil
}

func Serve[M sdk.Msg, R any](ctx sdk.Context, serverHandler func(context.Context, M) (R, error), msgs []sdk.Msg) error {
	for _, msg := range msgs {
		typedMsg := msg.(M)
		if _, err := serverHandler(sdk.WrapSDKContext(ctx), typedMsg); err != nil {
			return err
		}
	}
	return nil
}

func ValidateDependency[M sdk.Msg](ctx sdk.Context, dexKeeper *dexkeeper.Keeper, msg M, contractGetter func(M) string) error {
	executingContract := ctx.Context().Value(contract.CtxKeyExecutingContract)
	if executingContract == nil {
		return nil
	}
	contractAddr, ok := executingContract.(string)
	if !ok {
		return errors.New("invalid executing contract value in context")
	}
	contractInfo, err := dexKeeper.GetContract(ctx, contractAddr)
	if err != nil {
		return err
	}
	calleeContract := contractGetter(msg)
	for _, dependency := range contractInfo.Dependencies {
		if dependency.Dependency == calleeContract {
			return nil
		}
	}
	return fmt.Errorf("dependency from %s to %s is not specified", contractAddr, calleeContract)
}
