package consensus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testCommittee creates a committee from the given keys (for test use only).
func testCommittee(keys ...types.SecretKey) *types.Committee {
	pks := make([]types.PublicKey, len(keys))
	for i, k := range keys {
		pks[i] = k.Public()
	}
	c, err := types.NewRoundRobinElection(pks)
	if err != nil {
		panic(err)
	}
	return c
}

// seedPersistedInner is a test helper that persists a persistedInner using the public API.
func seedPersistedInner(dir string, state *persistedInner) {
	p, _, err := newPersister(dir, innerFile)
	if err != nil {
		panic(err)
	}
	if err := p.Persist(innerProtoConv.Marshal(state)); err != nil {
		panic(err)
	}
}

// loadInner is a test helper that loads persisted data and creates inner.
// Mirrors what NewState does: newPersister → newInner.
func loadInner(dir string, committee *types.Committee) (inner, error) {
	_, data, err := newPersister(dir, innerFile)
	if err != nil {
		return inner{}, err
	}
	return newInner(data, committee)
}

// makePrepareQC creates a PrepareQC with valid signatures from the given keys.
func makePrepareQC(keys []types.SecretKey, proposal *types.Proposal) *types.PrepareQC {
	var votes []*types.Signed[*types.PrepareVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewPrepareVote(proposal)))
	}
	return types.NewPrepareQC(votes)
}

func TestNewInnerEmpty(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 1)
	// No data should return empty inner (persistence disabled / fresh start)
	i, err := newInner(utils.None[[]byte](), committee)
	require.NoError(t, err)
	require.False(t, i.PrepareVote.IsPresent(), "prepareVote should be None")
	require.False(t, i.CommitVote.IsPresent(), "commitVote should be None")
	require.False(t, i.TimeoutVote.IsPresent(), "timeoutVote should be None")
}

func TestNewInnerPrepareVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create and persist a prepare vote at genesis view (0, 0)
	key := types.GenSecretKey(rng)
	committee := testCommittee(key)
	genesisProposal := types.GenProposalAt(rng, types.View{Index: 0, Number: 0})
	vote := types.Sign(key, types.NewPrepareVote(genesisProposal))

	seedPersistedInner(dir, &persistedInner{
		PrepareVote: utils.Some(vote),
	})

	// Load and verify
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	loaded, ok := i.PrepareVote.Get()
	require.True(t, ok, "prepareVote should be Some")
	require.NoError(t, utils.TestDiff(vote, loaded))
}

func TestNewInnerCommitVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create and persist a commit vote at genesis view (0, 0)
	key := types.GenSecretKey(rng)
	committee := testCommittee(key)
	genesisProposal := types.GenProposalAt(rng, types.View{Index: 0, Number: 0})
	prepareQC := makePrepareQC([]types.SecretKey{key}, genesisProposal)
	vote := types.Sign(key, types.NewCommitVote(genesisProposal))

	seedPersistedInner(dir, &persistedInner{
		PrepareQC:  utils.Some(prepareQC),
		CommitVote: utils.Some(vote),
	})

	// Load and verify
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	loaded, ok := i.CommitVote.Get()
	require.True(t, ok, "commitVote should be Some")
	require.NoError(t, utils.TestDiff(vote, loaded))
}

func TestNewInnerTimeoutVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create and persist a timeout vote at genesis view (0, 0)
	key := types.GenSecretKey(rng)
	committee := testCommittee(key)
	vote := types.NewFullTimeoutVote(key, types.View{Index: 0, Number: 0}, utils.None[*types.PrepareQC]())

	seedPersistedInner(dir, &persistedInner{
		TimeoutVote: utils.Some(vote),
	})

	// Load and verify
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	loaded, ok := i.TimeoutVote.Get()
	require.True(t, ok, "timeoutVote should be Some")
	require.NoError(t, utils.TestDiff(vote, loaded))
}

func TestNewInnerAllVotes(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Create all vote types at genesis view (0, 0)
	key := types.GenSecretKey(rng)
	committee := testCommittee(key)
	genesisProposal := types.GenProposalAt(rng, types.View{Index: 0, Number: 0})
	prepareQC := makePrepareQC([]types.SecretKey{key}, genesisProposal)
	prepareVote := types.Sign(key, types.NewPrepareVote(genesisProposal))
	commitVote := types.Sign(key, types.NewCommitVote(genesisProposal))
	timeoutVote := types.NewFullTimeoutVote(key, types.View{Index: 0, Number: 0}, utils.None[*types.PrepareQC]())

	seedPersistedInner(dir, &persistedInner{
		PrepareQC:   utils.Some(prepareQC),
		PrepareVote: utils.Some(prepareVote),
		CommitVote:  utils.Some(commitVote),
		TimeoutVote: utils.Some(timeoutVote),
	})

	// Load and verify all
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.PrepareVote.IsPresent(), "prepareVote should be Some")
	require.True(t, i.CommitVote.IsPresent(), "commitVote should be Some")
	require.True(t, i.TimeoutVote.IsPresent(), "timeoutVote should be Some")
}

func TestNewInnerPartialState(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	// Only persist prepareVote
	key := types.GenSecretKey(rng)
	committee := testCommittee(key)
	genesisProposal := types.GenProposalAt(rng, types.View{Index: 0, Number: 0})
	prepareVote := types.Sign(key, types.NewPrepareVote(genesisProposal))

	seedPersistedInner(dir, &persistedInner{
		PrepareVote: utils.Some(prepareVote),
	})

	// Load - only prepareVote should be present
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.PrepareVote.IsPresent(), "prepareVote should be Some")
	require.False(t, i.CommitVote.IsPresent(), "commitVote should be None")
	require.False(t, i.TimeoutVote.IsPresent(), "timeoutVote should be None")
}

func TestNewInnerCommitQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create a CommitQC at index 5
	proposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	vote := types.NewCommitVote(proposal)
	var votes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	qc := types.NewCommitQC(votes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC: utils.Some(qc),
	})

	// Load and verify
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.CommitQC.IsPresent(), "CommitQC should be loaded")
	loadedQC, ok := i.CommitQC.Get()
	require.True(t, ok)
	require.Equal(t, types.RoadIndex(5), loadedQC.Proposal().Index())
	// View should be (6, 0) since CommitQC at index 5 advances to index 6
	require.Equal(t, types.View{Index: 6, Number: 0}, i.View())
}

func TestNewInnerTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create a CommitQC at index 5 (required for TimeoutQC at index 6)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at (6, 2) - this advances view to (6, 3)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load and verify
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.TimeoutQC.IsPresent(), "TimeoutQC should be loaded")
	// View should be (6, 3) since TimeoutQC at (6, 2) advances to (6, 3)
	require.Equal(t, types.View{Index: 6, Number: 3}, i.View())
}

func TestNewInnerTimeoutQCOnlyGenesis(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create TimeoutQC at (0, 2) - no CommitQC needed for index 0
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 0, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load and verify - should work without CommitQC since index is 0
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.TimeoutQC.IsPresent(), "TimeoutQC should be loaded")
	require.Equal(t, types.View{Index: 0, Number: 3}, i.View())
}

func TestNewInnerTimeoutQCWithoutCommitQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create TimeoutQC at index 6 WITHOUT CommitQC at index 5
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 0}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Should return error - TimeoutQC at index 6 requires CommitQC at index 5
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerTimeoutQCAheadOfCommitQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at index 10 (way ahead of CommitQC)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 10, Number: 0}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Should return error - TimeoutQC index must equal CommitQC.Index + 1
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerViewSpecStaleTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 10
	qcProposal := types.GenProposalAt(rng, types.View{Index: 10, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at index 5 (stale - behind CommitQC).
	// Since inner is persisted atomically, a mismatched index is always corrupt.
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 5, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load - stale TimeoutQC should be treated as corrupt state
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerViewSpecValidBothQCs(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC at index 6, number 2 (valid - exactly CommitQC.Index + 1)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 2}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Load - both should be present
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.CommitQC.IsPresent(), "CommitQC should be loaded")
	require.True(t, i.TimeoutQC.IsPresent(), "TimeoutQC should be loaded")
	// View should be (6, 3) - TimeoutQC at (6, 2) advances to (6, 3)
	require.Equal(t, types.View{Index: 6, Number: 3}, i.View())
}

func TestNewInnerStaleVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create stale vote at view (3, 0) - before current view (6, 0).
	// Since inner is persisted atomically, a mismatched view is corrupt.
	staleProposal := types.GenProposalAt(rng, types.View{Index: 3, Number: 0})
	staleVote := types.Sign(keys[0], types.NewPrepareVote(staleProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(staleVote),
	})

	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerFuturePrepareVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future vote at view (10, 0) - ahead of current view (6, 0)
	futureProposal := types.GenProposalAt(rng, types.View{Index: 10, Number: 0})
	futureVote := types.Sign(keys[0], types.NewPrepareVote(futureProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(futureVote),
	})

	// Should return error - future votes indicate corrupt state
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerFutureCommitVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future commit vote at view (10, 0)
	futureProposal := types.GenProposalAt(rng, types.View{Index: 10, Number: 0})
	futureVote := types.Sign(keys[0], types.NewCommitVote(futureProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:   utils.Some(commitQC),
		CommitVote: utils.Some(futureVote),
	})

	// Should return error
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerFutureTimeoutVoteError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future timeout vote at view (10, 0)
	futureVote := types.NewFullTimeoutVote(keys[0], types.View{Index: 10, Number: 0}, utils.None[*types.PrepareQC]())

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		TimeoutVote: utils.Some(futureVote),
	})

	// Should return error
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCurrentViewVoteOk(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create vote at exactly current view (6, 0)
	currentProposal := types.GenProposalAt(rng, types.View{Index: 6, Number: 0})
	currentVote := types.Sign(keys[0], types.NewPrepareVote(currentProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(currentVote),
	})

	// Should succeed - current view votes are valid
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.PrepareVote.IsPresent(), "current view vote should be loaded")
}

func TestNewInnerCommitQCInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, _ := types.GenCommittee(rng, 3)

	// Create CommitQC signed by keys NOT in committee
	otherKeys := make([]types.SecretKey, 3)
	for i := range otherKeys {
		otherKeys[i] = types.GenSecretKey(rng)
	}
	proposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	vote := types.NewCommitVote(proposal)
	var votes []*types.Signed[*types.CommitVote]
	for _, k := range otherKeys {
		votes = append(votes, types.Sign(k, vote))
	}
	qc := types.NewCommitQC(votes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC: utils.Some(qc),
	})

	// Should return error - invalid signatures
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerTimeoutQCInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create valid CommitQC at index 5
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create TimeoutQC signed by keys NOT in committee
	otherKeys := make([]types.SecretKey, 3)
	for i := range otherKeys {
		otherKeys[i] = types.GenSecretKey(rng)
	}
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range otherKeys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 0}, utils.None[*types.PrepareQC]()))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		TimeoutQC: utils.Some(timeoutQC),
	})

	// Should return error - invalid signatures on TimeoutQC
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCurrentViewVoteInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create valid CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create vote at current view (6, 0) but signed by key NOT in committee
	otherKey := types.GenSecretKey(rng)
	currentProposal := types.GenProposalAt(rng, types.View{Index: 6, Number: 0})
	badVote := types.Sign(otherKey, types.NewPrepareVote(currentProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(badVote),
	})

	// Should return error - current view votes must have valid signatures
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerStaleVoteInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create valid CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create stale vote at (3, 0) signed by key NOT in committee.
	// Since inner is persisted atomically, a mismatched view is corrupt.
	otherKey := types.GenSecretKey(rng)
	staleProposal := types.GenProposalAt(rng, types.View{Index: 3, Number: 0})
	badVote := types.Sign(otherKey, types.NewPrepareVote(staleProposal))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareVote: utils.Some(badVote),
	})

	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerPrepareQC(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create prepareQC at genesis view (0, 0)
	proposal := types.GenProposalAt(rng, types.View{Index: 0, Number: 0})
	prepareQC := makePrepareQC(keys, proposal)

	seedPersistedInner(dir, &persistedInner{
		PrepareQC: utils.Some(prepareQC),
	})

	// Load and verify
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "prepareQC should be loaded")
}

func TestNewInnerStalePrepareQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create stale prepareQC at view (3, 0) - before current view (6, 0).
	// Since inner is persisted atomically, a mismatched view is corrupt.
	staleProposal := types.GenProposalAt(rng, types.View{Index: 3, Number: 0})
	stalePrepareQC := makePrepareQC(keys, staleProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(stalePrepareQC),
	})

	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCommitVoteWithoutPrepareQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Current view is (0, 0) (no CommitQC or TimeoutQC).
	// CommitVote requires PrepareQC justification.
	proposal := types.GenProposalAt(rng, types.View{Index: 0, Number: 0})
	commitVote := types.Sign(keys[0], types.NewCommitVote(proposal))

	seedPersistedInner(dir, &persistedInner{
		CommitVote: utils.Some(commitVote),
	})

	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "CommitVote present without PrepareQC")
}

func TestNewInnerFuturePrepareQCError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create future prepareQC at index 10 (> current 6)
	futureProposal := types.GenProposalAt(rng, types.View{Index: 10, Number: 0})
	prepareQC := makePrepareQC(keys, futureProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Should return error - future prepareQC indicates corrupt state
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerCurrentViewPrepareQCOk(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create prepareQC at current view (6, 0)
	currentProposal := types.GenProposalAt(rng, types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(keys, currentProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Should succeed - current view prepareQC is valid
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "current view prepareQC should be loaded")
}

func TestNewInnerCurrentViewPrepareQCInvalidSignatureError(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create prepareQC at current view (6, 0) but signed by keys NOT in committee
	otherKeys := make([]types.SecretKey, 3)
	for i := range otherKeys {
		otherKeys[i] = types.GenSecretKey(rng)
	}
	currentProposal := types.GenProposalAt(rng, types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(otherKeys, currentProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Should return error - current view prepareQC has invalid signatures
	_, err := loadInner(dir, committee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt persisted state")
}

func TestNewInnerPrepareQCIncludedInTimeoutVote(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)
	voteKey := keys[0]

	// Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Create prepareQC at current view (6, 0)
	currentProposal := types.GenProposalAt(rng, types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(keys, currentProposal)

	seedPersistedInner(dir, &persistedInner{
		CommitQC:  utils.Some(commitQC),
		PrepareQC: utils.Some(prepareQC),
	})

	// Load state
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "prepareQC should be loaded")

	// Simulate what voteTimeout does: create a FullTimeoutVote using i.PrepareQC
	currentView := i.View()
	timeoutVote := types.NewFullTimeoutVote(voteKey, currentView, i.PrepareQC)

	// The timeoutVote should pass verification (which checks prepareQC is correctly included)
	err = timeoutVote.Verify(committee)
	require.NoError(t, err, "timeoutVote with loaded prepareQC should verify")

	// Verify the loaded prepareQC matches what we persisted
	loadedPrepareQC, ok := i.PrepareQC.Get()
	require.True(t, ok)
	require.Equal(t, currentProposal.View(), loadedPrepareQC.Proposal().View(),
		"loaded prepareQC should have the correct view")
}

// Test that pushTimeoutQC clears stale votes and prepareQC
func TestPushTimeoutQCClearsStaleState(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	committee, keys := types.GenCommittee(rng, 3)

	// Setup: Create CommitQC at index 5 -> current view is (6, 0)
	qcProposal := types.GenProposalAt(rng, types.View{Index: 5, Number: 0})
	qcVote := types.NewCommitVote(qcProposal)
	var qcVotes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		qcVotes = append(qcVotes, types.Sign(k, qcVote))
	}
	commitQC := types.NewCommitQC(qcVotes)

	// Setup: Create prepareQC at current view (6, 0)
	currentProposal := types.GenProposalAt(rng, types.View{Index: 6, Number: 0})
	prepareQC := makePrepareQC(keys, currentProposal)

	// Setup: Create votes at current view (6, 0)
	prepareVote := types.Sign(keys[0], types.NewPrepareVote(currentProposal))
	commitVote := types.Sign(keys[0], types.NewCommitVote(currentProposal))
	timeoutVote := types.NewFullTimeoutVote(keys[0], types.View{Index: 6, Number: 0}, utils.Some(prepareQC))

	seedPersistedInner(dir, &persistedInner{
		CommitQC:    utils.Some(commitQC),
		PrepareQC:   utils.Some(prepareQC),
		PrepareVote: utils.Some(prepareVote),
		CommitVote:  utils.Some(commitVote),
		TimeoutVote: utils.Some(timeoutVote),
	})

	// Load initial state and verify everything is present
	i, err := loadInner(dir, committee)
	require.NoError(t, err)
	require.True(t, i.PrepareQC.IsPresent(), "prepareQC should be loaded")
	require.True(t, i.PrepareVote.IsPresent(), "prepareVote should be loaded")
	require.True(t, i.CommitVote.IsPresent(), "commitVote should be loaded")
	require.True(t, i.TimeoutVote.IsPresent(), "timeoutVote should be loaded")
	require.Equal(t, types.View{Index: 6, Number: 0}, i.View(), "initial view should be (6, 0)")

	// Create a TimeoutQC for current view (6, 0) that advances to (6, 1)
	var timeoutVotes []*types.FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, types.NewFullTimeoutVote(k, types.View{Index: 6, Number: 0}, utils.Some(prepareQC)))
	}
	timeoutQC := types.NewTimeoutQC(timeoutVotes)

	// Simulate pushTimeoutQC's Update callback
	newInner := inner{persistedInner{
		CommitQC:  i.CommitQC,
		TimeoutQC: utils.Some(timeoutQC),
	}}

	// Verify: view advanced to (6, 1)
	require.Equal(t, types.View{Index: 6, Number: 1}, newInner.View(), "view should advance to (6, 1)")

	// Verify: prepareQC and all votes are cleared (they're for old view)
	require.False(t, newInner.PrepareQC.IsPresent(), "prepareQC should be cleared")
	require.False(t, newInner.PrepareVote.IsPresent(), "prepareVote should be cleared")
	require.False(t, newInner.CommitVote.IsPresent(), "commitVote should be cleared")
	require.False(t, newInner.TimeoutVote.IsPresent(), "timeoutVote should be cleared")
}
