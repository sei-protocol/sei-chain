package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestParams(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	keeper := app.AccessControlKeeper

	response, err := app.AccessControlKeeper.Params(sdk.WrapSDKContext(ctx), &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, keeper.GetParams(ctx), response.Params)
}

func TestResourceDependencyMappingFromMessageKey(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	keeper := app.AccessControlKeeper
	response, err := keeper.ResourceDependencyMappingFromMessageKey(
		sdk.WrapSDKContext(ctx),
		&types.ResourceDependencyMappingFromMessageKeyRequest{MessageKey: "key"},
	)

	require.NoError(t, err)
	require.Equal(t, keeper.GetResourceDependencyMapping(ctx, types.MessageKey("key")), response.MessageDependencyMapping)
}

func TestWasmDependencyMappingCall(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	keeper := app.AccessControlKeeper

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 2, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    &acltypes.AccessOperation{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	_, err = keeper.WasmDependencyMapping(sdk.WrapSDKContext(ctx), &types.WasmDependencyMappingRequest{ContractAddress: wasmContractAddress.String()})
	require.NoError(t, err)

	_, err = keeper.WasmDependencyMapping(sdk.WrapSDKContext(ctx), &types.WasmDependencyMappingRequest{ContractAddress: "invalid"})
	require.Error(t, err)

	_, err = keeper.WasmDependencyMapping(sdk.WrapSDKContext(ctx), &types.WasmDependencyMappingRequest{ContractAddress: wasmContractAddresses[1].String()})
	require.Error(t, err)
}

func TestListResourceDependencyMapping(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	testDependencyMapping := acltypes.MessageDependencyMapping{
		MessageKey: "testKey",
		AccessOps: []acltypes.AccessOperation{
			{
				ResourceType:       acltypes.ResourceType_KV_EPOCH,
				AccessType:         acltypes.AccessType_READ,
				IdentifierTemplate: "someIdentifier",
			},
			*types.CommitAccessOp(),
		},
	}
	err := app.AccessControlKeeper.SetResourceDependencyMapping(ctx, testDependencyMapping)
	require.NoError(t, err)

	keeper := app.AccessControlKeeper
	result, err := keeper.ListResourceDependencyMapping(sdk.WrapSDKContext(ctx), &types.ListResourceDependencyMappingRequest{})
	require.NoError(t, err)
	require.Len(t, result.MessageDependencyMappingList, 1)
}

func TestListWasmDependencyMapping(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 2, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    &acltypes.AccessOperation{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	keeper := app.AccessControlKeeper
	result, err := keeper.ListWasmDependencyMapping(sdk.WrapSDKContext(ctx), &types.ListWasmDependencyMappingRequest{})
	require.NoError(t, err)
	require.Len(t, result.WasmDependencyMappingList, 1)
}
