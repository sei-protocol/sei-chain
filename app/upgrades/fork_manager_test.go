package upgrades_test

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

type ForkTestSuite struct {
	apptesting.KeeperTestHelper
}

type TestHandler struct {
	TargetHeight int64
	TargetChain  string
	Name         string
	ShouldError  bool
	RunVal       *bool
}

func NewTestHandler(chainID string, height int64, name string, shouldError bool, runFlag *bool) TestHandler {
	return TestHandler{
		TargetHeight: height,
		TargetChain:  chainID,
		ShouldError:  shouldError,
		RunVal:       runFlag,
		Name:         name,
	}
}

func (th TestHandler) GetTargetChainID() string {
	return th.TargetChain
}

func (th TestHandler) GetTargetHeight() int64 {
	return th.TargetHeight
}

func (th TestHandler) GetName() string {
	return th.Name
}

func (th TestHandler) ExecuteHandler(ctx sdk.Context) error {
	*th.RunVal = true
	if th.ShouldError {
		return fmt.Errorf("error executing handler")
	}
	return nil
}

var _ upgrades.HardForkHandler = TestHandler{}

func TestForkSuite(t *testing.T) {
	suite.Run(t, new(ForkTestSuite))
}

func (suite *ForkTestSuite) TestHardForkManager() {
	suite.Setup()
	suite.CreateTestContext()
	runFlag1 := false
	runFlag2 := false
	runFlag3 := false
	runFlag4 := false
	runFlag5 := false
	testHandler1 := NewTestHandler(suite.Ctx.ChainID(), 3, "handler1", false, &runFlag1)
	testHandler2 := NewTestHandler("otherChainID", 3, "handler2", false, &runFlag2)
	testHandler3 := NewTestHandler(suite.Ctx.ChainID(), 4, "handler3", true, &runFlag3)
	dupeHandler := NewTestHandler(suite.Ctx.ChainID(), 3, "handler1", false, &runFlag4)
	testHandler5 := NewTestHandler(suite.Ctx.ChainID(), 3, "handler5", false, &runFlag5)
	suite.App.HardForkManager.RegisterHandler(testHandler1)
	suite.App.HardForkManager.RegisterHandler(testHandler2)
	suite.App.HardForkManager.RegisterHandler(testHandler3)
	suite.Require().Panics(func() {
		suite.App.HardForkManager.RegisterHandler(dupeHandler)
	})
	suite.App.HardForkManager.RegisterHandler(testHandler5)

	// run with height of 2 - nothing should happen since not a target height
	// increments height and runs begin block
	suite.Ctx = suite.Ctx.WithBlockHeight(2)
	newHeader := tmtypes.Header{Height: suite.Ctx.BlockHeight(), ChainID: suite.Ctx.ChainID(), Time: time.Now().UTC()}
	suite.App.BeginBlocker(suite.Ctx, abci.RequestBeginBlock{Header: newHeader})
	suite.Require().False(runFlag1)
	suite.Require().False(runFlag2)
	suite.Require().False(runFlag3)
	suite.Require().False(runFlag4)
	suite.Require().False(runFlag5)

	// run with height of 3 - runflag 1 should now be true
	suite.Ctx = suite.Ctx.WithBlockHeight(3)
	newHeader = tmtypes.Header{Height: suite.Ctx.BlockHeight(), ChainID: suite.Ctx.ChainID(), Time: time.Now().UTC()}
	suite.App.BeginBlocker(suite.Ctx, abci.RequestBeginBlock{Header: newHeader})
	suite.Require().True(runFlag1)
	suite.Require().False(runFlag2)
	suite.Require().False(runFlag3)
	suite.Require().False(runFlag4)
	suite.Require().True(runFlag5)

	// run with height of 4 - runflag 3 should now be true and we expect a panic
	suite.Require().Panics(func() {
		suite.Ctx = suite.Ctx.WithBlockHeight(4)
		newHeader = tmtypes.Header{Height: suite.Ctx.BlockHeight(), ChainID: suite.Ctx.ChainID(), Time: time.Now().UTC()}
		suite.App.BeginBlocker(suite.Ctx, abci.RequestBeginBlock{Header: newHeader})
	})
	suite.Require().True(runFlag3)
	suite.Require().False(runFlag4)
}
