package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/ibc-go/v3/modules/core/05-port/keeper"
	"github.com/cosmos/ibc-go/v3/testing/simapp"
)

var (
	validPort   = "validportid"
	invalidPort = "(invalidPortID)"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx    sdk.Context
	keeper *keeper.Keeper
}

func (suite *KeeperTestSuite) SetupTest() {
	isCheckTx := false
	app := simapp.Setup(isCheckTx)

	suite.ctx = app.BaseApp.NewContext(isCheckTx, tmproto.Header{})
	suite.keeper = &app.IBCKeeper.PortKeeper
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) TestBind() {
	// Test that invalid portID causes panic
	require.Panics(suite.T(), func() { suite.keeper.BindPort(suite.ctx, invalidPort) }, "Did not panic on invalid portID")

	// Test that valid BindPort returns capability key
	capKey := suite.keeper.BindPort(suite.ctx, validPort)
	require.NotNil(suite.T(), capKey, "capabilityKey is nil on valid BindPort")

	isBound := suite.keeper.IsBound(suite.ctx, validPort)
	require.True(suite.T(), isBound, "port is bound successfully")

	isNotBound := suite.keeper.IsBound(suite.ctx, "not-a-port")
	require.False(suite.T(), isNotBound, "port is not bound")

	// Test that rebinding the same portid causes panic
	require.Panics(suite.T(), func() { suite.keeper.BindPort(suite.ctx, validPort) }, "did not panic on re-binding the same port")
}

func (suite *KeeperTestSuite) TestAuthenticate() {
	capKey := suite.keeper.BindPort(suite.ctx, validPort)

	// Require that passing in invalid portID causes panic
	require.Panics(suite.T(), func() { suite.keeper.Authenticate(suite.ctx, capKey, invalidPort) }, "did not panic on invalid portID")

	// Valid authentication should return true
	auth := suite.keeper.Authenticate(suite.ctx, capKey, validPort)
	require.True(suite.T(), auth, "valid authentication failed")

	// Test that authenticating against incorrect portid fails
	auth = suite.keeper.Authenticate(suite.ctx, capKey, "wrongportid")
	require.False(suite.T(), auth, "invalid authentication failed")

	// Test that authenticating port against different valid
	// capability key fails
	capKey2 := suite.keeper.BindPort(suite.ctx, "otherportid")
	auth = suite.keeper.Authenticate(suite.ctx, capKey2, validPort)
	require.False(suite.T(), auth, "invalid authentication for different capKey failed")
}
