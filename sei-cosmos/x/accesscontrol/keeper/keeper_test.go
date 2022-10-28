package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
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
			types.CommitAccessOp(),
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
		Enabled: true,
		AccessOps: []acltypes.AccessOperation{
			{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
			types.CommitAccessOp(),
		},
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmContractAddress, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, mapping)
	// test getting a dependency mapping for something function that isn't present
	_, err = app.AccessControlKeeper.GetWasmDependencyMapping(ctx, otherContractAddress)
	require.Error(t, aclkeeper.ErrWasmDependencyMappingNotFound, err)
}

func TestResetWasmDependencyMapping(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	wasmContractAddresses := simapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(30000000))
	wasmContractAddress := wasmContractAddresses[0]
	wasmMapping := acltypes.WasmDependencyMapping{
		Enabled: true,
		AccessOps: []acltypes.AccessOperation{
			{ResourceType: acltypes.ResourceType_KV, AccessType: acltypes.AccessType_WRITE, IdentifierTemplate: "someResource"},
			types.CommitAccessOp(),
		},
	}
	// set the dependency mapping
	err := app.AccessControlKeeper.SetWasmDependencyMapping(ctx, wasmContractAddress, wasmMapping)
	require.NoError(t, err)
	// test getting the dependency mapping
	mapping, err := app.AccessControlKeeper.GetWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, wasmMapping, mapping)
	// test resetting
	err = app.AccessControlKeeper.ResetWasmDependencyMapping(ctx, wasmContractAddress, "some reason")
	require.NoError(t, err)
	mapping, err = app.AccessControlKeeper.GetWasmDependencyMapping(ctx, wasmContractAddress)
	require.NoError(t, err)
	require.Equal(t, types.SynchronousAccessOps(), mapping.AccessOps)
	require.Equal(t, "some reason", mapping.ResetReason)
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
			types.CommitAccessOp(),
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
			types.CommitAccessOp(),
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
				ResourceType:       acltypes.ResourceType_KV_BANK,
				AccessType:         acltypes.AccessType_WRITE,
				IdentifierTemplate: "bankPrefix",
			},
			types.CommitAccessOp(),
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
