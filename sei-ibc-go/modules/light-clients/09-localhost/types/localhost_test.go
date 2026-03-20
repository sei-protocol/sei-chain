package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/exported"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/testing/simapp"
)

const (
	height = 4
)

var clientHeight = clienttypes.NewHeight(0, 10)

type LocalhostTestSuite struct {
	suite.Suite

	cdc   codec.Codec
	ctx   sdk.Context
	store sdk.KVStore
}

func (suite *LocalhostTestSuite) SetupTest() {
	isCheckTx := false
	app := simapp.Setup(isCheckTx)

	suite.cdc = app.AppCodec()
	suite.ctx = app.BaseApp.NewContext(isCheckTx, tmproto.Header{Height: 1, ChainID: "ibc-chain"})
	suite.store = app.IBCKeeper.ClientKeeper.ClientStore(suite.ctx, exported.Localhost)
}

func TestLocalhostTestSuite(t *testing.T) {
	suite.Run(t, new(LocalhostTestSuite))
}
