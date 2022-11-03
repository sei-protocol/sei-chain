package wasmbinding

import (
	"encoding/json"
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

type MockMessenger struct{}

func (m MockMessenger) DispatchMsg(ctx sdk.Context, contractAddr sdk.AccAddress, contractIBCPortID string, msg wasmvmtypes.CosmosMsg) (events []sdk.Event, data [][]byte, err error) {
	return []sdk.Event{{
		Type:       "test",
		Attributes: []abci.EventAttribute{},
	}}, nil, nil
}

func TestMessageHandlerDependencyDecorator(t *testing.T) {
	app := app.Setup(false)
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	defaultEncoders := wasmkeeper.DefaultEncoders(app.AppCodec(), app.TransferKeeper)
	dependencyDecorator := wasmbinding.NewSDKMessageDependencyDecorator(MockMessenger{}, app.AccessControlKeeper, defaultEncoders)
	testContext := app.NewContext(false, types.Header{})

	// setup bank send message with aclkeeper
	app.AccessControlKeeper.SetResourceDependencyMapping(testContext, sdkacltypes.MessageDependencyMapping{
		MessageKey: string(acltypes.GenerateMessageKey(&banktypes.MsgSend{})),
		AccessOps: []sdkacltypes.AccessOperation{
			{
				AccessType:         sdkacltypes.AccessType_READ,
				ResourceType:       sdkacltypes.ResourceType_KV,
				IdentifierTemplate: "*",
			},
			*acltypes.CommitAccessOp(),
		},
		DynamicEnabled: false,
	})

	// setup the wasm contract's dependency mapping
	app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, sdkacltypes.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []sdkacltypes.AccessOperationWithSelector{
			{
				Operation: &sdkacltypes.AccessOperation{
					AccessType:         sdkacltypes.AccessType_WRITE,
					ResourceType:       sdkacltypes.ResourceType_ANY,
					IdentifierTemplate: "*",
				},
			}, {
				Operation: acltypes.CommitAccessOp(),
			},
		},
	})

	events, _, _ := dependencyDecorator.DispatchMsg(testContext, contractAddr, "test", wasmvmtypes.CosmosMsg{
		Bank: &wasmvmtypes.BankMsg{
			Send: &wasmvmtypes.SendMsg{
				ToAddress: "sdfasdf",
				Amount: []wasmvmtypes.Coin{
					{
						Denom:  "usei",
						Amount: "12345",
					},
				},
			},
		},
	})
	// we should have received the test event
	require.Equal(t, []sdk.Event{
		{
			Type:       "test",
			Attributes: []abci.EventAttribute{},
		},
	}, events)

	app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, sdkacltypes.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []sdkacltypes.AccessOperationWithSelector{
			{
				Operation: &sdkacltypes.AccessOperation{
					AccessType:         sdkacltypes.AccessType_WRITE,
					ResourceType:       sdkacltypes.ResourceType_KV,
					IdentifierTemplate: "otherIdentifier",
				},
			}, {
				Operation: acltypes.CommitAccessOp(),
			},
		},
	})

	_, _, err = dependencyDecorator.DispatchMsg(testContext, contractAddr, "test", wasmvmtypes.CosmosMsg{
		Bank: &wasmvmtypes.BankMsg{
			Send: &wasmvmtypes.SendMsg{
				ToAddress: "sdfasdf",
				Amount: []wasmvmtypes.Coin{
					{
						Denom:  "usei",
						Amount: "12345",
					},
				},
			},
		},
	})
	// we expect an error now
	require.Error(t, accesscontrol.ErrUnexpectedWasmDependency, err)

	// reenable wasm mapping that's correct
	app.AccessControlKeeper.SetWasmDependencyMapping(testContext, contractAddr, sdkacltypes.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []sdkacltypes.AccessOperationWithSelector{
			{
				Operation: &sdkacltypes.AccessOperation{
					AccessType:         sdkacltypes.AccessType_WRITE,
					ResourceType:       sdkacltypes.ResourceType_KV,
					IdentifierTemplate: "*",
				},
			}, {
				Operation: acltypes.CommitAccessOp(),
			},
		},
	})
	// lets try with a message that wont decode properly
	_, _, err = dependencyDecorator.DispatchMsg(testContext, contractAddr, "test", wasmvmtypes.CosmosMsg{
		Custom: json.RawMessage{},
	})
	require.Error(t, wasmtypes.ErrUnknownMsg, err)
}
