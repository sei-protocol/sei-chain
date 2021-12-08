package types_test

import (
	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
)

var largeMemo = "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum"

func (suite *TypesTestSuite) TestValidateBasic() {
	testCases := []struct {
		name       string
		packetData types.InterchainAccountPacketData
		expPass    bool
	}{
		{
			"success",
			types.InterchainAccountPacketData{
				Type: types.EXECUTE_TX,
				Data: []byte("data"),
				Memo: "memo",
			},
			true,
		},
		{
			"success, empty memo",
			types.InterchainAccountPacketData{
				Type: types.EXECUTE_TX,
				Data: []byte("data"),
			},
			true,
		},
		{
			"type unspecified",
			types.InterchainAccountPacketData{
				Type: types.UNSPECIFIED,
				Data: []byte("data"), 
				Memo: "memo",
			},
			false,
		},
		{
			"empty data",
			types.InterchainAccountPacketData{
				Type: types.EXECUTE_TX,
				Data: nil,
				Memo: "memo",
			},
			false,
		},
		{
			"memo too large",
			types.InterchainAccountPacketData{
				Type: types.EXECUTE_TX,
				Data: []byte("data"),
				Memo: largeMemo,
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			err := tc.packetData.ValidateBasic()

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
