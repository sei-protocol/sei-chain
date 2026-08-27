package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/address"
)

var addr = sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())

func TestProposalKeys(t *testing.T) {
	// key proposal
	key := ProposalKey(1)
	proposalID := SplitProposalKey(key)
	require.Equal(t, int(proposalID), 1)

	// key active proposal queue
	now := time.Now()
	key = ActiveProposalQueueKey(3, now)
	proposalID, expTime := SplitActiveProposalQueueKey(key)
	require.Equal(t, int(proposalID), 3)
	require.True(t, now.Equal(expTime))

	// key inactive proposal queue
	key = InactiveProposalQueueKey(3, now)
	proposalID, expTime = SplitInactiveProposalQueueKey(key)
	require.Equal(t, int(proposalID), 3)
	require.True(t, now.Equal(expTime))

	// invalid key
	require.Panics(t, func() { SplitProposalKey([]byte("test")) })
	require.Panics(t, func() { SplitInactiveProposalQueueKey([]byte("test")) })
}

func TestDepositKeys(t *testing.T) {

	key := DepositsKey(2)
	proposalID := SplitProposalKey(key)
	require.Equal(t, int(proposalID), 2)

	key = DepositKey(2, addr)
	proposalID, depositorAddr := SplitKeyDeposit(key)
	require.Equal(t, int(proposalID), 2)
	require.Equal(t, addr, depositorAddr)
}

func TestVoteKeys(t *testing.T) {

	key := VotesKey(2)
	proposalID := SplitProposalKey(key)
	require.Equal(t, int(proposalID), 2)

	key = VoteKey(2, addr)
	proposalID, voterAddr := SplitKeyDeposit(key)
	require.Equal(t, int(proposalID), 2)
	require.Equal(t, addr, voterAddr)
}

func TestTallyKeys(t *testing.T) {
	require.Equal(t, append(TallyProgressKeyPrefix, GetProposalIDBytes(2)...), TallyProgressKey(2))
	require.NotEqual(t, TallyVotesKey(2, true), TallyVotesKey(2, false))
	require.NotEqual(t, TallyVoteKey(2, true, addr), TallyVoteKey(2, false, addr))
	require.NotEqual(t, TallyCleanupKey(2, true), TallyCleanupKey(2, false))
	require.Equal(t, append(append(VoteDelegationsKeyPrefix, GetProposalIDBytes(2)...), address.MustLengthPrefix(addr.Bytes())...), VoteDelegationsKey(2, addr))
	require.Equal(t, append(append(append(TallyVoteDelegationsKeyPrefix, GetProposalIDBytes(2)...), byte(1)), address.MustLengthPrefix(addr.Bytes())...), TallyVoteDelegationsKey(2, false, addr))
}
