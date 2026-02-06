package grpc_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmgrpc "github.com/sei-protocol/sei-chain/sei-tendermint/privval/grpc"
	privvalproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/privval"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const ChainID = "123"

var testKey = ed25519.TestSecretKey([]byte("test"))

func TestGetPubKey(t *testing.T) {

	testCases := []struct {
		name string
		pv   types.PrivValidator
		err  bool
	}{
		{name: "valid", pv: types.NewMockPV(), err: false},
		{name: "error on pubkey", pv: types.NewErroringMockPV(), err: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			logger := log.NewTestingLogger(t)

			s := tmgrpc.NewSignerServer(logger, ChainID, tc.pv)

			req := &privvalproto.PubKeyRequest{ChainId: ChainID}
			resp, err := s.GetPubKey(ctx, req)
			if tc.err {
				require.Error(t, err)
			} else {
				pk, err := tc.pv.GetPubKey(ctx)
				require.NoError(t, err)
				require.Equal(t, resp.PubKey, crypto.PubKeyToProto(pk))
			}
		})
	}

}

func TestSignVote(t *testing.T) {

	ts := time.Now()
	hash := tmrand.Bytes(crypto.HashSize)
	valAddr := tmrand.Bytes(crypto.AddressSize)

	testCases := []struct {
		name       string
		pv         types.PrivValidator
		have, want *types.Vote
		err        bool
	}{
		{name: "valid", pv: types.NewMockPV(), have: &types.Vote{
			Type:             tmproto.PrecommitType,
			Height:           1,
			Round:            2,
			BlockID:          types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp:        ts,
			ValidatorAddress: valAddr,
			ValidatorIndex:   1,
		}, want: &types.Vote{
			Type:             tmproto.PrecommitType,
			Height:           1,
			Round:            2,
			BlockID:          types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp:        ts,
			ValidatorAddress: valAddr,
			ValidatorIndex:   1,
		},
			err: false},
		{name: "invalid vote", pv: types.NewErroringMockPV(), have: &types.Vote{
			Type:             tmproto.PrecommitType,
			Height:           1,
			Round:            2,
			BlockID:          types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp:        ts,
			ValidatorAddress: valAddr,
			ValidatorIndex:   1,
			Signature:        utils.Some(testKey.Sign([]byte("signed"))),
		}, want: &types.Vote{
			Type:             tmproto.PrecommitType,
			Height:           1,
			Round:            2,
			BlockID:          types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp:        ts,
			ValidatorAddress: valAddr,
			ValidatorIndex:   1,
			Signature:        utils.Some(testKey.Sign([]byte("signed"))),
		},
			err: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			logger := log.NewTestingLogger(t)

			s := tmgrpc.NewSignerServer(logger, ChainID, tc.pv)

			req := &privvalproto.SignVoteRequest{ChainId: ChainID, Vote: tc.have.ToProto()}
			resp, err := s.SignVote(ctx, req)
			if tc.err {
				require.Error(t, err)
			} else {
				pbVote := tc.want.ToProto()

				require.NoError(t, tc.pv.SignVote(ctx, ChainID, pbVote))

				assert.Equal(t, pbVote.Signature, resp.Vote.Signature)
			}
		})
	}
}

func TestSignProposal(t *testing.T) {

	ts := time.Now()
	hash := tmrand.Bytes(crypto.HashSize)

	testCases := []struct {
		name       string
		pv         types.PrivValidator
		have, want *types.Proposal
		err        bool
	}{
		{name: "valid", pv: types.NewMockPV(), have: &types.Proposal{
			Type:      tmproto.ProposalType,
			Height:    1,
			Round:     2,
			POLRound:  2,
			BlockID:   types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp: ts,
		}, want: &types.Proposal{
			Type:      tmproto.ProposalType,
			Height:    1,
			Round:     2,
			POLRound:  2,
			BlockID:   types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp: ts,
		},
			err: false},
		{name: "invalid proposal", pv: types.NewErroringMockPV(), have: &types.Proposal{
			Type:      tmproto.ProposalType,
			Height:    1,
			Round:     2,
			POLRound:  2,
			BlockID:   types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp: ts,
			Signature: testKey.Sign([]byte("signed")),
		}, want: &types.Proposal{
			Type:      tmproto.ProposalType,
			Height:    1,
			Round:     2,
			POLRound:  2,
			BlockID:   types.BlockID{Hash: hash, PartSetHeader: types.PartSetHeader{Hash: hash, Total: 2}},
			Timestamp: ts,
			Signature: testKey.Sign([]byte("signed")),
		},
			err: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			logger := log.NewTestingLogger(t)

			s := tmgrpc.NewSignerServer(logger, ChainID, tc.pv)

			req := &privvalproto.SignProposalRequest{ChainId: ChainID, Proposal: tc.have.ToProto()}
			resp, err := s.SignProposal(ctx, req)
			if tc.err {
				require.Error(t, err)
			} else {
				pbProposal := tc.want.ToProto()
				require.NoError(t, tc.pv.SignProposal(ctx, ChainID, pbProposal))
				assert.Equal(t, pbProposal.Signature, resp.Proposal.Signature)
			}
		})
	}
}
