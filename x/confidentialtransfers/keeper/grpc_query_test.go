package keeper_test

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	"github.com/cosmos/cosmos-sdk/types/query"
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
		req             *types.GetCtAccountRequest
		expFail         bool
		expErrorMessage string
	}{
		{
			name:            "empty request",
			req:             &types.GetCtAccountRequest{},
			expFail:         true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = address cannot be empty",
		},
		{
			name:            "empty denom",
			req:             &types.GetCtAccountRequest{Address: addr.String()},
			expFail:         true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = invalid denom",
		},
		{
			name:    "invalid address",
			req:     &types.GetCtAccountRequest{Address: "INVALID"},
			expFail: true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = invalid address: decoding bech32 failed: " +
				"invalid bech32 string length 7",
		},
		{
			name:    "account for address does not exist",
			req:     &types.GetCtAccountRequest{Address: nonExistingAddr.String(), Denom: testDenom},
			expFail: true,
			expErrorMessage: fmt.Sprintf("rpc error: code = NotFound desc = account not found for account %s "+
				"and denom %s", nonExistingAddr, testDenom),
		},
		{
			name:    "account for the denom does not exist",
			req:     &types.GetCtAccountRequest{Address: addr.String(), Denom: nonExistingDenom},
			expFail: true,
			expErrorMessage: fmt.Sprintf("rpc error: code = NotFound desc = account not found for account %s "+
				"and denom %s", addr.String(), nonExistingDenom),
		},
		{
			name: "existing account for address and denom",
			req:  &types.GetCtAccountRequest{Address: addr.String(), Denom: testDenom},
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset
			app, ctx, queryClient := suite.App, suite.Ctx, suite.queryClient

			_ = app.ConfidentialTransfersKeeper.SetAccount(ctx, addr.String(), testDenom, *account)

			result, err := queryClient.GetCtAccount(ctx.Context(), tc.req)

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

func (suite *KeeperTestSuite) TestAllAccountsQuery() {
	_, _, addr := testdata.KeyTestPubAddr()
	_, _, nonExistingAddr := testdata.KeyTestPubAddr()
	testDenom1 := fmt.Sprintf("factory/%s/FIRST", addr.String())
	testDenom2 := fmt.Sprintf("factory/%s/SECOND", addr.String())

	pk1, _ := encryption.GenerateKey()
	pk2, _ := encryption.GenerateKey()
	ctAccount1 := generateCtAccount(pk1, testDenom1, 1000)
	ctAccount2 := generateCtAccount(pk2, testDenom2, 2000)
	account1, _ := ctAccount1.FromProto()
	account2, _ := ctAccount2.FromProto()

	testCases := []struct {
		name            string
		req             *types.GetAllCtAccountsRequest
		expResponse     *types.GetAllCtAccountsResponse
		expFail         bool
		expErrorMessage string
	}{
		{
			name:            "empty request",
			req:             &types.GetAllCtAccountsRequest{},
			expFail:         true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = address cannot be empty",
		},
		{
			name:    "invalid address",
			req:     &types.GetAllCtAccountsRequest{Address: "INVALID"},
			expFail: true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = invalid address: decoding bech32 failed: " +
				"invalid bech32 string length 7",
		},
		{
			name:        "account for address does not exist",
			req:         &types.GetAllCtAccountsRequest{Address: nonExistingAddr.String()},
			expResponse: &types.GetAllCtAccountsResponse{Pagination: &query.PageResponse{}},
		},
		{
			name: "accounts for address exist",
			req:  &types.GetAllCtAccountsRequest{Address: addr.String()},
			expResponse: &types.GetAllCtAccountsResponse{
				Accounts: []types.CtAccountWithDenom{
					{
						Denom:   testDenom1,
						Account: ctAccount1,
					},
					{
						Denom:   testDenom2,
						Account: ctAccount2,
					},
				},
				Pagination: &query.PageResponse{Total: 2},
			},
		},
		{
			name: "paginated request",
			req:  &types.GetAllCtAccountsRequest{Address: addr.String(), Pagination: &query.PageRequest{Limit: 1}},
			expResponse: &types.GetAllCtAccountsResponse{
				Accounts:   []types.CtAccountWithDenom{{Denom: testDenom1, Account: ctAccount1}},
				Pagination: &query.PageResponse{NextKey: []byte(testDenom2)},
			},
		},
		{
			name: "paginated request - second page",
			req: &types.GetAllCtAccountsRequest{Address: addr.String(), Pagination: &query.PageRequest{
				Limit: 1,
				Key:   []byte(testDenom2)},
			},
			expResponse: &types.GetAllCtAccountsResponse{
				Accounts:   []types.CtAccountWithDenom{{Denom: testDenom2, Account: ctAccount2}},
				Pagination: &query.PageResponse{},
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset
			app, ctx, queryClient := suite.App, suite.Ctx, suite.queryClient

			_ = app.ConfidentialTransfersKeeper.SetAccount(ctx, addr.String(), testDenom1, *account1)
			_ = app.ConfidentialTransfersKeeper.SetAccount(ctx, addr.String(), testDenom2, *account2)

			result, err := queryClient.GetAllCtAccounts(ctx.Context(), tc.req)

			if tc.expFail {
				suite.Require().Error(err)
				suite.Require().EqualError(err, tc.expErrorMessage)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(result)
				suite.Require().Equal(tc.expResponse, result)
			}
		})
	}
}
