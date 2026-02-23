package capability_test

import (
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/capability"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/types"
)

type CapabilityTestSuite struct {
	suite.Suite

	cdc    codec.Codec
	ctx    sdk.Context
	app    *seiapp.App
	keeper *keeper.Keeper
	module module.AppModule
}

func (suite *CapabilityTestSuite) SetupTest() {
	checkTx := false
	app := seiapp.Setup(suite.T(), checkTx, false, false)
	cdc := app.AppCodec()

	// create new keeper so we can define custom scoping before init and seal
	keeper := keeper.NewKeeper(cdc, app.GetKey(types.StoreKey), app.GetMemKey(types.MemStoreKey))

	suite.app = app
	suite.ctx = app.BaseApp.NewContext(checkTx, tmproto.Header{Height: 1})
	suite.keeper = keeper
	suite.cdc = cdc
	suite.module = capability.NewAppModule(cdc, *keeper)
}

// The following test case mocks a specific bug discovered in https://github.com/cosmos/cosmos-sdk/issues/9800
// and ensures that the current code successfully fixes the issue.
func (suite *CapabilityTestSuite) TestInitializeMemStore() {
	// mock statesync by creating new keeper that shares persistent state but loses in-memory map
	newKeeper := keeper.NewKeeper(suite.cdc, suite.app.GetKey(types.StoreKey), suite.app.GetMemKey("mem_capability"))
	newSk1 := newKeeper.ScopeToModule(banktypes.ModuleName)

	cap1, err := newSk1.NewCapability(suite.ctx, "transfer")
	suite.Require().NoError(err)
	suite.Require().NotNil(cap1)

	// Mock App startup
	ctx := suite.app.BaseApp.NewUncachedContext(false, tmproto.Header{})
	newKeeper.Seal()
	suite.Require().False(newKeeper.IsInitialized(ctx), "memstore initialized flag set before BeginBlock")

	// Mock app beginblock and ensure that no gas has been consumed and memstore is initialized
	ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{})
	capability.BeginBlocker(ctx, *newKeeper)
	suite.Require().True(newKeeper.IsInitialized(ctx), "memstore initialized flag not set")

	// Mock the first transaction getting capability and subsequently failing
	// by using a cached context and discarding all cached writes.
	cacheCtx, _ := ctx.CacheContext()
	_, ok := newSk1.GetCapability(cacheCtx, "transfer")
	suite.Require().True(ok)

	// Ensure that the second transaction can still receive capability even if first tx fails.
	ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{})

	cap1, ok = newSk1.GetCapability(ctx, "transfer")
	suite.Require().True(ok)

	// Ensure the capabilities don't get reinitialized on next BeginBlock
	// by testing to see if capability returns same pointer
	// also check that initialized flag is still set
	capability.BeginBlocker(ctx, *newKeeper)
	recap, ok := newSk1.GetCapability(ctx, "transfer")
	suite.Require().True(ok)
	suite.Require().Equal(cap1, recap, "capabilities got reinitialized after second BeginBlock")
	suite.Require().True(newKeeper.IsInitialized(ctx), "memstore initialized flag not set")
}

func TestCapabilityTestSuite(t *testing.T) {
	suite.Run(t, new(CapabilityTestSuite))
}
