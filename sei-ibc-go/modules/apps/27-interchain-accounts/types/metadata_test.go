package types_test

import (
	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

func (suite *TypesTestSuite) TestValidateControllerMetadata() {

	var metadata types.Metadata

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success",
			func() {},
			true,
		},
		{
			"success with empty account address",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                "",
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			true,
		},
		{
			"unsupported encoding format",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               "invalid-encoding-format",
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"unsupported transaction type",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 "invalid-tx-type",
				}
			},
			false,
		},
		{
			"invalid controller connection",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: "connection-10",
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"invalid host connection",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       "connection-10",
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"invalid address",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                " ",
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"invalid version",
			func() {
				metadata = types.Metadata{
					Version:                "invalid version",
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			metadata = types.NewMetadata(types.Version, ibctesting.FirstConnectionID, ibctesting.FirstConnectionID, TestOwnerAddress, types.EncodingProtobuf, types.TxTypeSDKMultiMsg)

			tc.malleate() // malleate mutates test data

			err := types.ValidateControllerMetadata(
				suite.chainA.GetContext(),
				suite.chainA.App.GetIBCKeeper().ChannelKeeper,
				[]string{ibctesting.FirstConnectionID},
				metadata,
			)

			if tc.expPass {
				suite.Require().NoError(err, tc.name)
			} else {
				suite.Require().Error(err, tc.name)
			}
		})
	}
}

func (suite *TypesTestSuite) TestValidateHostMetadata() {

	var metadata types.Metadata

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"success",
			func() {},
			true,
		},
		{
			"success with empty account address",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                "",
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			true,
		},
		{
			"unsupported encoding format",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               "invalid-encoding-format",
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"unsupported transaction type",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 "invalid-tx-type",
				}
			},
			false,
		},
		{
			"invalid controller connection",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: "connection-10",
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"invalid host connection",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       "connection-10",
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"invalid address",
			func() {
				metadata = types.Metadata{
					Version:                types.Version,
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                " ",
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
		{
			"invalid version",
			func() {
				metadata = types.Metadata{
					Version:                "invalid version",
					ControllerConnectionId: ibctesting.FirstConnectionID,
					HostConnectionId:       ibctesting.FirstConnectionID,
					Address:                TestOwnerAddress,
					Encoding:               types.EncodingProtobuf,
					TxType:                 types.TxTypeSDKMultiMsg,
				}
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			path := ibctesting.NewPath(suite.chainA, suite.chainB)
			suite.coordinator.SetupConnections(path)

			metadata = types.NewMetadata(types.Version, ibctesting.FirstConnectionID, ibctesting.FirstConnectionID, TestOwnerAddress, types.EncodingProtobuf, types.TxTypeSDKMultiMsg)

			tc.malleate() // malleate mutates test data

			err := types.ValidateHostMetadata(
				suite.chainA.GetContext(),
				suite.chainA.App.GetIBCKeeper().ChannelKeeper,
				[]string{ibctesting.FirstConnectionID},
				metadata,
			)

			if tc.expPass {
				suite.Require().NoError(err, tc.name)
			} else {
				suite.Require().Error(err, tc.name)
			}
		})
	}
}
