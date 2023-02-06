package keeper

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	wasmvm "github.com/CosmWasm/wasmvm"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CosmWasm/wasmd/x/wasm/keeper/wasmtesting"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

func TestOnOpenChannel(t *testing.T) {
	var m wasmtesting.MockWasmer
	wasmtesting.MakeIBCInstantiable(&m)
	messenger := &wasmtesting.MockMessageHandler{}
	parentCtx, keepers := CreateTestInput(t, false, SupportedFeatures, WithMessageHandler(messenger))
	example := SeedNewContractInstance(t, parentCtx, keepers, &m)
	const myContractGas = 40

	specs := map[string]struct {
		contractAddr sdk.AccAddress
		contractGas  sdk.Gas
		contractErr  error
		expGas       uint64
		expErr       bool
	}{
		"consume contract gas": {
			contractAddr: example.Contract,
			contractGas:  myContractGas,
			expGas:       myContractGas,
		},
		"consume max gas": {
			contractAddr: example.Contract,
			contractGas:  math.MaxUint64 / DefaultGasMultiplier,
			expGas:       math.MaxUint64 / DefaultGasMultiplier,
		},
		"consume gas on error": {
			contractAddr: example.Contract,
			contractGas:  myContractGas,
			contractErr:  errors.New("test, ignore"),
			expErr:       true,
		},
		"unknown contract address": {
			contractAddr: RandomAccountAddress(t),
			expErr:       true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			myChannel := wasmvmtypes.IBCChannel{Version: "my test channel"}
			myMsg := wasmvmtypes.IBCChannelOpenMsg{OpenTry: &wasmvmtypes.IBCOpenTry{Channel: myChannel, CounterpartyVersion: "foo"}}
			m.IBCChannelOpenFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCChannelOpenMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBC3ChannelOpenResponse, uint64, error) {
				assert.Equal(t, myMsg, msg)
				return &wasmvmtypes.IBC3ChannelOpenResponse{}, spec.contractGas * DefaultGasMultiplier, spec.contractErr
			}

			ctx, _ := parentCtx.CacheContext()
			before := ctx.GasMeter().GasConsumed()

			// when
			msg := wasmvmtypes.IBCChannelOpenMsg{
				OpenTry: &wasmvmtypes.IBCOpenTry{
					Channel:             myChannel,
					CounterpartyVersion: "foo",
				},
			}
			_, err := keepers.WasmKeeper.OnOpenChannel(ctx, spec.contractAddr, msg)

			// then
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// verify gas consumed
			const storageCosts = sdk.Gas(2903)
			assert.Equal(t, spec.expGas, ctx.GasMeter().GasConsumed()-before-storageCosts)
		})
	}
}

func TestOnConnectChannel(t *testing.T) {
	var m wasmtesting.MockWasmer
	wasmtesting.MakeIBCInstantiable(&m)
	messenger := &wasmtesting.MockMessageHandler{}
	parentCtx, keepers := CreateTestInput(t, false, SupportedFeatures, WithMessageHandler(messenger))
	example := SeedNewContractInstance(t, parentCtx, keepers, &m)
	const myContractGas = 40

	specs := map[string]struct {
		contractAddr       sdk.AccAddress
		contractResp       *wasmvmtypes.IBCBasicResponse
		contractErr        error
		overwriteMessenger *wasmtesting.MockMessageHandler
		expContractGas     sdk.Gas
		expErr             bool
		expEventTypes      []string
	}{
		"consume contract gas": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp:   &wasmvmtypes.IBCBasicResponse{},
		},
		"consume gas on error, ignore events + messages": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			contractErr: errors.New("test, ignore"),
			expErr:      true,
		},
		"dispatch contract messages on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages: []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
			},
		},
		"emit contract events on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			expEventTypes: []string{types.WasmModuleEventType},
		},
		"messenger errors returned, events stored": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			overwriteMessenger: wasmtesting.NewErroringMessageHandler(),
			expErr:             true,
			expEventTypes:      []string{types.WasmModuleEventType},
		},
		"unknown contract address": {
			contractAddr: RandomAccountAddress(t),
			expErr:       true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			myChannel := wasmvmtypes.IBCChannel{Version: "my test channel"}
			myMsg := wasmvmtypes.IBCChannelConnectMsg{OpenConfirm: &wasmvmtypes.IBCOpenConfirm{Channel: myChannel}}
			m.IBCChannelConnectFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCChannelConnectMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
				assert.Equal(t, msg, myMsg)
				return spec.contractResp, myContractGas * DefaultGasMultiplier, spec.contractErr
			}

			ctx, _ := parentCtx.CacheContext()
			ctx = ctx.WithEventManager(sdk.NewEventManager())

			before := ctx.GasMeter().GasConsumed()
			msger, capturedMsgs := wasmtesting.NewCapturingMessageHandler()
			*messenger = *msger
			if spec.overwriteMessenger != nil {
				*messenger = *spec.overwriteMessenger
			}

			// when
			msg := wasmvmtypes.IBCChannelConnectMsg{
				OpenConfirm: &wasmvmtypes.IBCOpenConfirm{
					Channel: myChannel,
				},
			}
			err := keepers.WasmKeeper.OnConnectChannel(ctx, spec.contractAddr, msg)

			// then
			if spec.expErr {
				require.Error(t, err)
				assert.Empty(t, capturedMsgs) // no messages captured on error
				assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
				return
			}
			require.NoError(t, err)
			// verify gas consumed
			const storageCosts = sdk.Gas(2903)
			assert.Equal(t, spec.expContractGas, ctx.GasMeter().GasConsumed()-before-storageCosts)
			// verify msgs dispatched
			require.Len(t, *capturedMsgs, len(spec.contractResp.Messages))
			for i, m := range spec.contractResp.Messages {
				assert.Equal(t, (*capturedMsgs)[i], m.Msg)
			}
			assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
		})
	}
}

func TestOnCloseChannel(t *testing.T) {
	var m wasmtesting.MockWasmer
	wasmtesting.MakeIBCInstantiable(&m)
	messenger := &wasmtesting.MockMessageHandler{}
	parentCtx, keepers := CreateTestInput(t, false, SupportedFeatures, WithMessageHandler(messenger))
	example := SeedNewContractInstance(t, parentCtx, keepers, &m)
	const myContractGas = 40

	specs := map[string]struct {
		contractAddr       sdk.AccAddress
		contractResp       *wasmvmtypes.IBCBasicResponse
		contractErr        error
		overwriteMessenger *wasmtesting.MockMessageHandler
		expContractGas     sdk.Gas
		expErr             bool
		expEventTypes      []string
	}{
		"consume contract gas": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp:   &wasmvmtypes.IBCBasicResponse{},
		},
		"consume gas on error, ignore events + messages": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			contractErr: errors.New("test, ignore"),
			expErr:      true,
		},
		"dispatch contract messages on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages: []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
			},
		},
		"emit contract events on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			expEventTypes: []string{types.WasmModuleEventType},
		},
		"messenger errors returned, events stored": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			overwriteMessenger: wasmtesting.NewErroringMessageHandler(),
			expErr:             true,
			expEventTypes:      []string{types.WasmModuleEventType},
		},
		"unknown contract address": {
			contractAddr: RandomAccountAddress(t),
			expErr:       true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			myChannel := wasmvmtypes.IBCChannel{Version: "my test channel"}
			myMsg := wasmvmtypes.IBCChannelCloseMsg{CloseInit: &wasmvmtypes.IBCCloseInit{Channel: myChannel}}
			m.IBCChannelCloseFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCChannelCloseMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
				assert.Equal(t, msg, myMsg)
				return spec.contractResp, myContractGas * DefaultGasMultiplier, spec.contractErr
			}

			ctx, _ := parentCtx.CacheContext()
			before := ctx.GasMeter().GasConsumed()
			msger, capturedMsgs := wasmtesting.NewCapturingMessageHandler()
			*messenger = *msger

			if spec.overwriteMessenger != nil {
				*messenger = *spec.overwriteMessenger
			}

			// when
			msg := wasmvmtypes.IBCChannelCloseMsg{
				CloseInit: &wasmvmtypes.IBCCloseInit{
					Channel: myChannel,
				},
			}
			err := keepers.WasmKeeper.OnCloseChannel(ctx, spec.contractAddr, msg)

			// then
			if spec.expErr {
				require.Error(t, err)
				assert.Empty(t, capturedMsgs) // no messages captured on error
				assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
				return
			}
			require.NoError(t, err)
			// verify gas consumed
			const storageCosts = sdk.Gas(2903)
			assert.Equal(t, spec.expContractGas, ctx.GasMeter().GasConsumed()-before-storageCosts)
			// verify msgs dispatched
			require.Len(t, *capturedMsgs, len(spec.contractResp.Messages))
			for i, m := range spec.contractResp.Messages {
				assert.Equal(t, (*capturedMsgs)[i], m.Msg)
			}
			assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
		})
	}
}

func TestOnRecvPacket(t *testing.T) {
	var m wasmtesting.MockWasmer
	wasmtesting.MakeIBCInstantiable(&m)
	messenger := &wasmtesting.MockMessageHandler{}
	parentCtx, keepers := CreateTestInput(t, false, SupportedFeatures, WithMessageHandler(messenger))
	example := SeedNewContractInstance(t, parentCtx, keepers, &m)
	const myContractGas = 40
	const storageCosts = sdk.Gas(2903)

	specs := map[string]struct {
		contractAddr       sdk.AccAddress
		contractResp       *wasmvmtypes.IBCReceiveResponse
		contractErr        error
		overwriteMessenger *wasmtesting.MockMessageHandler
		mockReplyFn        func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error)
		expContractGas     sdk.Gas
		expAck             []byte
		expErr             bool
		expEventTypes      []string
	}{
		"consume contract gas": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCReceiveResponse{
				Acknowledgement: []byte("myAck"),
			},
			expAck: []byte("myAck"),
		},
		"can return empty ack": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp:   &wasmvmtypes.IBCReceiveResponse{},
		},
		"consume gas on error, ignore events + messages": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCReceiveResponse{
				Acknowledgement: []byte("myAck"),
				Messages:        []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}},
				Attributes:      []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			contractErr: errors.New("test, ignore"),
			expErr:      true,
		},
		"dispatch contract messages on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCReceiveResponse{
				Acknowledgement: []byte("myAck"),
				Messages:        []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
			},
			expAck: []byte("myAck"),
		},
		"emit contract attributes on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCReceiveResponse{
				Acknowledgement: []byte("myAck"),
				Attributes:      []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			expEventTypes: []string{types.WasmModuleEventType},
			expAck:        []byte("myAck"),
		},
		"emit contract events on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 46, // charge or custom event as well
			contractResp: &wasmvmtypes.IBCReceiveResponse{
				Acknowledgement: []byte("myAck"),
				Attributes:      []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
				Events: []wasmvmtypes.Event{{
					Type: "custom",
					Attributes: []wasmvmtypes.EventAttribute{{
						Key:   "message",
						Value: "to rudi",
					}},
				}},
			},
			expEventTypes: []string{types.WasmModuleEventType, "wasm-custom"},
			expAck:        []byte("myAck"),
		},
		"messenger errors returned, events stored": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCReceiveResponse{
				Acknowledgement: []byte("myAck"),
				Messages:        []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
				Attributes:      []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			overwriteMessenger: wasmtesting.NewErroringMessageHandler(),
			expErr:             true,
			expEventTypes:      []string{types.WasmModuleEventType},
		},
		"submessage reply can overwrite ack data": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + storageCosts,
			contractResp: &wasmvmtypes.IBCReceiveResponse{
				Acknowledgement: []byte("myAck"),
				Messages:        []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyAlways, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}},
			},
			mockReplyFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
				return &wasmvmtypes.Response{Data: []byte("myBetterAck")}, 0, nil
			},
			expAck:        []byte("myBetterAck"),
			expEventTypes: []string{types.EventTypeReply},
		},
		"unknown contract address": {
			contractAddr: RandomAccountAddress(t),
			expErr:       true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			myPacket := wasmvmtypes.IBCPacket{Data: []byte("my data")}

			m.IBCPacketReceiveFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketReceiveMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCReceiveResult, uint64, error) {
				assert.Equal(t, myPacket, msg.Packet)
				return &wasmvmtypes.IBCReceiveResult{Ok: spec.contractResp}, myContractGas * DefaultGasMultiplier, spec.contractErr
			}
			if spec.mockReplyFn != nil {
				m.ReplyFn = spec.mockReplyFn
				h, ok := keepers.WasmKeeper.wasmVMResponseHandler.(*DefaultWasmVMContractResponseHandler)
				require.True(t, ok)
				h.md = NewMessageDispatcher(messenger, keepers.WasmKeeper)
			}

			ctx, _ := parentCtx.CacheContext()
			before := ctx.GasMeter().GasConsumed()

			msger, capturedMsgs := wasmtesting.NewCapturingMessageHandler()
			*messenger = *msger

			if spec.overwriteMessenger != nil {
				*messenger = *spec.overwriteMessenger
			}

			// when
			msg := wasmvmtypes.IBCPacketReceiveMsg{Packet: myPacket}
			gotAck, err := keepers.WasmKeeper.OnRecvPacket(ctx, spec.contractAddr, msg)

			// then
			if spec.expErr {
				require.Error(t, err)
				assert.Empty(t, capturedMsgs) // no messages captured on error
				assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
				return
			}
			require.NoError(t, err)
			require.Equal(t, spec.expAck, gotAck)

			// verify gas consumed
			const storageCosts = sdk.Gas(2903)
			assert.Equal(t, spec.expContractGas, ctx.GasMeter().GasConsumed()-before-storageCosts)
			// verify msgs dispatched
			require.Len(t, *capturedMsgs, len(spec.contractResp.Messages))
			for i, m := range spec.contractResp.Messages {
				assert.Equal(t, (*capturedMsgs)[i], m.Msg)
			}
			assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
		})
	}
}

func TestOnAckPacket(t *testing.T) {
	var m wasmtesting.MockWasmer
	wasmtesting.MakeIBCInstantiable(&m)
	messenger := &wasmtesting.MockMessageHandler{}
	parentCtx, keepers := CreateTestInput(t, false, SupportedFeatures, WithMessageHandler(messenger))
	example := SeedNewContractInstance(t, parentCtx, keepers, &m)
	const myContractGas = 40

	specs := map[string]struct {
		contractAddr       sdk.AccAddress
		contractResp       *wasmvmtypes.IBCBasicResponse
		contractErr        error
		overwriteMessenger *wasmtesting.MockMessageHandler
		expContractGas     sdk.Gas
		expErr             bool
		expEventTypes      []string
	}{
		"consume contract gas": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp:   &wasmvmtypes.IBCBasicResponse{},
		},
		"consume gas on error, ignore events + messages": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			contractErr: errors.New("test, ignore"),
			expErr:      true,
		},
		"dispatch contract messages on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages: []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
			},
		},
		"emit contract events on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			expEventTypes: []string{types.WasmModuleEventType},
		},
		"messenger errors returned, events stored": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			overwriteMessenger: wasmtesting.NewErroringMessageHandler(),
			expErr:             true,
			expEventTypes:      []string{types.WasmModuleEventType},
		},
		"unknown contract address": {
			contractAddr: RandomAccountAddress(t),
			expErr:       true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			myAck := wasmvmtypes.IBCPacketAckMsg{Acknowledgement: wasmvmtypes.IBCAcknowledgement{Data: []byte("myAck")}}
			m.IBCPacketAckFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketAckMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
				assert.Equal(t, myAck, msg)
				return spec.contractResp, myContractGas * DefaultGasMultiplier, spec.contractErr
			}

			ctx, _ := parentCtx.CacheContext()
			before := ctx.GasMeter().GasConsumed()
			msger, capturedMsgs := wasmtesting.NewCapturingMessageHandler()
			*messenger = *msger

			if spec.overwriteMessenger != nil {
				*messenger = *spec.overwriteMessenger
			}

			// when
			err := keepers.WasmKeeper.OnAckPacket(ctx, spec.contractAddr, myAck)

			// then

			if spec.expErr {
				require.Error(t, err)
				assert.Empty(t, capturedMsgs) // no messages captured on error
				assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
				return
			}
			require.NoError(t, err)
			// verify gas consumed
			const storageCosts = sdk.Gas(2903)
			assert.Equal(t, spec.expContractGas, ctx.GasMeter().GasConsumed()-before-storageCosts)
			// verify msgs dispatched
			require.Len(t, *capturedMsgs, len(spec.contractResp.Messages))
			for i, m := range spec.contractResp.Messages {
				assert.Equal(t, (*capturedMsgs)[i], m.Msg)
			}
			assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
		})
	}
}

func TestOnTimeoutPacket(t *testing.T) {
	var m wasmtesting.MockWasmer
	wasmtesting.MakeIBCInstantiable(&m)
	messenger := &wasmtesting.MockMessageHandler{}
	parentCtx, keepers := CreateTestInput(t, false, SupportedFeatures, WithMessageHandler(messenger))
	example := SeedNewContractInstance(t, parentCtx, keepers, &m)
	const myContractGas = 40

	specs := map[string]struct {
		contractAddr       sdk.AccAddress
		contractResp       *wasmvmtypes.IBCBasicResponse
		contractErr        error
		overwriteMessenger *wasmtesting.MockMessageHandler
		expContractGas     sdk.Gas
		expErr             bool
		expEventTypes      []string
	}{
		"consume contract gas": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp:   &wasmvmtypes.IBCBasicResponse{},
		},
		"consume gas on error, ignore events + messages": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			contractErr: errors.New("test, ignore"),
			expErr:      true,
		},
		"dispatch contract messages on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages: []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
			},
		},
		"emit contract attributes on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			expEventTypes: []string{types.WasmModuleEventType},
		},
		"emit contract events on success": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 46, // cost for custom events
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
				Events: []wasmvmtypes.Event{{
					Type: "custom",
					Attributes: []wasmvmtypes.EventAttribute{{
						Key:   "message",
						Value: "to rudi",
					}},
				}},
			},
			expEventTypes: []string{types.WasmModuleEventType, "wasm-custom"},
		},
		"messenger errors returned, events stored before": {
			contractAddr:   example.Contract,
			expContractGas: myContractGas + 10,
			contractResp: &wasmvmtypes.IBCBasicResponse{
				Messages:   []wasmvmtypes.SubMsg{{ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{}}}, {ReplyOn: wasmvmtypes.ReplyNever, Msg: wasmvmtypes.CosmosMsg{Custom: json.RawMessage(`{"foo":"bar"}`)}}},
				Attributes: []wasmvmtypes.EventAttribute{{Key: "Foo", Value: "Bar"}},
			},
			overwriteMessenger: wasmtesting.NewErroringMessageHandler(),
			expErr:             true,
			expEventTypes:      []string{types.WasmModuleEventType},
		},
		"unknown contract address": {
			contractAddr: RandomAccountAddress(t),
			expErr:       true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			myPacket := wasmvmtypes.IBCPacket{Data: []byte("my test packet")}
			m.IBCPacketTimeoutFn = func(codeID wasmvm.Checksum, env wasmvmtypes.Env, msg wasmvmtypes.IBCPacketTimeoutMsg, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.IBCBasicResponse, uint64, error) {
				assert.Equal(t, myPacket, msg.Packet)
				return spec.contractResp, myContractGas * DefaultGasMultiplier, spec.contractErr
			}

			ctx, _ := parentCtx.CacheContext()
			before := ctx.GasMeter().GasConsumed()
			msger, capturedMsgs := wasmtesting.NewCapturingMessageHandler()
			*messenger = *msger

			if spec.overwriteMessenger != nil {
				*messenger = *spec.overwriteMessenger
			}

			// when
			msg := wasmvmtypes.IBCPacketTimeoutMsg{Packet: myPacket}
			err := keepers.WasmKeeper.OnTimeoutPacket(ctx, spec.contractAddr, msg)

			// then
			if spec.expErr {
				require.Error(t, err)
				assert.Empty(t, capturedMsgs) // no messages captured on error
				assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
				return
			}
			require.NoError(t, err)
			// verify gas consumed
			const storageCosts = sdk.Gas(2903)
			assert.Equal(t, spec.expContractGas, ctx.GasMeter().GasConsumed()-before-storageCosts)
			// verify msgs dispatched
			require.Len(t, *capturedMsgs, len(spec.contractResp.Messages))
			for i, m := range spec.contractResp.Messages {
				assert.Equal(t, (*capturedMsgs)[i], m.Msg)
			}
			assert.Equal(t, spec.expEventTypes, stripTypes(ctx.EventManager().Events()))
		})
	}
}

func stripTypes(events sdk.Events) []string {
	var r []string
	for _, e := range events {
		r = append(r, e.Type)
	}
	return r
}
