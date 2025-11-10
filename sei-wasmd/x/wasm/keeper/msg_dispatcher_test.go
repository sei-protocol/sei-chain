package keeper

import (
	"errors"
	"fmt"
	"testing"

	"github.com/tendermint/tendermint/libs/log"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/CosmWasm/wasmd/x/wasm/keeper/wasmtesting"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

func TestDispatchSubmessages(t *testing.T) {
	noReplyCalled := &mockReplyer{}
	var anyGasLimit uint64 = 1
	specs := map[string]struct {
		msgs       []wasmvmtypes.SubMsg
		replyer    *mockReplyer
		msgHandler *wasmtesting.MockMessageHandler
		expErr     bool
		expData    []byte
		expCommits []bool
		expEvents  sdk.Events
	}{
		"no reply on error without error": {
			msgs:    []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyError}},
			replyer: noReplyCalled,
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, [][]byte{[]byte("myData")}, nil
				},
			},
			expCommits: []bool{true},
		},
		"no reply on success without success": {
			msgs:    []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplySuccess}},
			replyer: noReplyCalled,
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, nil, errors.New("test, ignore")
				},
			},
			expCommits: []bool{false},
			expErr:     true,
		},
		"reply on success - received": {
			msgs: []wasmvmtypes.SubMsg{{
				ReplyOn: wasmvmtypes.ReplySuccess,
			}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					return []byte("myReplyData"), nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, [][]byte{[]byte("myData")}, nil
				},
			},
			expData:    []byte("myReplyData"),
			expCommits: []bool{true},
		},
		"reply on error - handled": {
			msgs: []wasmvmtypes.SubMsg{{
				ReplyOn: wasmvmtypes.ReplyError,
			}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					return []byte("myReplyData"), nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, nil, errors.New("my error")
				},
			},
			expData:    []byte("myReplyData"),
			expCommits: []bool{false},
		},
		"with reply events": {
			msgs: []wasmvmtypes.SubMsg{{
				ReplyOn: wasmvmtypes.ReplySuccess,
			}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					ctx.EventManager().EmitEvent(sdk.NewEvent("wasm-reply"))
					return []byte("myReplyData"), nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					myEvents := []sdk.Event{{Type: "myEvent", Attributes: []abci.EventAttribute{{Key: []byte("foo"), Value: []byte("bar")}}}}
					return myEvents, [][]byte{[]byte("myData")}, nil
				},
			},
			expData:    []byte("myReplyData"),
			expCommits: []bool{true},
			expEvents: []sdk.Event{
				{
					Type:       "myEvent",
					Attributes: []abci.EventAttribute{{Key: []byte("foo"), Value: []byte("bar")}},
				},
				sdk.NewEvent("wasm-reply"),
			},
		},
		"with context events - released on commit": {
			msgs: []wasmvmtypes.SubMsg{{
				ReplyOn: wasmvmtypes.ReplyNever,
			}},
			replyer: &mockReplyer{},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					myEvents := []sdk.Event{{Type: "myEvent", Attributes: []abci.EventAttribute{{Key: []byte("foo"), Value: []byte("bar")}}}}
					ctx.EventManager().EmitEvents(myEvents)
					return nil, nil, nil
				},
			},
			expCommits: []bool{true},
			expEvents: []sdk.Event{{
				Type:       "myEvent",
				Attributes: []abci.EventAttribute{{Key: []byte("foo"), Value: []byte("bar")}},
			}},
		},
		"with context events - discarded on failure": {
			msgs: []wasmvmtypes.SubMsg{{
				ReplyOn: wasmvmtypes.ReplyNever,
			}},
			replyer: &mockReplyer{},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					myEvents := []sdk.Event{{Type: "myEvent", Attributes: []abci.EventAttribute{{Key: []byte("foo"), Value: []byte("bar")}}}}
					ctx.EventManager().EmitEvents(myEvents)
					return nil, nil, errors.New("testing")
				},
			},
			expCommits: []bool{false},
			expErr:     true,
		},
		"reply returns error": {
			msgs: []wasmvmtypes.SubMsg{{
				ReplyOn: wasmvmtypes.ReplySuccess,
			}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					return nil, errors.New("reply failed")
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, nil, nil
				},
			},
			expCommits: []bool{false},
			expErr:     true,
		},
		"with gas limit - out of gas": {
			msgs: []wasmvmtypes.SubMsg{{
				GasLimit: &anyGasLimit,
				ReplyOn:  wasmvmtypes.ReplyError,
			}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					return []byte("myReplyData"), nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					ctx.GasMeter().ConsumeGas(sdk.Gas(101), "testing")
					return nil, [][]byte{[]byte("someData")}, nil
				},
			},
			expData:    []byte("myReplyData"),
			expCommits: []bool{false},
		},
		"with gas limit - within limit no error": {
			msgs: []wasmvmtypes.SubMsg{{
				GasLimit: &anyGasLimit,
				ReplyOn:  wasmvmtypes.ReplyError,
			}},
			replyer: &mockReplyer{},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					ctx.GasMeter().ConsumeGas(sdk.Gas(1), "testing")
					return nil, [][]byte{[]byte("someData")}, nil
				},
			},
			expCommits: []bool{true},
		},
		"never reply - with nil response": {
			msgs:    []wasmvmtypes.SubMsg{{ID: 1, ReplyOn: wasmvmtypes.ReplyNever}, {ID: 2, ReplyOn: wasmvmtypes.ReplyNever}},
			replyer: &mockReplyer{},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, [][]byte{nil}, nil
				},
			},
			expCommits: []bool{true, true},
		},
		"never reply - with any non nil response": {
			msgs:    []wasmvmtypes.SubMsg{{ID: 1, ReplyOn: wasmvmtypes.ReplyNever}, {ID: 2, ReplyOn: wasmvmtypes.ReplyNever}},
			replyer: &mockReplyer{},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, [][]byte{{}}, nil
				},
			},
			expCommits: []bool{true, true},
		},
		"never reply - with error": {
			msgs:    []wasmvmtypes.SubMsg{{ID: 1, ReplyOn: wasmvmtypes.ReplyNever}, {ID: 2, ReplyOn: wasmvmtypes.ReplyNever}},
			replyer: &mockReplyer{},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, [][]byte{{}}, errors.New("testing")
				},
			},
			expCommits: []bool{false, false},
			expErr:     true,
		},
		"multiple msg - last reply returned": {
			msgs: []wasmvmtypes.SubMsg{{ID: 1, ReplyOn: wasmvmtypes.ReplyError}, {ID: 2, ReplyOn: wasmvmtypes.ReplyError}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					return []byte(fmt.Sprintf("myReplyData:%d", reply.ID)), nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, nil, errors.New("my error")
				},
			},
			expData:    []byte("myReplyData:2"),
			expCommits: []bool{false, false},
		},
		"multiple msg - last non nil reply returned": {
			msgs: []wasmvmtypes.SubMsg{{ID: 1, ReplyOn: wasmvmtypes.ReplyError}, {ID: 2, ReplyOn: wasmvmtypes.ReplyError}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					if reply.ID == 2 {
						return nil, nil
					}
					return []byte("myReplyData:1"), nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, nil, errors.New("my error")
				},
			},
			expData:    []byte("myReplyData:1"),
			expCommits: []bool{false, false},
		},
		"multiple msg - empty reply can overwrite result": {
			msgs: []wasmvmtypes.SubMsg{{ID: 1, ReplyOn: wasmvmtypes.ReplyError}, {ID: 2, ReplyOn: wasmvmtypes.ReplyError}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					if reply.ID == 2 {
						return []byte{}, nil
					}
					return []byte("myReplyData:1"), nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, nil, errors.New("my error")
				},
			},
			expData:    []byte{},
			expCommits: []bool{false, false},
		},
		"message event filtered without reply": {
			msgs: []wasmvmtypes.SubMsg{{
				ReplyOn: wasmvmtypes.ReplyNever,
			}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					return nil, errors.New("should never be called")
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					myEvents := []sdk.Event{
						sdk.NewEvent("message"),
						sdk.NewEvent("execute", sdk.NewAttribute("foo", "bar")),
					}
					return myEvents, [][]byte{[]byte("myData")}, nil
				},
			},
			expData:    nil,
			expCommits: []bool{true},
			expEvents:  []sdk.Event{sdk.NewEvent("execute", sdk.NewAttribute("foo", "bar"))},
		},
		"reply gets proper events": {
			msgs: []wasmvmtypes.SubMsg{{ID: 1, ReplyOn: wasmvmtypes.ReplyAlways}},
			replyer: &mockReplyer{
				replyFn: func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
					if reply.Result.Err != "" {
						return nil, errors.New(reply.Result.Err)
					}
					res := reply.Result.Ok

					// ensure the input events are what we expect
					// I didn't use require.Equal() to act more like a contract... but maybe that would be better
					if len(res.Events) != 2 {
						return nil, fmt.Errorf("event count: %#v", res.Events)
					}
					if res.Events[0].Type != "execute" {
						return nil, fmt.Errorf("event0: %#v", res.Events[0])
					}
					if res.Events[1].Type != "wasm" {
						return nil, fmt.Errorf("event1: %#v", res.Events[1])
					}

					// let's add a custom event here and see if it makes it out
					ctx.EventManager().EmitEvent(sdk.NewEvent("wasm-reply"))

					// update data from what we got in
					return res.Data, nil
				},
			},
			msgHandler: &wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					events = []sdk.Event{
						sdk.NewEvent("message", sdk.NewAttribute("_contract_address", contractAddr.String())),
						// we don't know what the contarctAddr will be so we can't use it in the final tests
						sdk.NewEvent("execute", sdk.NewAttribute("_contract_address", "placeholder-random-addr")),
						sdk.NewEvent("wasm", sdk.NewAttribute("random", "data")),
					}
					return events, [][]byte{[]byte("subData")}, nil
				},
			},
			expData:    []byte("subData"),
			expCommits: []bool{true},
			expEvents: []sdk.Event{
				sdk.NewEvent("execute", sdk.NewAttribute("_contract_address", "placeholder-random-addr")),
				sdk.NewEvent("wasm", sdk.NewAttribute("random", "data")),
				sdk.NewEvent("wasm-reply"),
			},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			var mockStore wasmtesting.MockCommitMultiStore
			em := sdk.NewEventManager()
			ctx := sdk.Context{}.WithMultiStore(&mockStore).
				WithGasMeter(sdk.NewGasMeter(100, 1, 1)).
				WithEventManager(em).WithLogger(log.TestingLogger())
			d := NewMessageDispatcher(spec.msgHandler, spec.replyer)
			gotData, gotErr := d.DispatchSubmessages(ctx, RandomAccountAddress(t), "any_port", spec.msgs, wasmvmtypes.MessageInfo{}, types.CodeInfo{})
			if spec.expErr {
				require.Error(t, gotErr)
				assert.Empty(t, em.Events())
				return
			} else {
				require.NoError(t, gotErr)
				assert.Equal(t, spec.expData, gotData)
			}
			assert.Equal(t, spec.expCommits, mockStore.Committed)
			if len(spec.expEvents) == 0 {
				assert.Empty(t, em.Events())
			} else {
				assert.Equal(t, spec.expEvents, em.Events())
			}
		})
	}
}

type mockReplyer struct {
	replyFn func(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error)
}

func (m mockReplyer) reply(ctx sdk.Context, contractAddress sdk.AccAddress, reply wasmvmtypes.Reply) ([]byte, error) {
	if m.replyFn == nil {
		panic("not expected to be called")
	}
	return m.replyFn(ctx, contractAddress, reply)
}
