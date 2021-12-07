package types_test

import (
	"fmt"

	"github.com/cosmos/ibc-go/v2/modules/apps/27-interchain-accounts/types"
)

func (suite *TypesTestSuite) TestParseAddressFromVersion() {

	testCases := []struct {
		name     string
		version  string
		expValue string
		expPass  bool
	}{
		{
			"success",
			types.NewAppVersion(types.VersionPrefix, TestOwnerAddress),
			TestOwnerAddress,
			true,
		},
		{
			"failed to parse address from version",
			"invalid-version-string",
			"",
			false,
		},
		{
			"failure with multiple delimiters",
			fmt.Sprint(types.NewAppVersion(types.VersionPrefix, TestOwnerAddress), types.Delimiter, types.NewAppVersion(types.VersionPrefix, TestOwnerAddress)),
			"",
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			addr, err := types.ParseAddressFromVersion(tc.version)

			if tc.expPass {
				suite.Require().Equal(tc.expValue, addr)
				suite.Require().NoError(err, tc.name)
			} else {
				suite.Require().Empty(addr)
				suite.Require().Error(err, tc.name)
			}
		})
	}
}

func (suite *TypesTestSuite) TestValidateVersion() {
	testCases := []struct {
		name    string
		version string
		expPass bool
	}{
		{
			"success",
			types.NewAppVersion(types.VersionPrefix, TestOwnerAddress),
			true,
		},
		{
			"unexpected version string format",
			"invalid-version-string-format",
			false,
		},
		{
			"unexpected version string format, additional delimiter",
			types.NewAppVersion(types.VersionPrefix, "cosmos17dtl0mjt3t77kpu.hg2edqzjpszulwhgzuj9ljs"),
			false,
		},
		{
			"invalid version",
			types.NewAppVersion("ics27-5", TestOwnerAddress),
			false,
		},
		{
			"invalid account address - empty",
			types.NewAppVersion(types.VersionPrefix, ""),
			false,
		},
		{
			"invalid account address - exceeded character length",
			types.NewAppVersion(types.VersionPrefix, "ofwafxhdmqcdbpzvrccxkidbunrwyyoboyctignpvthxbwxtmnzyfwhhywobaatltfwafxhdmqcdbpzvrccxkidbunrwyyoboyctignpvthxbwxtmnzyfwhhywobaatlt"),
			false,
		},
		{
			"invalid account address - non alphanumeric characters",
			types.NewAppVersion(types.VersionPrefix, "-_-"),
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := types.ValidateVersion(tc.version)

			if tc.expPass {
				suite.Require().NoError(err, tc.name)
			} else {
				suite.Require().Error(err, tc.name)
			}
		})
	}
}
