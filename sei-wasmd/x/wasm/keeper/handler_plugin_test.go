package keeper

import (
	"encoding/json"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	ibccoretypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
	wasmvm "github.com/sei-protocol/sei-chain/sei-wasmvm"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper/wasmtesting"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

func TestMessageHandlerChainDispatch(t *testing.T) {
	capturingHandler, gotMsgs := wasmtesting.NewCapturingMessageHandler()

	alwaysUnknownMsgHandler := &wasmtesting.MockMessageHandler{
		DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
			return nil, nil, types.ErrUnknownMsg
		},
	}

	assertNotCalledHandler := &wasmtesting.MockMessageHandler{
		DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
			t.Fatal("not expected to be called")
			return
		},
	}

	myMsg := wasmvmtypes.CosmosMsg{Custom: []byte(`{}`)}
	specs := map[string]struct {
		handlers  []Messenger
		expErr    *sdkerrors.Error
		expEvents []sdk.Event
	}{
		"single handler": {
			handlers: []Messenger{capturingHandler},
		},
		"passed to next handler": {
			handlers: []Messenger{alwaysUnknownMsgHandler, capturingHandler},
		},
		"stops iteration when handled": {
			handlers: []Messenger{capturingHandler, assertNotCalledHandler},
		},
		"stops iteration on handler error": {
			handlers: []Messenger{&wasmtesting.MockMessageHandler{
				DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, _ types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
					return nil, nil, types.ErrInvalidMsg
				},
			}, assertNotCalledHandler},
			expErr: types.ErrInvalidMsg,
		},
		"return events when handle": {
			handlers: []Messenger{
				&wasmtesting.MockMessageHandler{
					DispatchMsgFn: func(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg, info wasmvmtypes.MessageInfo, codeInfo types.CodeInfo) (events []sdk.Event, data [][]byte, err error) {
						_, data, _ = capturingHandler.DispatchMsg(ctx, contractAddr, contractIBCPortID, msg, info, codeInfo)
						return []sdk.Event{sdk.NewEvent("myEvent", sdk.NewAttribute("foo", "bar"))}, data, nil
					},
				},
			},
			expEvents: []sdk.Event{sdk.NewEvent("myEvent", sdk.NewAttribute("foo", "bar"))},
		},
		"return error when none can handle": {
			handlers: []Messenger{alwaysUnknownMsgHandler},
			expErr:   types.ErrUnknownMsg,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			*gotMsgs = make([]wasmvmtypes.CosmosMsg, 0)

			// when
			h := MessageHandlerChain{spec.handlers}
			gotEvents, gotData, gotErr := h.DispatchMsg(sdk.Context{}, RandomAccountAddress(t), "anyPort", myMsg, wasmvmtypes.MessageInfo{}, types.CodeInfo{})

			// then
			require.True(t, spec.expErr.Is(gotErr), "exp %v but got %#+v", spec.expErr, gotErr)
			if spec.expErr != nil {
				return
			}
			assert.Equal(t, []wasmvmtypes.CosmosMsg{myMsg}, *gotMsgs)
			assert.Equal(t, [][]byte{{1}}, gotData) // {1} is default in capturing handler
			assert.Equal(t, spec.expEvents, gotEvents)
		})
	}
}

func TestSDKMessageHandlerDispatch(t *testing.T) {
	myEvent := sdk.NewEvent("myEvent", sdk.NewAttribute("foo", "bar"))
	const myData = "myData"
	myRouterResult := sdk.Result{
		Data:   []byte(myData),
		Events: sdk.Events{myEvent}.ToABCIEvents(),
	}

	var gotMsg []sdk.Msg
	capturingMessageRouter := wasmtesting.MessageRouterFunc(func(msg sdk.Msg) baseapp.MsgServiceHandler {
		return func(ctx sdk.Context, req sdk.Msg) (*sdk.Result, error) {
			gotMsg = append(gotMsg, msg)
			return &myRouterResult, nil
		}
	})
	noRouteMessageRouter := wasmtesting.MessageRouterFunc(func(msg sdk.Msg) baseapp.MsgServiceHandler {
		return nil
	})
	myContractAddr := RandomAccountAddress(t)
	myContractMessage := wasmvmtypes.CosmosMsg{Custom: []byte("{}")}

	specs := map[string]struct {
		srcRoute         MessageRouter
		srcEncoder       CustomEncoder
		expErr           *sdkerrors.Error
		expMsgDispatched int
	}{
		"all good": {
			srcRoute: capturingMessageRouter,
			srcEncoder: func(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]sdk.Msg, error) {
				myMsg := types.MsgExecuteContract{
					Sender:   myContractAddr.String(),
					Contract: RandomBech32AccountAddress(t),
					Msg:      []byte("{}"),
				}
				return []sdk.Msg{&myMsg}, nil
			},
			expMsgDispatched: 1,
		},
		"multiple output msgs": {
			srcRoute: capturingMessageRouter,
			srcEncoder: func(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]sdk.Msg, error) {
				first := &types.MsgExecuteContract{
					Sender:   myContractAddr.String(),
					Contract: RandomBech32AccountAddress(t),
					Msg:      []byte("{}"),
				}
				second := &types.MsgExecuteContract{
					Sender:   myContractAddr.String(),
					Contract: RandomBech32AccountAddress(t),
					Msg:      []byte("{}"),
				}
				return []sdk.Msg{first, second}, nil
			},
			expMsgDispatched: 2,
		},
		"invalid sdk message rejected": {
			srcRoute: capturingMessageRouter,
			srcEncoder: func(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]sdk.Msg, error) {
				invalidMsg := types.MsgExecuteContract{
					Sender:   myContractAddr.String(),
					Contract: RandomBech32AccountAddress(t),
					Msg:      []byte("INVALID_JSON"),
				}
				return []sdk.Msg{&invalidMsg}, nil
			},
			expErr: types.ErrInvalid,
		},
		"invalid sender rejected": {
			srcRoute: capturingMessageRouter,
			srcEncoder: func(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]sdk.Msg, error) {
				invalidMsg := types.MsgExecuteContract{
					Sender:   RandomBech32AccountAddress(t),
					Contract: RandomBech32AccountAddress(t),
					Msg:      []byte("{}"),
				}
				return []sdk.Msg{&invalidMsg}, nil
			},
			expErr: sdkerrors.ErrUnauthorized,
		},
		"unroutable message rejected": {
			srcRoute: noRouteMessageRouter,
			srcEncoder: func(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]sdk.Msg, error) {
				myMsg := types.MsgExecuteContract{
					Sender:   myContractAddr.String(),
					Contract: RandomBech32AccountAddress(t),
					Msg:      []byte("{}"),
				}
				return []sdk.Msg{&myMsg}, nil
			},
			expErr: sdkerrors.ErrUnknownRequest,
		},
		"encoding error passed": {
			srcRoute: capturingMessageRouter,
			srcEncoder: func(sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo, _ types.CodeInfo) ([]sdk.Msg, error) {
				myErr := types.ErrUnpinContractFailed // any error that is not used
				return nil, myErr
			},
			expErr: types.ErrUnpinContractFailed,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			gotMsg = make([]sdk.Msg, 0)

			// when
			ctx := sdk.Context{}
			h := NewSDKMessageHandler(spec.srcRoute, MessageEncoders{Custom: spec.srcEncoder})
			gotEvents, gotData, gotErr := h.DispatchMsg(ctx, myContractAddr, "myPort", myContractMessage, wasmvmtypes.MessageInfo{}, types.CodeInfo{})

			// then
			require.True(t, spec.expErr.Is(gotErr), "exp %v but got %#+v", spec.expErr, gotErr)
			if spec.expErr != nil {
				require.Len(t, gotMsg, 0)
				return
			}
			assert.Len(t, gotMsg, spec.expMsgDispatched)
			for i := 0; i < spec.expMsgDispatched; i++ {
				assert.Equal(t, myEvent, gotEvents[i])
				assert.Equal(t, []byte(myData), gotData[i])
			}
		})
	}
}

func TestIBCRawPacketHandler(t *testing.T) {
	specs := map[string]struct {
		portID    string
		channelID string
	}{
		"valid packet": {
			portID:    "contractsIBCPort",
			channelID: "channel-1",
		},
		"empty contract port": {
			channelID: "channel-1",
		},
		"empty channel": {
			portID: "contractsIBCPort",
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			h := NewIBCRawPacketHandler()
			msg := wasmvmtypes.CosmosMsg{IBC: &wasmvmtypes.IBCMsg{SendPacket: &wasmvmtypes.SendPacketMsg{
				ChannelID: spec.channelID,
				Data:      []byte("myData"),
				Timeout:   wasmvmtypes.IBCTimeout{Block: &wasmvmtypes.IBCTimeoutBlock{Revision: 1, Height: 2}},
			}}}

			evts, data, gotErr := h.DispatchMsg(sdk.Context{}, RandomAccountAddress(t), spec.portID, msg, wasmvmtypes.MessageInfo{}, types.CodeInfo{})

			require.ErrorIs(t, gotErr, ibccoretypes.ErrIBCDeprecated)
			require.EqualError(t, gotErr, "ibc module is deprecated")
			assert.Nil(t, evts)
			assert.Nil(t, data)
		})
	}
}

func TestBurnCoinMessageHandlerIntegration(t *testing.T) {
	// testing via full keeper setup so that we are confident the
	// module permissions are set correct and no other handler
	// picks the message in the default handler chain
	ctx, keepers := CreateDefaultTestInput(t)
	// set some supply
	keepers.Faucet.NewFundedAccount(ctx, sdk.NewCoin("denom", sdk.NewInt(10_000_000)))
	k := keepers.WasmKeeper

	example := InstantiateHackatomExampleContract(t, ctx, keepers) // with deposit of 100 stake

	before, err := keepers.BankKeeper.TotalSupply(sdk.WrapSDKContext(ctx), &banktypes.QueryTotalSupplyRequest{})
	require.NoError(t, err)

	specs := map[string]struct {
		msg    wasmvmtypes.BurnMsg
		expErr bool
	}{
		"all good": {
			msg: wasmvmtypes.BurnMsg{
				Amount: wasmvmtypes.Coins{{
					Denom:  "denom",
					Amount: "100",
				}},
			},
		},
		"not enough funds in contract": {
			msg: wasmvmtypes.BurnMsg{
				Amount: wasmvmtypes.Coins{{
					Denom:  "denom",
					Amount: "101",
				}},
			},
			expErr: true,
		},
		"zero amount rejected": {
			msg: wasmvmtypes.BurnMsg{
				Amount: wasmvmtypes.Coins{{
					Denom:  "denom",
					Amount: "0",
				}},
			},
			expErr: true,
		},
		"unknown denom - insufficient funds": {
			msg: wasmvmtypes.BurnMsg{
				Amount: wasmvmtypes.Coins{{
					Denom:  "unknown",
					Amount: "1",
				}},
			},
			expErr: true,
		},
	}
	parentCtx := ctx
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			ctx, _ = parentCtx.CacheContext()
			k.wasmVM = &wasmtesting.MockWasmer{ExecuteFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, executeMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
				return &wasmvmtypes.Response{
					Messages: []wasmvmtypes.SubMsg{
						{Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{Burn: &spec.msg}}, ReplyOn: wasmvmtypes.ReplyNever},
					},
				}, 0, nil
			}}

			// when
			_, err = k.execute(ctx, example.Contract, example.CreatorAddr, nil, nil)

			// then
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// and total supply reduced by burned amount
			after, err := keepers.BankKeeper.TotalSupply(sdk.WrapSDKContext(ctx), &banktypes.QueryTotalSupplyRequest{})
			require.NoError(t, err)
			diff := before.Supply.Sub(after.Supply)
			assert.Equal(t, sdk.NewCoins(sdk.NewCoin("denom", sdk.NewInt(100))), diff)
		})
	}

	// test cases:
	// not enough money to burn
}
