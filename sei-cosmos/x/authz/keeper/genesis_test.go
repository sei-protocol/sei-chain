package keeper_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz/keeper"
	bank "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

type GenesisTestSuite struct {
	suite.Suite

	ctx    sdk.Context
	keeper keeper.Keeper
}

func (suite *GenesisTestSuite) SetupTest() {
	checkTx := false
	app := app.Setup(suite.T(), checkTx, false, false)

	suite.ctx = app.BaseApp.NewContext(checkTx, tmproto.Header{Height: 1})
	suite.keeper = app.AuthzKeeper
}

var (
	granteePub  = secp256k1.GenPrivKey().PubKey()
	granterPub  = secp256k1.GenPrivKey().PubKey()
	granteeAddr = sdk.AccAddress(granteePub.Address())
	granterAddr = sdk.AccAddress(granterPub.Address())
)

func (suite *GenesisTestSuite) TestInitGenesis() {
	coins := sdk.NewCoins(sdk.NewCoin("foo", sdk.NewInt(1_000)))
	expiration := suite.ctx.BlockHeader().Time.Add(time.Hour)
	grant := &bank.SendAuthorization{SpendLimit: coins}
	authorization, err := codectypes.NewAnyWithValue(grant)
	suite.Require().NoError(err)

	suite.keeper.InitGenesis(suite.ctx, authz.NewGenesisState([]authz.GrantAuthorization{{
		Granter:       granterAddr.String(),
		Grantee:       granteeAddr.String(),
		Authorization: authorization,
		Expiration:    expiration,
	}}))

	got, gotExpiration := suite.keeper.GetCleanAuthorization(suite.ctx, granteeAddr, granterAddr, grant.MsgTypeURL())
	suite.Require().Equal(grant, got)
	suite.Require().Equal(expiration, gotExpiration)
}

func TestGenesisTestSuite(t *testing.T) {
	suite.Run(t, new(GenesisTestSuite))
}
