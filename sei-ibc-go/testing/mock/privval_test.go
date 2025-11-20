package mock_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/ibc-go/v3/testing/mock"
)

const chainID = "testChain"

func TestGetPubKey(t *testing.T) {
	pv := mock.NewPV()
	pk, err := pv.GetPubKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, "ed25519", pk.Type())
}

func TestSignVote(t *testing.T) {
	pv := mock.NewPV()
	pk, _ := pv.GetPubKey(context.Background())

	vote := &tmproto.Vote{Height: 2}
	pv.SignVote(context.Background(), chainID, vote)

	msg := tmtypes.VoteSignBytes(chainID, vote)
	ok := pk.VerifySignature(msg, vote.Signature)
	require.True(t, ok)
}

func TestSignProposal(t *testing.T) {
	pv := mock.NewPV()
	pk, _ := pv.GetPubKey(context.Background())

	proposal := &tmproto.Proposal{Round: 2}
	pv.SignProposal(context.Background(), chainID, proposal)

	msg := tmtypes.ProposalSignBytes(chainID, proposal)
	ok := pk.VerifySignature(msg, proposal.Signature)
	require.True(t, ok)
}
