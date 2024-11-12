package keeper_test

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
)

func (suite *KeeperTestSuite) TestAccountQuery() {
	_, _, addr := testdata.KeyTestPubAddr()
	_, _, nonExistingAddr := testdata.KeyTestPubAddr()
	testDenom := fmt.Sprintf("factory/%s/TEST", addr.String())
	nonExistingDenom := fmt.Sprintf("factory/%s/NONEXISTING", addr.String())

	pk1, _ := encryption.GenerateKey()
	ctAccount := generateCtAccount(pk1, testDenom, 1000)
	account, _ := ctAccount.FromProto()

	testCases := []struct {
		name            string
		req             *types.GetAccountRequest
		expFail         bool
		expErrorMessage string
	}{
		{
			name:            "empty request",
			req:             &types.GetAccountRequest{},
			expFail:         true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = address cannot be empty",
		},
		{
			name:            "empty denom",
			req:             &types.GetAccountRequest{Address: addr.String()},
			expFail:         true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = invalid denom",
		},
		{
			name:    "account for address does not exist",
			req:     &types.GetAccountRequest{Address: nonExistingAddr.String(), Denom: testDenom},
			expFail: true,
			expErrorMessage: fmt.Sprintf("rpc error: code = NotFound desc = account not found for account %s "+
				"and denom %s", nonExistingAddr, testDenom),
		},
		{
			name:    "account for the denom does not exist",
			req:     &types.GetAccountRequest{Address: addr.String(), Denom: nonExistingDenom},
			expFail: true,
			expErrorMessage: fmt.Sprintf("rpc error: code = NotFound desc = account not found for account %s "+
				"and denom %s", addr.String(), nonExistingDenom),
		},
		{
			name: "existing denom can be found",
			req:  &types.GetAccountRequest{Address: addr.String(), Denom: testDenom},
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset
			app, ctx, queryClient := suite.App, suite.Ctx, suite.queryClient

			app.ConfidentialTransfersKeeper.SetAccount(ctx, addr, testDenom, *account)

			result, err := queryClient.GetAccount(ctx.Context(), tc.req)

			if tc.expFail {
				suite.Require().Error(err)
				suite.Require().EqualError(err, tc.expErrorMessage)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(result)
				suite.Require().Equal(&ctAccount, result.Account)
			}
		})
	}
}
