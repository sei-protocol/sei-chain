package keeper

import (
	"fmt"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

// Messenger is an extension point for custom wasmd message handling
type Messenger interface {
	// DispatchMsg encodes the wasmVM message and dispatches it.
	DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) (resCtx sdk.Context, events []sdk.Event, data [][]byte, err error)
}

// replyer is a subset of keeper that can handle replies to submessages
type replyer interface {
	reply(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error)
}

// MessageDispatcher coordinates message sending and submessage reply/ state commits
type MessageDispatcher struct {
	messenger Messenger
	keeper    replyer
}

// NewMessageDispatcher constructor
func NewMessageDispatcher(messenger Messenger, keeper replyer) *MessageDispatcher {
	return &MessageDispatcher{messenger: messenger, keeper: keeper}
}

// DispatchMessages sends all messages.
func (d MessageDispatcher) DispatchMessages(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) error {
	for _, msg := range msgs {
		_, events, _, err := d.messenger.DispatchMsg(ctx, contractAddr, ibcPort, msg, info, codeInfo)
		if err != nil {
			return err
		}
		// redispatch all events, (type sdk.EventTypeMessage will be filtered out in the handler)
		ctx.EventManager().EmitEvents(events)
	}
	return nil
}

// dispatchMsgWithGasLimit sends a message with gas limit applied
func (d MessageDispatcher) dispatchMsgWithGasLimit(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msg wasmvmtypes.CosmosMsg, gasLimit uint64, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) (resCtx sdk.Context, events []sdk.Event, data [][]byte, err error) {
	limitedMeter := sdk.NewGasMeterWithMultiplier(ctx, gasLimit)
	subCtx := ctx.WithGasMeter(limitedMeter)

	// catch out of gas panic and just charge the entire gas limit
	defer func() {
		if r := recover(); r != nil {
			// if it's not an OutOfGas error, raise it again
			if _, ok := r.(sdk.ErrorOutOfGas); !ok {
				// log it to get the original stack trace somewhere (as panic(r) keeps message but stacktrace to here
				moduleLogger(ctx).Info("SubMsg rethrowing panic: %#v", r)
				panic(r)
			}
			ctx.GasMeter().ConsumeGas(gasLimit, "Sub-Message OutOfGas panic")
			err = sdkerrors.Wrap(sdkerrors.ErrOutOfGas, "SubMsg hit gas limit")
		}
	}()
	resCtx, events, data, err = d.messenger.DispatchMsg(subCtx, contractAddr, ibcPort, msg, info, codeInfo)

	// make sure we charge the parent what was spent
	spent := subCtx.GasMeter().GasConsumed()
	ctx.GasMeter().ConsumeGas(spent, "From limited Sub-Message")

	return resCtx, events, data, err
}

// DispatchSubmessages builds a sandbox to execute these messages and returns the execution result to the contract
// that dispatched them, both on success as well as failure
func (d MessageDispatcher) DispatchSubmessages(ctx sdk.Context, contractAddr sdk.AccAddress, ibcPort string, msgs []wasmvmtypes.SubMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) ([]byte, error) {
	var rsp []byte
	for _, msg := range msgs {
		switch msg.ReplyOn {
		case wasmvmtypes.ReplySuccess, wasmvmtypes.ReplyError, wasmvmtypes.ReplyAlways, wasmvmtypes.ReplyNever:
		default:
			return nil, sdkerrors.Wrap(types.ErrInvalid, "replyOn value")
		}
		// first, we build a sub-context which we can use inside the submessages
		subCtx, commit := ctx.CacheContext()
		em := sdk.NewEventManager()
		subCtx = subCtx.WithEventManager(em)

		// check how much gas left locally, optionally wrap the gas meter
		gasRemaining := ctx.GasMeter().Limit() - ctx.GasMeter().GasConsumed()
		limitGas := msg.GasLimit != nil && (*msg.GasLimit < gasRemaining)

		var err error
		var events []sdk.Event
		var data [][]byte
		if limitGas {
			ctx, events, data, err = d.dispatchMsgWithGasLimit(subCtx, contractAddr, ibcPort, msg.Msg, *msg.GasLimit, info, codeInfo)
		} else {
			ctx, events, data, err = d.messenger.DispatchMsg(subCtx, contractAddr, ibcPort, msg.Msg, info, codeInfo)
		}

		// if it succeeds, commit state changes from submessage, and pass on events to Event Manager
		var filteredEvents []sdk.Event
		if err == nil {
			commit()
			filteredEvents = filterEvents(append(em.Events(), events...))
			ctx.EventManager().EmitEvents(filteredEvents)
		} // on failure, revert state from sandbox, and ignore events (just skip doing the above)

		// we only callback if requested. Short-circuit here the cases we don't want to
		if (msg.ReplyOn == wasmvmtypes.ReplySuccess || msg.ReplyOn == wasmvmtypes.ReplyNever) && err != nil {
			return nil, err
		}
		if msg.ReplyOn == wasmvmtypes.ReplyNever || (msg.ReplyOn == wasmvmtypes.ReplyError && err == nil) {
			continue
		}

		// otherwise, we create a SubMsgResult and pass it into the calling contract
		var result wasmvmtypes.SubMsgResult
		if err == nil {
			// just take the first one for now if there are multiple sub-sdk messages
			// and safely return nothing if no data
			var responseData []byte
			if len(data) > 0 {
				responseData = data[0]
			}
			result = wasmvmtypes.SubMsgResult{
				Ok: &wasmvmtypes.SubMsgResponse{
					Events: sdkEventsToWasmVMEvents(filteredEvents),
					Data:   responseData,
				},
			}
		} else {
			// Issue #759 - we don't return error string for worries of non-determinism
			moduleLogger(ctx).Info("Redacting submessage error", "cause", err)
			result = wasmvmtypes.SubMsgResult{
				Err: redactError(err).Error(),
			}
		}

		// now handle the reply, we use the parent context, and abort on error
		reply := wasmvmtypes.Reply{
			ID:     msg.ID,
			Result: result,
		}

		// we can ignore any result returned as there is nothing to do with the data
		// and the events are already in the ctx.EventManager()
		rspData, err := d.keeper.reply(ctx, contractAddr, reply)
		switch {
		case err != nil:
			return nil, sdkerrors.Wrap(err, "reply")
		case rspData != nil:
			rsp = rspData
		}
	}
	return rsp, nil
}

// Issue #759 - we don't return error string for worries of non-determinism
func redactError(err error) error {
	// Do not redact system errors
	// SystemErrors must be created in x/wasm and we can ensure determinism
	if wasmvmtypes.ToSystemError(err) != nil {
		return err
	}

	// FIXME: do we want to hardcode some constant string mappings here as well?
	// Or better document them? (SDK error string may change on a patch release to fix wording)
	// sdk/11 is out of gas
	// sdk/5 is insufficient funds (on bank send)
	// (we can theoretically redact less in the future, but this is a first step to safety)
	codespace, code, _ := sdkerrors.ABCIInfo(err, false)
	return fmt.Errorf("codespace: %s, code: %d", codespace, code)
}

func filterEvents(events []sdk.Event) []sdk.Event {
	// pre-allocate space for efficiency
	res := make([]sdk.Event, 0, len(events))
	for _, ev := range events {
		if ev.Type != "message" {
			res = append(res, ev)
		}
	}
	return res
}

func sdkEventsToWasmVMEvents(events []sdk.Event) []wasmvmtypes.Event {
	res := make([]wasmvmtypes.Event, len(events))
	for i, ev := range events {
		res[i] = wasmvmtypes.Event{
			Type:       ev.Type,
			Attributes: sdkAttributesToWasmVMAttributes(ev.Attributes),
		}
	}
	return res
}

func sdkAttributesToWasmVMAttributes(attrs []abci.EventAttribute) []wasmvmtypes.EventAttribute {
	res := make([]wasmvmtypes.EventAttribute, len(attrs))
	for i, attr := range attrs {
		res[i] = wasmvmtypes.EventAttribute{
			Key:   string(attr.Key),
			Value: string(attr.Value),
		}
	}
	return res
}
