package types_test

import (
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/suite"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	tmprotostate "github.com/tendermint/tendermint/proto/tendermint/state"
	tmstate "github.com/tendermint/tendermint/state"

	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/host/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

const (
	gasUsed   = uint64(100)
	gasWanted = uint64(100)
)

type TypesTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
}

func (suite *TypesTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 2)

	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
}

func TestTypesTestSuite(t *testing.T) {
	suite.Run(t, new(TypesTestSuite))
}

// The safety of including ABCI error codes in the acknowledgement rests
// on the inclusion of these ABCI error codes in the abcitypes.ResposneDeliverTx
// hash. If the ABCI codes get removed from consensus they must no longer be used
// in the packet acknowledgement.
//
// This test acts as an indicator that the ABCI error codes may no longer be deterministic.
func (suite *TypesTestSuite) TestABCICodeDeterminism() {
	// same ABCI error code used
	err := sdkerrors.Wrap(sdkerrors.ErrOutOfGas, "error string 1")
	errSameABCICode := sdkerrors.Wrap(sdkerrors.ErrOutOfGas, "error string 2")

	// different ABCI error code used
	errDifferentABCICode := sdkerrors.ErrNotFound

	deliverTx := sdkerrors.ResponseDeliverTx(err, gasUsed, gasWanted, false)
	responses := tmprotostate.ABCIResponses{
		DeliverTxs: []*abcitypes.ResponseDeliverTx{
			&deliverTx,
		},
	}

	deliverTxSameABCICode := sdkerrors.ResponseDeliverTx(errSameABCICode, gasUsed, gasWanted, false)
	responsesSameABCICode := tmprotostate.ABCIResponses{
		DeliverTxs: []*abcitypes.ResponseDeliverTx{
			&deliverTxSameABCICode,
		},
	}

	deliverTxDifferentABCICode := sdkerrors.ResponseDeliverTx(errDifferentABCICode, gasUsed, gasWanted, false)
	responsesDifferentABCICode := tmprotostate.ABCIResponses{
		DeliverTxs: []*abcitypes.ResponseDeliverTx{
			&deliverTxDifferentABCICode,
		},
	}

	hash := tmstate.ABCIResponsesResultsHash(&responses)
	hashSameABCICode := tmstate.ABCIResponsesResultsHash(&responsesSameABCICode)
	hashDifferentABCICode := tmstate.ABCIResponsesResultsHash(&responsesDifferentABCICode)

	suite.Require().Equal(hash, hashSameABCICode)
	suite.Require().NotEqual(hash, hashDifferentABCICode)
}

// TestAcknowledgementError will verify that only a constant string and
// ABCI error code are used in constructing the acknowledgement error string
func (suite *TypesTestSuite) TestAcknowledgementError() {
	// same ABCI error code used
	err := sdkerrors.Wrap(sdkerrors.ErrOutOfGas, "error string 1")
	errSameABCICode := sdkerrors.Wrap(sdkerrors.ErrOutOfGas, "error string 2")

	// different ABCI error code used
	errDifferentABCICode := sdkerrors.ErrNotFound

	ack := types.NewErrorAcknowledgement(err)
	ackSameABCICode := types.NewErrorAcknowledgement(errSameABCICode)
	ackDifferentABCICode := types.NewErrorAcknowledgement(errDifferentABCICode)

	suite.Require().Equal(ack, ackSameABCICode)
	suite.Require().NotEqual(ack, ackDifferentABCICode)

}
