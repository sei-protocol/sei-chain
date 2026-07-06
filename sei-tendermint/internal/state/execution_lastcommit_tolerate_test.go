//go:build mock_chain_validation

package state_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// lastCommitFakeStore implements only LoadValidators; buildLastCommitInfo calls
// nothing else. Embedding the interface leaves the rest nil (panics if touched),
// which keeps the fake honest about what the function under test depends on.
type lastCommitFakeStore struct {
	sm.Store
	vals *types.ValidatorSet
}

func (f lastCommitFakeStore) LoadValidators(int64) (*types.ValidatorSet, error) {
	return f.vals, nil
}

// Under mock_chain_validation, buildLastCommitInfo must build best-effort commit
// info rather than panic when the commit size diverges from the validator set.
// Pins: votes sized by the valset, present signatures applied, absent slots not-signed.
func TestBuildLastCommitInfo_ToleratesCommitValSetMismatch(t *testing.T) {
	valSet := genValSet(3)
	store := lastCommitFakeStore{vals: valSet}

	// Commit at height 1 with only ONE signature vs three validators.
	block := &types.Block{
		Header: types.Header{Height: 2},
		LastCommit: &types.Commit{
			Height:     1,
			Round:      0,
			Signatures: []types.CommitSig{{BlockIDFlag: types.BlockIDFlagCommit}},
		},
	}

	var ci abci.CommitInfo
	require.NotPanics(t, func() { ci = sm.BuildLastCommitInfo(block, store, 1) })

	require.Len(t, ci.Votes, 3, "votes are sized by the validator set, not the (shorter) commit")
	require.True(t, ci.Votes[0].SignedLastBlock, "the one present signature is applied")
	require.False(t, ci.Votes[1].SignedLastBlock, "validators beyond the commit are not-signed")
	require.False(t, ci.Votes[2].SignedLastBlock)
}
