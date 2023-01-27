package keeper_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/types/address"
	aclkeeper "github.com/cosmos/cosmos-sdk/x/accesscontrol/keeper"
	acltestutil "github.com/cosmos/cosmos-sdk/x/accesscontrol/testutil"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type KeeperTestSuite struct {
	suite.Suite

	app         *simapp.SimApp
	ctx         sdk.Context
	queryClient types.QueryClient
	addrs       []sdk.AccAddress
}

func (suite *KeeperTestSuite) SetupTest() {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, app.InterfaceRegistry())
	types.RegisterQueryServer(queryHelper, app.AccessControlKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	suite.app = app
	suite.ctx = ctx
	suite.queryClient = queryClient
	suite.addrs = simapp.AddTestAddrsIncremental(app, ctx, 2, sdk.NewInt(30000000))
}

func TestResourceDependencyMapping(t *testing.T) {
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
	invalidDependencyMapping := acltypes.MessageDependencyMapping{
		MessageKey: "invalidKey",
		AccessOps: []acltypes.AccessOperation{
			{
				ResourceType:       acltypes.ResourceType_ANY,
				AccessType:         acltypes.AccessType_READ,
				IdentifierTemplate: "*",
			},
		},
	}
	err := app.AccessControlKeeper.SetResourceDependencyMapping(ctx, testDependencyMapping)
	require.NoError(t, err)
	// we expect an error due to failed validation
	err = app.AccessControlKeeper.SetResourceDependencyMapping(ctx, invalidDependencyMapping)
	require.Error(t, types.ErrNoCommitAccessOp, err)
	// test simple get
	mapping := app.AccessControlKeeper.GetResourceDependencyMapping(ctx, "testKey")
	require.Equal(t, testDependencyMapping, mapping)
	// test get on key not present - we expect synchronousMappning because of invalid Set
	mapping = app.AccessControlKeeper.GetResourceDependencyMapping(ctx, "invalidKey")
	require.Equal(t, types.SynchronousMessageDependencyMapping("invalidKey"), mapping)

	// if we iterate, we should only get 1 value
	counter := 0
	app.AccessControlKeeper.IterateResourceKeys(ctx, func(dependencyMapping acltypes.MessageDependencyMapping) (stop bool) {
		counter++
		return false
	})
	require.Equal(t, 1, counter)
}

func TestWasmDependencyMapping(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 2, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	otherContractAddress := wasmContractAddresses[1]
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
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping for something function that isn't present
	_, err = app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, otherContractAddress)
	require.Error(t, aclkeeper.ErrWasmDependencyMappingNotFound, err)
}

func TestWasmDependencyMappingWithExecuteMsgInfo(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ExecuteAccessOps: []*acltypes.WasmAccessOperations{
			{
				MessageName: "test",
				WasmOperations: []*acltypes.WasmAccessOperation{
					{
						Operation:    &acltypes.AccessOperation{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
						SelectorType: acltypes.AccessOperationSelectorType_NONE,
					},
				},
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the access operations
	info, _ := types.NewExecuteMessageInfo(
		[]byte("{\"test\":{}}"),
	)
	ops, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(ctx, wasmContractAddress, "", info, make(aclkeeper.ContractReferenceLookupMap))
	require.NoError(t, err)

	expectedAccessOps := []acltypes.AccessOperation{
		{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
		*types.CommitAccessOp(),
	}
	require.Equal(t, ops, expectedAccessOps)
}

func TestWasmDependencyMappingWithQueryMsgInfo(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		QueryAccessOps: []*acltypes.WasmAccessOperations{
			{
				MessageName: "test",
				WasmOperations: []*acltypes.WasmAccessOperation{
					{
						Operation:    &acltypes.AccessOperation{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
						SelectorType: acltypes.AccessOperationSelectorType_NONE,
					},
				},
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the access operations
	info, _ := types.NewQueryMessageInfo(
		[]byte("{\"test\":{}}"),
	)
	ops, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(ctx, wasmContractAddress, "", info, make(aclkeeper.ContractReferenceLookupMap))
	require.NoError(t, err)

	expectedAccessOps := []acltypes.AccessOperation{
		{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
		*types.CommitAccessOp(),
	}
	require.Equal(t, ops, expectedAccessOps)
}

func TestResetWasmDependencyMapping(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource",
				},
			}, {
				Operation: types.CommitAccessOp(),
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test resetting
	err = app.AccessControlKeeper.ResetWasmDependencyMapping(ctx, wasmContractAddress, "some reason")
	require.NoError(t, err)
	mapping, err = app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, types.SynchronousWasmAccessOps(), mapping.BaseAccessOps)
	require.Equal(t, "some reason", mapping.ResetReason)
}

func TestWasmDependencyMappingWithJQSelector(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: wasmContractAddress.String() + "/%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ,
				Selector:     ".send.from",
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: wasmContractAddress.String() + "/%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ,
				Selector:     ".receive.amount",
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: wasmContractAddress.String() + "/%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ,
				Selector:     ".send.amount",
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
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping with selector
	info, _ := types.NewExecuteMessageInfo([]byte("{\"send\":{\"from\":\"bob\",\"amount\":10}}"))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		"",
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.True(t, types.NewAccessOperationSet(deps).HasIdentifier(fmt.Sprintf("%s/%s", wasmContractAddress.String(), hex.EncodeToString([]byte("bob")))))
	require.True(t, types.NewAccessOperationSet(deps).HasIdentifier(fmt.Sprintf("%s/%s", wasmContractAddress.String(), hex.EncodeToString([]byte("10")))))
}

func TestWasmDependencyMappingWithJQBech32Selector(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "someprefix%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_BECH32_ADDRESS,
				Selector:     ".send.address",
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "someprefix%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_BECH32_ADDRESS,
				Selector:     ".receive.address",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping with selector
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", wasmBech32)))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		"",
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.True(t, types.NewAccessOperationSet(deps).HasIdentifier(fmt.Sprintf("someprefix%s", hex.EncodeToString(wasmContractAddress))))
}

func TestWasmDependencyMappingWithJQLengthPrefixedAddressSelector(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "someprefix%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS,
				Selector:     ".send.address",
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "someprefix%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS,
				Selector:     ".receive.address",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping with selector
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", wasmBech32)))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		"",
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.True(t, types.NewAccessOperationSet(deps).HasIdentifier(fmt.Sprintf("someprefix%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress)))))
}

func TestWasmDependencyMappingWithSenderBech32Selector(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "someprefix%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_BECH32_ADDRESS,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping with selector
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", wasmBech32)))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		wasmBech32,
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.True(t, types.NewAccessOperationSet(deps).HasIdentifier(fmt.Sprintf("someprefix%s", hex.EncodeToString(wasmContractAddress))))
}

func TestWasmDependencyMappingWithSenderLengthPrefixedSelector(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "someprefix%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping with selector
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", wasmBech32)))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		wasmBech32,
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.True(t, types.NewAccessOperationSet(deps).HasIdentifier(fmt.Sprintf("someprefix%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress)))))
}

func TestWasmDependencyMappingWithConditionalSelector(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "*",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_MESSAGE_CONDITIONAL,
				Selector:     ".send",
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "*",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_MESSAGE_CONDITIONAL,
				Selector:     ".other_execute",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping with selector
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", wasmBech32)))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		wasmBech32,
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 2, types.NewAccessOperationSet(deps).Size())
	require.True(t, types.NewAccessOperationSet(deps).HasResourceType(acltypes.ResourceType_KV_WASM))
}

func TestWasmDependencyMappingWithConstantSelector(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_WASM,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "prefix%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_CONSTANT_STRING_TO_HEX,
				Selector:     "constantValue",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)
	// test getting a dependency mapping with selector
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", wasmBech32)))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		wasmBech32,
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.True(t, types.NewAccessOperationSet(deps).HasIdentifier(fmt.Sprintf("prefix%s", hex.EncodeToString([]byte("constantValue")))))
}

func TestWasmDependencyMappingWithContractReference(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 3, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	thirdAddr := wasmContractAddresses[2]

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	interContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS,
				Selector:     ".send.address",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: interContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, interContractMapping)
	require.NoError(t, err)

	// this mapping creates a reference to the inter-contract dependency
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			// this one should be appropriately discarded because we are not processing a contract reference
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS,
				Selector:     ".field.doesnt.exist",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: interContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "some_message",
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", thirdAddr.String())))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		thirdAddr.String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 3, types.NewAccessOperationSet(deps).Size())
	expectedAccessOps := []acltypes.AccessOperation{
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress))),
		},
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: "*",
		},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps), types.NewAccessOperationSet(deps))
}

func TestWasmDependencyMappingWithContractReferenceWasmTranslator(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 3, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	thirdAddr := wasmContractAddresses[2]

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	interContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS,
				Selector:     ".some_message.address",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: interContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, interContractMapping)
	require.NoError(t, err)

	// this mapping creates a reference to the inter-contract dependency
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			// this one should be appropriately discarded because we are not processing a contract reference
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS,
				Selector:     ".field.doesnt.exist",
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ExecuteContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "send",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress:         interContractAddress.String(),
						MessageType:             acltypes.WasmMessageSubtype_EXECUTE,
						MessageName:             "some_message",
						JsonTranslationTemplate: "{\"some_message\":{\"address\":\".send.address\"}}",
					},
				},
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", thirdAddr.String())))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		thirdAddr.String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 3, types.NewAccessOperationSet(deps).Size())
	expectedAccessOps := []acltypes.AccessOperation{
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress))),
		},
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(thirdAddr))),
		},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps), types.NewAccessOperationSet(deps))
}

func TestWasmDependencyMappingWithContractReferenceSelectorMultipleReferences(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 4, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	inter2ContractAddress := wasmContractAddresses[2]
	otherAddr := wasmContractAddresses[3]

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	interContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: inter2ContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "message_b",
			},
		},
		ContractAddress: interContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, interContractMapping)
	require.NoError(t, err)

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	inter2ContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
					AccessType:         acltypes.AccessType_READ,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: inter2ContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, inter2ContractMapping)
	require.NoError(t, err)

	// this mapping creates a reference to the inter-contract dependency
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: interContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "message_a",
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", otherAddr.String())))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		otherAddr.String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 3, types.NewAccessOperationSet(deps).Size())
	expectedAccessOps := []acltypes.AccessOperation{
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress))),
		},
		{
			ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "*",
		},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps), types.NewAccessOperationSet(deps))
}

func TestWasmDependencyMappingWithContractReferenceSelectorCircularDependency(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 4, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	inter2ContractAddress := wasmContractAddresses[2]
	otherAddr := wasmContractAddresses[3]

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	interContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: inter2ContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "message_b",
			},
		},
		ContractAddress: interContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, interContractMapping)
	require.NoError(t, err)

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	inter2ContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
					AccessType:         acltypes.AccessType_READ,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: wasmContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "send",
			},
		},
		ContractAddress: inter2ContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, inter2ContractMapping)
	require.NoError(t, err)

	// this mapping creates a reference to the inter-contract dependency
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: interContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "message_a",
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", otherAddr.String())))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		otherAddr.String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 4, types.NewAccessOperationSet(deps).Size())
	expectedAccessOps := []acltypes.AccessOperation{
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress))),
		},
		{
			ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "*",
		},
		// sync access ops after ORACLE READ due to circular dependency with wasmContract
		{AccessType: acltypes.AccessType_UNKNOWN, ResourceType: acltypes.ResourceType_ANY, IdentifierTemplate: "*"},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps), types.NewAccessOperationSet(deps))
}

func TestWasmDependencyMappingWithContractReferenceNonCircularDependency(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 4, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	inter2ContractAddress := wasmContractAddresses[2]
	otherAddr := wasmContractAddresses[3]

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	interContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: inter2ContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "message_b",
			},
		},
		ContractAddress: interContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, interContractMapping)
	require.NoError(t, err)

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	inter2ContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
					AccessType:         acltypes.AccessType_READ,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: wasmContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "other_msg",
			},
		},
		ContractAddress: inter2ContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, inter2ContractMapping)
	require.NoError(t, err)

	// this mapping creates a reference to the inter-contract dependency
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ExecuteContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "send",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: interContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
						MessageName:     "message_a",
					},
				},
			},
		},
		ExecuteAccessOps: []*acltypes.WasmAccessOperations{
			{
				MessageName: "other_msg",
				WasmOperations: []*acltypes.WasmAccessOperation{
					{
						Operation: &acltypes.AccessOperation{
							ResourceType:       acltypes.ResourceType_KV_STAKING,
							AccessType:         acltypes.AccessType_READ,
							IdentifierTemplate: "stakingIdentifier",
						},
						SelectorType: acltypes.AccessOperationSelectorType_NONE,
					},
				},
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", otherAddr.String())))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		otherAddr.String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 4, types.NewAccessOperationSet(deps).Size())
	expectedAccessOps := []acltypes.AccessOperation{
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress))),
		},
		{
			ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "*",
		},
		{
			ResourceType:       acltypes.ResourceType_KV_STAKING,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "stakingIdentifier",
		},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps), types.NewAccessOperationSet(deps))
}

func TestWasmDependencyMappingWithContractReferenceCircularDependencyWithContractOverlap(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 4, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	inter2ContractAddress := wasmContractAddresses[2]
	otherAddr := wasmContractAddresses[3]

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	interContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: inter2ContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "message_b",
			},
		},
		ContractAddress: interContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, interContractMapping)
	require.NoError(t, err)

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	inter2ContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
					AccessType:         acltypes.AccessType_READ,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: wasmContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "other_msg",
			},
		},
		ContractAddress: inter2ContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, inter2ContractMapping)
	require.NoError(t, err)

	// In this mapping, we will have a cycle that goes through the wasm contract because it goes via a different execute message,
	// but will get caught at interContract1 because of `message_a`
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ExecuteAccessOps: []*acltypes.WasmAccessOperations{
			{
				MessageName: "other_msg",
				WasmOperations: []*acltypes.WasmAccessOperation{
					{
						Operation: &acltypes.AccessOperation{
							ResourceType:       acltypes.ResourceType_KV_STAKING,
							AccessType:         acltypes.AccessType_READ,
							IdentifierTemplate: "stakingIdentifier",
						},
						SelectorType: acltypes.AccessOperationSelectorType_NONE,
					},
				},
			},
		},
		ExecuteContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "send",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: interContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
						MessageName:     "message_a",
					},
				},
			},
			{
				MessageName: "other_msg",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: interContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
						MessageName:     "message_a",
					},
				},
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", otherAddr.String())))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		otherAddr.String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 5, types.NewAccessOperationSet(deps).Size())
	expectedAccessOps := []acltypes.AccessOperation{
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress))),
		},
		{
			ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "*",
		},
		{
			ResourceType:       acltypes.ResourceType_KV_STAKING,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "stakingIdentifier",
		},
		{AccessType: acltypes.AccessType_UNKNOWN, ResourceType: acltypes.ResourceType_ANY, IdentifierTemplate: "*"},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps), types.NewAccessOperationSet(deps))
}

func TestWasmDependencyMappingWithContractReferenceDNE(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 3, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)

	// this mapping creates a reference to the inter-contract dependency
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		BaseContractReferences: []*acltypes.WasmContractReference{
			{
				ContractAddress: interContractAddress.String(),
				MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
				MessageName:     "some_message",
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", wasmBech32)))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		wasmContractAddresses[2].String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 2, types.NewAccessOperationSet(deps).Size())
	require.Equal(t, types.SynchronousAccessOpsSet(), types.NewAccessOperationSet(deps))
}

func TestWasmDependencyMappingWithContractReferencePartitioned(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 4, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	interContractAddress := wasmContractAddresses[1]
	inter2ContractAddress := wasmContractAddresses[2]
	otherAddr := wasmContractAddresses[3]

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	interContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "02%s",
				},
				SelectorType: acltypes.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: interContractAddress.String(),
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, interContractMapping)
	require.NoError(t, err)

	// create a dummy mapping of a bank balance write for the sender address (eg. performing some action like depositing funds)
	// also performs a bank write to an address specified by the JSON body (following same schema as contract A for now)
	inter2ContractMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_STAKING,
					AccessType:         acltypes.AccessType_WRITE,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
					AccessType:         acltypes.AccessType_UNKNOWN,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation: &acltypes.AccessOperation{
					ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
					AccessType:         acltypes.AccessType_READ,
					IdentifierTemplate: "*",
				},
			},
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ContractAddress: inter2ContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, inter2ContractMapping)
	require.NoError(t, err)

	// this mapping creates a reference to the inter-contract dependency
	wasmMapping := acltypes.WasmDependencyMapping{
		BaseAccessOps: []*acltypes.WasmAccessOperation{
			{
				Operation:    types.CommitAccessOp(),
				SelectorType: acltypes.AccessOperationSelectorType_NONE,
			},
		},
		ExecuteContractReferences: []*acltypes.WasmContractReferences{
			{
				MessageName: "send",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: interContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
						MessageName:     "message_a",
					},
				},
			},
			{
				MessageName: "other_msg",
				ContractReferences: []*acltypes.WasmContractReference{
					{
						ContractAddress: inter2ContractAddress.String(),
						MessageType:     acltypes.WasmMessageSubtype_QUERY,
						MessageName:     "message_b",
					},
				},
			},
		},
		ContractAddress: wasmContractAddress.String(),
	}
	// set the dependency mapping
	err = app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmMapping)
	require.NoError(t, err)

	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetRawWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, *mapping)

	// test getting a dependency mapping with selector that expands the inter-contract reference into the contract's dependencies
	require.NoError(t, err)
	info, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"send\":{\"address\":\"%s\",\"amount\":10}}", otherAddr.String())))
	deps, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		otherAddr.String(),
		info,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 2, types.NewAccessOperationSet(deps).Size())
	expectedAccessOps := []acltypes.AccessOperation{
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_WRITE,
			IdentifierTemplate: fmt.Sprintf("02%s", hex.EncodeToString(address.MustLengthPrefix(wasmContractAddress))),
		},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps), types.NewAccessOperationSet(deps))

	require.NoError(t, err)

	info2, _ := types.NewExecuteMessageInfo([]byte(fmt.Sprintf("{\"other_msg\":{\"address\":\"%s\",\"amount\":10}}", otherAddr.String())))
	deps2, err := app.AccessControlKeeper.GetWasmDependencyAccessOps(
		ctx,
		wasmContractAddress,
		otherAddr.String(),
		info2,
		make(aclkeeper.ContractReferenceLookupMap),
	)
	require.NoError(t, err)
	require.Equal(t, 3, types.NewAccessOperationSet(deps2).Size())
	expectedAccessOps2 := []acltypes.AccessOperation{
		// this was turned to READ from UNKNOWN
		{
			ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "*",
		},
		{
			ResourceType:       acltypes.ResourceType_KV_ORACLE_EXCHANGE_RATE,
			AccessType:         acltypes.AccessType_READ,
			IdentifierTemplate: "*",
		},
		*types.CommitAccessOp(),
	}
	require.Equal(t, types.NewAccessOperationSet(expectedAccessOps2), types.NewAccessOperationSet(deps2))
}

func TestContractReferenceAddressParser(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmBech32, err := sdk.Bech32ifyAddressBytes("cosmos", wasmContractAddress)
	require.NoError(t, err)

	msgInfo := types.WasmMessageInfo{
		MessageType:     acltypes.WasmMessageSubtype_EXECUTE,
		MessageName:     "some_name",
		MessageBody:     []byte("{\"test_msg\": {}}"),
		MessageFullBody: []byte(fmt.Sprintf("{\"test_msg\": {\"some_addr\": \"%s\"}}", wasmBech32)),
	}

	parsedLiteral := aclkeeper.ParseContractReferenceAddress(wasmBech32, "someSender", &msgInfo)
	require.Equal(t, wasmBech32, parsedLiteral)

	parsedSender := aclkeeper.ParseContractReferenceAddress("_sender", wasmBech32, &msgInfo)
	require.Equal(t, wasmBech32, parsedSender)

	parsedJQ := aclkeeper.ParseContractReferenceAddress(".test_msg.some_addr", wasmBech32, &msgInfo)
	require.Equal(t, wasmBech32, parsedJQ)

	parsedJQInvalid := aclkeeper.ParseContractReferenceAddress(".test_msg.other_addr", wasmBech32, &msgInfo)
	require.Equal(t, ".test_msg.other_addr", parsedJQInvalid)
}

func (suite *KeeperTestSuite) TestMessageDependencies() {
	suite.SetupTest()
	app := suite.app
	ctx := suite.ctx
	req := suite.Require()

	// setup bank send message
	bankSendMsg := banktypes.MsgSend{
		FromAddress: suite.addrs[0].String(),
		ToAddress:   suite.addrs[1].String(),
		Amount:      sdk.NewCoins(sdk.Coin{Denom: "usei", Amount: sdk.NewInt(10)}),
	}
	bankMsgKey := types.GenerateMessageKey(&bankSendMsg)

	// setup staking delegate msg
	stakingDelegate := stakingtypes.MsgDelegate{
		DelegatorAddress: suite.addrs[0].String(),
		ValidatorAddress: suite.addrs[1].String(),
		Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(10)},
	}
	delegateKey := types.GenerateMessageKey(&stakingDelegate)

	// setup staking dependency
	delegateStaticMapping := acltypes.MessageDependencyMapping{
		MessageKey: string(delegateKey),
		AccessOps: []acltypes.AccessOperation{
			{
				ResourceType:       acltypes.ResourceType_KV_STAKING_DELEGATION,
				AccessType:         acltypes.AccessType_WRITE,
				IdentifierTemplate: "stakingPrefix",
			},
			{
				ResourceType:       acltypes.ResourceType_KV_STAKING_VALIDATOR,
				AccessType:         acltypes.AccessType_WRITE,
				IdentifierTemplate: "stakingPrefix",
			},
			*types.CommitAccessOp(),
		},
		DynamicEnabled: true,
	}
	err := app.AccessControlKeeper.SetResourceDependencyMapping(ctx, delegateStaticMapping)
	req.NoError(err)

	// setup staking delegate msg
	stakingUndelegate := stakingtypes.MsgUndelegate{
		DelegatorAddress: suite.addrs[0].String(),
		ValidatorAddress: suite.addrs[1].String(),
		Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(10)},
	}
	undelegateKey := types.GenerateMessageKey(&stakingUndelegate)
	// setup staking dependency
	undelegateStaticMapping := acltypes.MessageDependencyMapping{
		MessageKey: string(undelegateKey),
		AccessOps: []acltypes.AccessOperation{
			{
				ResourceType:       acltypes.ResourceType_KV_STAKING_DELEGATION,
				AccessType:         acltypes.AccessType_WRITE,
				IdentifierTemplate: "stakingUndelegatePrefix",
			},
			{
				ResourceType:       acltypes.ResourceType_KV_STAKING_VALIDATOR,
				AccessType:         acltypes.AccessType_WRITE,
				IdentifierTemplate: "stakingUndelegatePrefix",
			},
			*types.CommitAccessOp(),
		},
		DynamicEnabled: true,
	}
	err = app.AccessControlKeeper.SetResourceDependencyMapping(ctx, undelegateStaticMapping)
	req.NoError(err)

	// get the message dependencies from keeper (because nothing configured, should return synchronous)
	app.AccessControlKeeper.SetDependencyMappingDynamicFlag(ctx, bankMsgKey, false)
	accessOps := app.AccessControlKeeper.GetMessageDependencies(ctx, &bankSendMsg)
	req.Equal(types.SynchronousMessageDependencyMapping("").AccessOps, accessOps)

	// setup bank send static dependency
	bankStaticMapping := acltypes.MessageDependencyMapping{
		MessageKey: string(bankMsgKey),
		AccessOps: []acltypes.AccessOperation{
			{
				ResourceType:       acltypes.ResourceType_KV_BANK_BALANCES,
				AccessType:         acltypes.AccessType_WRITE,
				IdentifierTemplate: "*",
			},
			*types.CommitAccessOp(),
		},
		DynamicEnabled: false,
	}
	err = app.AccessControlKeeper.SetResourceDependencyMapping(ctx, bankStaticMapping)
	req.NoError(err)

	// now, because we have static mappings + dynamic enabled == false, we get the static access ops
	accessOps = app.AccessControlKeeper.GetMessageDependencies(ctx, &bankSendMsg)
	req.Equal(bankStaticMapping.AccessOps, accessOps)

	// lets enable dynamic enabled
	app.AccessControlKeeper.SetDependencyMappingDynamicFlag(ctx, bankMsgKey, true)
	// verify dynamic enabled
	dependencyMapping := app.AccessControlKeeper.GetResourceDependencyMapping(ctx, bankMsgKey)
	req.Equal(true, dependencyMapping.DynamicEnabled)

	// now, because we have static mappings + dynamic enabled == true, we get dynamic ops
	accessOps = app.AccessControlKeeper.GetMessageDependencies(ctx, &bankSendMsg)
	dynamicOps, err := acltestutil.BankSendDepGenerator(app.AccessControlKeeper, ctx, &bankSendMsg)
	req.NoError(err)
	req.Equal(dynamicOps, accessOps)

	// lets true doing the same for staking delegate, which SHOULD fail validation and set dynamic to false and return static mapping
	accessOps = app.AccessControlKeeper.GetMessageDependencies(ctx, &stakingDelegate)
	req.Equal(delegateStaticMapping.AccessOps, accessOps)
	// verify dynamic got disabled
	dependencyMapping = app.AccessControlKeeper.GetResourceDependencyMapping(ctx, delegateKey)
	req.Equal(false, dependencyMapping.DynamicEnabled)

	// lets also try with undelegate, but this time there is no dynamic generator, so we disable it as well
	accessOps = app.AccessControlKeeper.GetMessageDependencies(ctx, &stakingUndelegate)
	req.Equal(undelegateStaticMapping.AccessOps, accessOps)
	// verify dynamic got disabled
	dependencyMapping = app.AccessControlKeeper.GetResourceDependencyMapping(ctx, undelegateKey)
	req.Equal(false, dependencyMapping.DynamicEnabled)
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}
