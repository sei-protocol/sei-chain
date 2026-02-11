package mock_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/testing/mock"
)

const chainID = "testChain"

func TestGetPubKey(t *testing.T) {
	pv := mock.NewPV()
	pk, err := pv.GetPubKey(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ed25519", pk.Type())
}

func TestSignVote(t *testing.T) {
	pv := mock.NewPV()
	pk, _ := pv.GetPubKey(t.Context())

	vote := &tmproto.Vote{Height: 2}
	pv.SignVote(t.Context(), chainID, vote)

	msg := tmtypes.VoteSignBytes(chainID, vote)
	require.NoError(t, pk.Verify(msg, utils.OrPanic1(crypto.SigFromBytes(vote.Signature))))
}

func TestSignProposal(t *testing.T) {
	pv := mock.NewPV()
	pk, _ := pv.GetPubKey(t.Context())

	proposal := &tmproto.Proposal{Round: 2}
	pv.SignProposal(t.Context(), chainID, proposal)

	msg := tmtypes.ProposalSignBytes(chainID, proposal)
	require.NoError(t, pk.Verify(msg, utils.OrPanic1(crypto.SigFromBytes(proposal.Signature))))
}
