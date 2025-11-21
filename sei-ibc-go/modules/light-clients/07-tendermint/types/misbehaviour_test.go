package types_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tendermint/tendermint/crypto/tmhash"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"

	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v3/modules/core/exported"
	"github.com/cosmos/ibc-go/v3/modules/light-clients/07-tendermint/types"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
	ibctestingmock "github.com/cosmos/ibc-go/v3/testing/mock"
)

func (suite *TendermintTestSuite) TestMisbehaviour() {
	signers := []tmtypes.PrivValidator{suite.privVal}
	heightMinus1 := clienttypes.NewHeight(0, height.RevisionHeight-1)

	misbehaviour := &types.Misbehaviour{
		Header1:  suite.header,
		Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now, suite.valSet, suite.valSet, signers),
		ClientId: clientID,
	}

	suite.Require().Equal(exported.Tendermint, misbehaviour.ClientType())
	suite.Require().Equal(clientID, misbehaviour.GetClientID())
}

// TODO: remove when integrating into monorepo, this is testing tooling that is currently private to the module and not worth bumping tm version for
func makeExtCommit(ctx context.Context, blockID tmtypes.BlockID, height int64, round int32,
	voteSet *tmtypes.VoteSet, validators []tmtypes.PrivValidator, now time.Time) (*tmtypes.ExtendedCommit, error) {

	// all sign
	for i := 0; i < len(validators); i++ {
		pubKey, err := validators[i].GetPubKey(ctx)
		if err != nil {
			return nil, fmt.Errorf("can't get pubkey: %w", err)
		}
		vote := &tmtypes.Vote{
			ValidatorAddress: pubKey.Address(),
			ValidatorIndex:   int32(i),
			Height:           height,
			Round:            round,
			Type:             tmproto.PrecommitType,
			BlockID:          blockID,
			Timestamp:        now,
		}

		_, err = signAddVote(ctx, validators[i], vote, voteSet)
		if err != nil {
			return nil, err
		}
	}

	return voteSet.MakeExtendedCommit(), nil
}

// TODO: remove this when integrating into monorepo
func signAddVote(ctx context.Context, privVal tmtypes.PrivValidator, vote *tmtypes.Vote, voteSet *tmtypes.VoteSet) (signed bool, err error) {
	v := vote.ToProto()
	err = privVal.SignVote(ctx, voteSet.ChainID(), v)
	if err != nil {
		return false, err
	}
	vote.Signature = v.Signature
	vote.ExtensionSignature = v.ExtensionSignature
	return voteSet.AddVote(vote)
}

func (suite *TendermintTestSuite) TestMisbehaviourValidateBasic() {
	altPrivVal := ibctestingmock.NewPV()
	altPubKey, err := altPrivVal.GetPubKey(suite.T().Context())
	suite.Require().NoError(err)

	revisionHeight := int64(height.RevisionHeight)

	altVal := tmtypes.NewValidator(altPubKey, revisionHeight)

	// Create bothValSet with both suite validator and altVal
	bothValSet := tmtypes.NewValidatorSet(append(suite.valSet.Validators, altVal))
	// Create alternative validator set with only altVal
	altValSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{altVal})

	signers := []tmtypes.PrivValidator{suite.privVal}

	// Create signer array and ensure it is in same order as bothValSet
	_, suiteVal := suite.valSet.GetByIndex(0)
	bothSigners := ibctesting.CreateSortedSignerArray(altPrivVal, suite.privVal, altVal, suiteVal)

	altSigners := []tmtypes.PrivValidator{altPrivVal}

	heightMinus1 := clienttypes.NewHeight(0, height.RevisionHeight-1)

	testCases := []struct {
		name                 string
		misbehaviour         *types.Misbehaviour
		malleateMisbehaviour func(misbehaviour *types.Misbehaviour) error
		expPass              bool
	}{
		{
			"valid fork misbehaviour, two headers at same height have different time",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now.Add(time.Minute), suite.valSet, suite.valSet, signers),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			true,
		},
		{
			"valid time misbehaviour, both headers at different heights are at same time",
			&types.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight+5), heightMinus1, suite.now, suite.valSet, suite.valSet, signers),
				Header2:  suite.header,
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			true,
		},
		{
			"misbehaviour Header1 is nil",
			types.NewMisbehaviour(clientID, nil, suite.header),
			func(m *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"misbehaviour Header2 is nil",
			types.NewMisbehaviour(clientID, suite.header, nil),
			func(m *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"valid misbehaviour with different trusted headers",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), clienttypes.NewHeight(0, height.RevisionHeight-3), suite.now.Add(time.Minute), suite.valSet, bothValSet, signers),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			true,
		},
		{
			"trusted height is 0 in Header1",
			&types.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), clienttypes.ZeroHeight(), suite.now.Add(time.Minute), suite.valSet, suite.valSet, signers),
				Header2:  suite.header,
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"trusted height is 0 in Header2",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), clienttypes.ZeroHeight(), suite.now.Add(time.Minute), suite.valSet, suite.valSet, signers),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"trusted valset is nil in Header1",
			&types.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now.Add(time.Minute), suite.valSet, nil, signers),
				Header2:  suite.header,
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"trusted valset is nil in Header2",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now.Add(time.Minute), suite.valSet, nil, signers),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"invalid client ID ",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now, suite.valSet, suite.valSet, signers),
				ClientId: "GAIA",
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"chainIDs do not match",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader("ethermint", int64(height.RevisionHeight), heightMinus1, suite.now, suite.valSet, suite.valSet, signers),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"header2 height is greater",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, 6, clienttypes.NewHeight(0, height.RevisionHeight+1), suite.now, suite.valSet, suite.valSet, signers),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error { return nil },
			false,
		},
		{
			"header 1 doesn't have 2/3 majority",
			&types.Misbehaviour{
				Header1:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now, bothValSet, suite.valSet, bothSigners),
				Header2:  suite.header,
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error {
				// voteSet contains only altVal which is less than 2/3 of total power (height/1height)
				wrongVoteSet := tmtypes.NewVoteSet(chainID, int64(misbehaviour.Header1.GetHeight().GetRevisionHeight()), 1, tmproto.PrecommitType, altValSet)
				blockID, err := tmtypes.BlockIDFromProto(&misbehaviour.Header1.Commit.BlockID)
				if err != nil {
					return err
				}

				tmCommit, err := makeExtCommit(suite.T().Context(), *blockID, int64(misbehaviour.Header2.GetHeight().GetRevisionHeight()), misbehaviour.Header1.Commit.Round, wrongVoteSet, altSigners, suite.now)
				misbehaviour.Header1.Commit = tmCommit.ToCommit().ToProto()

				return err
			},
			false,
		},
		{
			"header 2 doesn't have 2/3 majority",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now, bothValSet, suite.valSet, bothSigners),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error {
				// voteSet contains only altVal which is less than 2/3 of total power (height/1height)
				wrongVoteSet := tmtypes.NewVoteSet(chainID, int64(misbehaviour.Header2.GetHeight().GetRevisionHeight()), 1, tmproto.PrecommitType, altValSet)
				blockID, err := tmtypes.BlockIDFromProto(&misbehaviour.Header2.Commit.BlockID)
				if err != nil {
					return err
				}

				tmCommit, err := makeExtCommit(suite.T().Context(), *blockID, int64(misbehaviour.Header2.GetHeight().GetRevisionHeight()), misbehaviour.Header2.Commit.Round, wrongVoteSet, altSigners, suite.now)
				misbehaviour.Header2.Commit = tmCommit.ToCommit().ToProto()
				return err
			},
			false,
		},
		{
			"validators sign off on wrong commit",
			&types.Misbehaviour{
				Header1:  suite.header,
				Header2:  suite.chainA.CreateTMClientHeader(chainID, int64(height.RevisionHeight), heightMinus1, suite.now, bothValSet, suite.valSet, bothSigners),
				ClientId: clientID,
			},
			func(misbehaviour *types.Misbehaviour) error {
				tmBlockID := ibctesting.MakeBlockID(tmhash.Sum([]byte("other_hash")), 3, tmhash.Sum([]byte("other_partset")))
				misbehaviour.Header2.Commit.BlockID = tmBlockID.ToProto()
				return nil
			},
			false,
		},
	}

	for i, tc := range testCases {
		tc := tc

		err := tc.malleateMisbehaviour(tc.misbehaviour)
		suite.Require().NoError(err)

		if tc.expPass {
			suite.Require().NoError(tc.misbehaviour.ValidateBasic(), "valid test case %d failed: %s", i, tc.name)
		} else {
			suite.Require().Error(tc.misbehaviour.ValidateBasic(), "invalid test case %d passed: %s", i, tc.name)
		}
	}
}
