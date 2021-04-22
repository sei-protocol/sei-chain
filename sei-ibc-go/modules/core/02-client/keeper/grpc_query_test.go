package keeper_test

import (
	"fmt"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/metadata"

	"github.com/cosmos/ibc-go/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/modules/core/23-commitment/types"
	"github.com/cosmos/ibc-go/modules/core/exported"
	ibctmtypes "github.com/cosmos/ibc-go/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/testing"
)

func (suite *KeeperTestSuite) TestQueryClientState() {
	var (
		req            *types.QueryClientStateRequest
		expClientState *codectypes.Any
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{"invalid clientID",
			func() {
				req = &types.QueryClientStateRequest{}
			},
			false,
		},
		{"client not found",
			func() {
				req = &types.QueryClientStateRequest{
					ClientId: testClientID,
				}
			},
			false,
		},
		{
			"success",
			func() {
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, types.ZeroHeight(), commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				suite.keeper.SetClientState(suite.ctx, testClientID, clientState)

				var err error
				expClientState, err = types.PackClientState(clientState)
				suite.Require().NoError(err)

				req = &types.QueryClientStateRequest{
					ClientId: testClientID,
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.ctx)
			res, err := suite.queryClient.ClientState(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(expClientState, res.ClientState)

				// ensure UnpackInterfaces is defined
				cachedValue := res.ClientState.GetCachedValue()
				suite.Require().NotNil(cachedValue)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryClientStates() {
	var (
		req             *types.QueryClientStatesRequest
		expClientStates = types.IdentifiedClientStates{}
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"empty pagination",
			func() {
				req = &types.QueryClientStatesRequest{}
			},
			true,
		},
		{
			"success, no results",
			func() {
				req = &types.QueryClientStatesRequest{
					Pagination: &query.PageRequest{
						Limit:      3,
						CountTotal: true,
					},
				}
			},
			true,
		},
		{
			"success",
			func() {
				path1 := ibctesting.NewPath(suite.chainA, suite.chainB)
				suite.coordinator.SetupClients(path1)

				path2 := ibctesting.NewPath(suite.chainA, suite.chainB)
				suite.coordinator.SetupClients(path2)

				clientStateA1 := path1.EndpointA.GetClientState()
				clientStateA2 := path2.EndpointA.GetClientState()

				idcs := types.NewIdentifiedClientState(path1.EndpointA.ClientID, clientStateA1)
				idcs2 := types.NewIdentifiedClientState(path2.EndpointA.ClientID, clientStateA2)

				// order is sorted by client id, localhost is last
				expClientStates = types.IdentifiedClientStates{idcs, idcs2}.Sort()
				req = &types.QueryClientStatesRequest{
					Pagination: &query.PageRequest{
						Limit:      7,
						CountTotal: true,
					},
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset
			expClientStates = nil

			tc.malleate()

			// always add localhost which is created by default in init genesis
			localhostClientState := suite.chainA.GetClientState(exported.Localhost)
			identifiedLocalhost := types.NewIdentifiedClientState(exported.Localhost, localhostClientState)
			expClientStates = append(expClientStates, identifiedLocalhost)

			ctx := sdk.WrapSDKContext(suite.chainA.GetContext())

			res, err := suite.chainA.QueryServer.ClientStates(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(expClientStates.Sort(), res.ClientStates)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryConsensusState() {
	var (
		req               *types.QueryConsensusStateRequest
		expConsensusState *codectypes.Any
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"invalid clientID",
			func() {
				req = &types.QueryConsensusStateRequest{}
			},
			false,
		},
		{
			"invalid height",
			func() {
				req = &types.QueryConsensusStateRequest{
					ClientId:       testClientID,
					RevisionNumber: 0,
					RevisionHeight: 0,
					LatestHeight:   false,
				}
			},
			false,
		},
		{
			"consensus state not found",
			func() {
				req = &types.QueryConsensusStateRequest{
					ClientId:     testClientID,
					LatestHeight: true,
				}
			},
			false,
		},
		{
			"success latest height",
			func() {
				clientState := ibctmtypes.NewClientState(testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false)
				cs := ibctmtypes.NewConsensusState(
					suite.consensusState.Timestamp, commitmenttypes.NewMerkleRoot([]byte("hash1")), nil,
				)
				suite.keeper.SetClientState(suite.ctx, testClientID, clientState)
				suite.keeper.SetClientConsensusState(suite.ctx, testClientID, testClientHeight, cs)

				var err error
				expConsensusState, err = types.PackConsensusState(cs)
				suite.Require().NoError(err)

				req = &types.QueryConsensusStateRequest{
					ClientId:     testClientID,
					LatestHeight: true,
				}
			},
			true,
		},
		{
			"success with height",
			func() {
				cs := ibctmtypes.NewConsensusState(
					suite.consensusState.Timestamp, commitmenttypes.NewMerkleRoot([]byte("hash1")), nil,
				)
				suite.keeper.SetClientConsensusState(suite.ctx, testClientID, testClientHeight, cs)

				var err error
				expConsensusState, err = types.PackConsensusState(cs)
				suite.Require().NoError(err)

				req = &types.QueryConsensusStateRequest{
					ClientId:       testClientID,
					RevisionNumber: 0,
					RevisionHeight: height,
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.ctx)
			res, err := suite.queryClient.ConsensusState(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(expConsensusState, res.ConsensusState)

				// ensure UnpackInterfaces is defined
				cachedValue := res.ConsensusState.GetCachedValue()
				suite.Require().NotNil(cachedValue)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryConsensusStates() {
	var (
		req                *types.QueryConsensusStatesRequest
		expConsensusStates = []types.ConsensusStateWithHeight{}
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"invalid client identifier",
			func() {
				req = &types.QueryConsensusStatesRequest{}
			},
			false,
		},
		{
			"empty pagination",
			func() {
				req = &types.QueryConsensusStatesRequest{
					ClientId: testClientID,
				}
			},
			true,
		},
		{
			"success, no results",
			func() {
				req = &types.QueryConsensusStatesRequest{
					ClientId: testClientID,
					Pagination: &query.PageRequest{
						Limit:      3,
						CountTotal: true,
					},
				}
			},
			true,
		},
		{
			"success",
			func() {
				cs := ibctmtypes.NewConsensusState(
					suite.consensusState.Timestamp, commitmenttypes.NewMerkleRoot([]byte("hash1")), nil,
				)
				cs2 := ibctmtypes.NewConsensusState(
					suite.consensusState.Timestamp.Add(time.Second), commitmenttypes.NewMerkleRoot([]byte("hash2")), nil,
				)

				clientState := ibctmtypes.NewClientState(
					testChainID, ibctmtypes.DefaultTrustLevel, trustingPeriod, ubdPeriod, maxClockDrift, testClientHeight, commitmenttypes.GetSDKSpecs(), ibctesting.UpgradePath, false, false,
				)

				// Use CreateClient to ensure that processedTime metadata gets stored.
				clientId, err := suite.keeper.CreateClient(suite.ctx, clientState, cs)
				suite.Require().NoError(err)
				suite.keeper.SetClientConsensusState(suite.ctx, clientId, testClientHeight.Increment(), cs2)

				// order is swapped because the res is sorted by client id
				expConsensusStates = []types.ConsensusStateWithHeight{
					types.NewConsensusStateWithHeight(testClientHeight, cs),
					types.NewConsensusStateWithHeight(testClientHeight.Increment().(types.Height), cs2),
				}
				req = &types.QueryConsensusStatesRequest{
					ClientId: clientId,
					Pagination: &query.PageRequest{
						Limit:      3,
						CountTotal: true,
					},
				}
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()
			ctx := sdk.WrapSDKContext(suite.ctx)

			res, err := suite.queryClient.ConsensusStates(ctx, req)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)
				suite.Require().Equal(len(expConsensusStates), len(res.ConsensusStates))
				for i := range expConsensusStates {
					suite.Require().NotNil(res.ConsensusStates[i])
					suite.Require().Equal(expConsensusStates[i], res.ConsensusStates[i])

					// ensure UnpackInterfaces is defined
					cachedValue := res.ConsensusStates[i].ConsensusState.GetCachedValue()
					suite.Require().NotNil(cachedValue)
				}
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryUpgradedConsensusStates() {
	var (
		req               *types.QueryUpgradedConsensusStateRequest
		expConsensusState *codectypes.Any
		height            int64
	)

	testCases := []struct {
		msg      string
		malleate func()
		expPass  bool
	}{
		{
			"no plan",
			func() {
				req = &types.QueryUpgradedConsensusStateRequest{}
			},
			false,
		},
		{
			"valid consensus state",
			func() {
				req = &types.QueryUpgradedConsensusStateRequest{}
				lastHeight := types.NewHeight(0, uint64(suite.ctx.BlockHeight()))
				height = int64(lastHeight.GetRevisionHeight())
				suite.ctx = suite.ctx.WithBlockHeight(height)

				expConsensusState = types.MustPackConsensusState(suite.consensusState)
				bz := types.MustMarshalConsensusState(suite.cdc, suite.consensusState)
				err := suite.keeper.SetUpgradedConsensusState(suite.ctx, height, bz)
				suite.Require().NoError(err)
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.msg), func() {
			suite.SetupTest() // reset

			tc.malleate()

			ctx := sdk.WrapSDKContext(suite.ctx)
			ctx = metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader, fmt.Sprintf("%d", height))

			res, err := suite.queryClient.UpgradedConsensusState(ctx, req)
			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().True(expConsensusState.Equal(res.UpgradedConsensusState))
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestQueryParams() {
	ctx := sdk.WrapSDKContext(suite.chainA.GetContext())
	expParams := types.DefaultParams()
	res, _ := suite.queryClient.ClientParams(ctx, &types.QueryClientParamsRequest{})
	suite.Require().Equal(&expParams, res.Params)
}
