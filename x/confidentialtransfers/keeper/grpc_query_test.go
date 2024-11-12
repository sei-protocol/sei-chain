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
			name:    "invalid address",
			req:     &types.GetAccountRequest{Address: "INVALID"},
			expFail: true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = invalid address: decoding bech32 failed: " +
				"invalid bech32 string length 7",
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
			name: "existing account for address and denom",
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
		req             *types.GetAllAccountsRequest
		expResponse     *types.GetAllAccountsResponse
		expFail         bool
		expErrorMessage string
	}{
		{
			name:            "empty request",
			req:             &types.GetAllAccountsRequest{},
			expFail:         true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = address cannot be empty",
		},
		{
			name:    "invalid address",
			req:     &types.GetAllAccountsRequest{Address: "INVALID"},
			expFail: true,
			expErrorMessage: "rpc error: code = InvalidArgument desc = invalid address: decoding bech32 failed: " +
				"invalid bech32 string length 7",
		},
		{
			name:        "account for address does not exist",
			req:         &types.GetAllAccountsRequest{Address: nonExistingAddr.String()},
			expResponse: &types.GetAllAccountsResponse{Pagination: &query.PageResponse{}},
		},
		{
			name: "accounts for address exist",
			req:  &types.GetAllAccountsRequest{Address: addr.String()},
			expResponse: &types.GetAllAccountsResponse{
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
			req:  &types.GetAllAccountsRequest{Address: addr.String(), Pagination: &query.PageRequest{Limit: 1}},
			expResponse: &types.GetAllAccountsResponse{
				Accounts:   []types.CtAccountWithDenom{{Denom: testDenom1, Account: ctAccount1}},
				Pagination: &query.PageResponse{NextKey: []byte(testDenom2)},
			},
		},
		{
			name: "paginated request - second page",
			req: &types.GetAllAccountsRequest{Address: addr.String(), Pagination: &query.PageRequest{
				Limit: 1,
				Key:   []byte(testDenom2)},
			},
			expResponse: &types.GetAllAccountsResponse{
				Accounts:   []types.CtAccountWithDenom{{Denom: testDenom2, Account: ctAccount2}},
				Pagination: &query.PageResponse{},
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset
			app, ctx, queryClient := suite.App, suite.Ctx, suite.queryClient

			app.ConfidentialTransfersKeeper.SetAccount(ctx, addr, testDenom1, *account1)
			app.ConfidentialTransfersKeeper.SetAccount(ctx, addr, testDenom2, *account2)

			result, err := queryClient.GetAllAccounts(ctx.Context(), tc.req)

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
