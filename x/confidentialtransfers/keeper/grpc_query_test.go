package keeper_test

import (
	gocontext "context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
)

func (suite *KeeperTestSuite) TestAccountQuery() {
	app, ctx, queryClient := suite.App, suite.Ctx, suite.queryClient
	_, _, addr := testdata.KeyTestPubAddr()
	pk1, _ := encryption.GenerateKey()
	testDenom := fmt.Sprintf("factory/%s/TEST", addr.String())
	ctAccount := generateCtAccount(pk1, testDenom, 1000)
	account, _ := ctAccount.FromProto()

	app.ConfidentialTransfersKeeper.SetAccount(ctx, addr, testDenom, *account)

	result, err := queryClient.GetAccount(gocontext.Background(),
		&types.GetAccountRequest{Address: addr.String(), Denom: testDenom})
	suite.Require().NoError(err)

	suite.Require().Equal(&ctAccount, result.Account)
}
