package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/crypto"

	icatypes "github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

var (
	// TODO: Cosmos-SDK ADR-28: Update crypto.AddressHash() when sdk uses address.Module()
	// https://github.com/cosmos/cosmos-sdk/issues/10225
	//
	// TestAccAddress defines a resuable bech32 address for testing purposes
	TestAccAddress = icatypes.GenerateAddress(sdk.AccAddress(crypto.AddressHash([]byte(icatypes.ModuleName))), ibctesting.FirstConnectionID, TestPortID)

	// TestOwnerAddress defines a reusable bech32 address for testing purposes
	TestOwnerAddress = "cosmos17dtl0mjt3t77kpuhg2edqzjpszulwhgzuj9ljs"

	// TestPortID defines a resuable port identifier for testing purposes
	TestPortID, _ = icatypes.NewControllerPortID(TestOwnerAddress)

	// TestVersion defines a resuable interchainaccounts version string for testing purposes
	TestVersion = string(icatypes.ModuleCdc.MustMarshalJSON(&icatypes.Metadata{
		Version:                icatypes.Version,
		ControllerConnectionId: ibctesting.FirstConnectionID,
		HostConnectionId:       ibctesting.FirstConnectionID,
		Encoding:               icatypes.EncodingProtobuf,
		TxType:                 icatypes.TxTypeSDKMultiMsg,
	}))
)

type KeeperTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
	chainC *ibctesting.TestChain
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 3)
	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
	suite.chainC = suite.coordinator.GetChain(ibctesting.GetChainID(3))
}

func NewICAPath(chainA, chainB *ibctesting.TestChain) *ibctesting.Path {
	path := ibctesting.NewPath(chainA, chainB)
	path.EndpointA.ChannelConfig.PortID = icatypes.PortID
	path.EndpointB.ChannelConfig.PortID = icatypes.PortID
	path.EndpointA.ChannelConfig.Order = channeltypes.ORDERED
	path.EndpointB.ChannelConfig.Order = channeltypes.ORDERED
	path.EndpointA.ChannelConfig.Version = TestVersion
	path.EndpointB.ChannelConfig.Version = TestVersion

	return path
}

// SetupICAPath invokes the InterchainAccounts entrypoint and subsequent channel handshake handlers
func SetupICAPath(path *ibctesting.Path, owner string) error {
	if err := RegisterInterchainAccount(path.EndpointA, owner); err != nil {
		return err
	}

	if err := path.EndpointB.ChanOpenTry(); err != nil {
		return err
	}

	if err := path.EndpointA.ChanOpenAck(); err != nil {
		return err
	}

	if err := path.EndpointB.ChanOpenConfirm(); err != nil {
		return err
	}

	return nil
}

// RegisterInterchainAccount is a helper function for starting the channel handshake
func RegisterInterchainAccount(endpoint *ibctesting.Endpoint, owner string) error {
	portID, err := icatypes.NewControllerPortID(owner)
	if err != nil {
		return err
	}

	channelSequence := endpoint.Chain.App.GetIBCKeeper().ChannelKeeper.GetNextChannelSequence(endpoint.Chain.GetContext())

	if err := endpoint.Chain.GetSimApp().ICAControllerKeeper.RegisterInterchainAccount(endpoint.Chain.GetContext(), endpoint.ConnectionID, owner); err != nil {
		return err
	}

	// commit state changes for proof verification
	endpoint.Chain.App.Commit()
	endpoint.Chain.NextBlock()

	// update port/channel ids
	endpoint.ChannelID = channeltypes.FormatChannelIdentifier(channelSequence)
	endpoint.ChannelConfig.PortID = portID

	return nil
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) TestIsBound() {
	suite.SetupTest()

	path := NewICAPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path)

	err := SetupICAPath(path, TestOwnerAddress)
	suite.Require().NoError(err)

	isBound := suite.chainA.GetSimApp().ICAControllerKeeper.IsBound(suite.chainA.GetContext(), path.EndpointA.ChannelConfig.PortID)
	suite.Require().True(isBound)
}

func (suite *KeeperTestSuite) TestGetAllPorts() {
	suite.SetupTest()

	path := NewICAPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path)

	err := SetupICAPath(path, TestOwnerAddress)
	suite.Require().NoError(err)

	expectedPorts := []string{TestPortID}

	ports := suite.chainA.GetSimApp().ICAControllerKeeper.GetAllPorts(suite.chainA.GetContext())
	suite.Require().Len(ports, len(expectedPorts))
	suite.Require().Equal(expectedPorts, ports)
}

func (suite *KeeperTestSuite) TestGetInterchainAccountAddress() {
	suite.SetupTest()

	path := NewICAPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path)

	err := SetupICAPath(path, TestOwnerAddress)
	suite.Require().NoError(err)

	counterpartyPortID := path.EndpointA.ChannelConfig.PortID
	expectedAddr := icatypes.GenerateAddress(suite.chainA.GetSimApp().AccountKeeper.GetModuleAddress(icatypes.ModuleName), ibctesting.FirstConnectionID, counterpartyPortID)

	retrievedAddr, found := suite.chainA.GetSimApp().ICAControllerKeeper.GetInterchainAccountAddress(suite.chainA.GetContext(), ibctesting.FirstConnectionID, counterpartyPortID)
	suite.Require().True(found)
	suite.Require().Equal(expectedAddr.String(), retrievedAddr)

	retrievedAddr, found = suite.chainA.GetSimApp().ICAControllerKeeper.GetInterchainAccountAddress(suite.chainA.GetContext(), "invalid conn", "invalid port")
	suite.Require().False(found)
	suite.Require().Empty(retrievedAddr)
}

func (suite *KeeperTestSuite) TestGetAllActiveChannels() {
	var (
		expectedChannelID string = "test-channel"
		expectedPortID    string = "test-port"
	)

	suite.SetupTest()

	path := NewICAPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path)

	err := SetupICAPath(path, TestOwnerAddress)
	suite.Require().NoError(err)

	suite.chainA.GetSimApp().ICAControllerKeeper.SetActiveChannelID(suite.chainA.GetContext(), ibctesting.FirstConnectionID, expectedPortID, expectedChannelID)

	expectedChannels := []icatypes.ActiveChannel{
		{
			ConnectionId: ibctesting.FirstConnectionID,
			PortId:       TestPortID,
			ChannelId:    path.EndpointA.ChannelID,
		},
		{
			ConnectionId: ibctesting.FirstConnectionID,
			PortId:       expectedPortID,
			ChannelId:    expectedChannelID,
		},
	}

	activeChannels := suite.chainA.GetSimApp().ICAControllerKeeper.GetAllActiveChannels(suite.chainA.GetContext())
	suite.Require().Len(activeChannels, len(expectedChannels))
	suite.Require().Equal(expectedChannels, activeChannels)
}

func (suite *KeeperTestSuite) TestGetAllInterchainAccounts() {
	var (
		expectedAccAddr string = "test-acc-addr"
		expectedPortID  string = "test-port"
	)

	suite.SetupTest()

	path := NewICAPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupConnections(path)

	err := SetupICAPath(path, TestOwnerAddress)
	suite.Require().NoError(err)

	suite.chainA.GetSimApp().ICAControllerKeeper.SetInterchainAccountAddress(suite.chainA.GetContext(), ibctesting.FirstConnectionID, expectedPortID, expectedAccAddr)

	expectedAccounts := []icatypes.RegisteredInterchainAccount{
		{
			ConnectionId:   ibctesting.FirstConnectionID,
			PortId:         TestPortID,
			AccountAddress: TestAccAddress.String(),
		},
		{
			ConnectionId:   ibctesting.FirstConnectionID,
			PortId:         expectedPortID,
			AccountAddress: expectedAccAddr,
		},
	}

	interchainAccounts := suite.chainA.GetSimApp().ICAControllerKeeper.GetAllInterchainAccounts(suite.chainA.GetContext())
	suite.Require().Len(interchainAccounts, len(expectedAccounts))
	suite.Require().Equal(expectedAccounts, interchainAccounts)
}

func (suite *KeeperTestSuite) TestIsActiveChannel() {
	suite.SetupTest()

	path := NewICAPath(suite.chainA, suite.chainB)
	owner := TestOwnerAddress
	suite.coordinator.SetupConnections(path)

	err := SetupICAPath(path, owner)
	suite.Require().NoError(err)
	portID := path.EndpointA.ChannelConfig.PortID

	isActive := suite.chainA.GetSimApp().ICAControllerKeeper.IsActiveChannel(suite.chainA.GetContext(), ibctesting.FirstConnectionID, portID)
	suite.Require().Equal(isActive, true)
}

func (suite *KeeperTestSuite) TestSetInterchainAccountAddress() {
	var (
		expectedAccAddr string = "test-acc-addr"
		expectedPortID  string = "test-port"
	)

	suite.chainA.GetSimApp().ICAControllerKeeper.SetInterchainAccountAddress(suite.chainA.GetContext(), ibctesting.FirstConnectionID, expectedPortID, expectedAccAddr)

	retrievedAddr, found := suite.chainA.GetSimApp().ICAControllerKeeper.GetInterchainAccountAddress(suite.chainA.GetContext(), ibctesting.FirstConnectionID, expectedPortID)
	suite.Require().True(found)
	suite.Require().Equal(expectedAccAddr, retrievedAddr)
}
