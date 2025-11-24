package types_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

// expected export ordering:
// processed height and processed time per height
// then all iteration keys
func (suite *TendermintTestSuite) TestExportMetadata() {
	// test intializing client and exporting metadata
	path := ibctesting.NewPath(suite.chainA, suite.chainB)
	suite.coordinator.SetupClients(path)
	clientStore := suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID)
	clientState := path.EndpointA.GetClientState()
	height := clientState.GetLatestHeight()

	initIteration := types.GetIterationKey(clientStore, height)
	suite.Require().NotEqual(0, len(initIteration))
	initProcessedTime, found := types.GetProcessedTime(clientStore, height)
	suite.Require().True(found)
	initProcessedHeight, found := types.GetProcessedHeight(clientStore, height)
	suite.Require().True(found)

	gm := clientState.ExportMetadata(suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID))
	suite.Require().NotNil(gm, "client with metadata returned nil exported metadata")
	suite.Require().Len(gm, 3, "exported metadata has unexpected length")

	suite.Require().Equal(types.ProcessedHeightKey(height), gm[0].GetKey(), "metadata has unexpected key")
	actualProcessedHeight, err := clienttypes.ParseHeight(string(gm[0].GetValue()))
	suite.Require().NoError(err)
	suite.Require().Equal(initProcessedHeight, actualProcessedHeight, "metadata has unexpected value")

	suite.Require().Equal(types.ProcessedTimeKey(height), gm[1].GetKey(), "metadata has unexpected key")
	suite.Require().Equal(initProcessedTime, sdk.BigEndianToUint64(gm[1].GetValue()), "metadata has unexpected value")

	suite.Require().Equal(types.IterationKey(height), gm[2].GetKey(), "metadata has unexpected key")
	suite.Require().Equal(initIteration, gm[2].GetValue(), "metadata has unexpected value")

	// test updating client and exporting metadata
	err = path.EndpointA.UpdateClient()
	suite.Require().NoError(err)

	clientState = path.EndpointA.GetClientState()
	updateHeight := clientState.GetLatestHeight()

	iteration := types.GetIterationKey(clientStore, updateHeight)
	suite.Require().NotEqual(0, len(initIteration))
	processedTime, found := types.GetProcessedTime(clientStore, updateHeight)
	suite.Require().True(found)
	processedHeight, found := types.GetProcessedHeight(clientStore, updateHeight)
	suite.Require().True(found)

	gm = clientState.ExportMetadata(suite.chainA.App.GetIBCKeeper().ClientKeeper.ClientStore(suite.chainA.GetContext(), path.EndpointA.ClientID))
	suite.Require().NotNil(gm, "client with metadata returned nil exported metadata")
	suite.Require().Len(gm, 6, "exported metadata has unexpected length")

	// expected ordering:
	// initProcessedHeight, initProcessedTime, processedHeight, processedTime, initIteration, iteration

	// check init processed height and time
	suite.Require().Equal(types.ProcessedHeightKey(height), gm[0].GetKey(), "metadata has unexpected key")
	actualProcessedHeight, err = clienttypes.ParseHeight(string(gm[0].GetValue()))
	suite.Require().NoError(err)
	suite.Require().Equal(initProcessedHeight, actualProcessedHeight, "metadata has unexpected value")

	suite.Require().Equal(types.ProcessedTimeKey(height), gm[1].GetKey(), "metadata has unexpected key")
	suite.Require().Equal(initProcessedTime, sdk.BigEndianToUint64(gm[1].GetValue()), "metadata has unexpected value")

	// check processed height and time after update
	suite.Require().Equal(types.ProcessedHeightKey(updateHeight), gm[2].GetKey(), "metadata has unexpected key")
	actualProcessedHeight, err = clienttypes.ParseHeight(string(gm[2].GetValue()))
	suite.Require().NoError(err)
	suite.Require().Equal(processedHeight, actualProcessedHeight, "metadata has unexpected value")

	suite.Require().Equal(types.ProcessedTimeKey(updateHeight), gm[3].GetKey(), "metadata has unexpected key")
	suite.Require().Equal(processedTime, sdk.BigEndianToUint64(gm[3].GetValue()), "metadata has unexpected value")

	// check iteration keys
	suite.Require().Equal(types.IterationKey(height), gm[4].GetKey(), "metadata has unexpected key")
	suite.Require().Equal(initIteration, gm[4].GetValue(), "metadata has unexpected value")

	suite.Require().Equal(types.IterationKey(updateHeight), gm[5].GetKey(), "metadata has unexpected key")
	suite.Require().Equal(iteration, gm[5].GetValue(), "metadata has unexpected value")
}
