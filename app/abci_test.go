package app

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/migration"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

// TestMigrationSubspaceRegistered verifies the generic "migration" params
// subspace is wired with its key table so governance can edit
// NumKeysToMigratePerBlock via a ParameterChangeProposal.
func TestMigrationSubspaceRegistered(t *testing.T) {
	a := Setup(t, false, false, false)
	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok, "migration subspace must be registered")
	require.True(t, subspace.HasKeyTable(), "migration subspace must have a key table")

	ctx := a.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now()})
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(123))
	var got uint64
	subspace.GetIfExists(ctx, migration.KeyNumKeysToMigratePerBlock, &got)
	require.Equal(t, uint64(123), got)
}

// TestApplyMigrationBatchSize covers the BeginBlock push: the gov param is
// read from chain state and forwarded into the SC commit store.
func TestApplyMigrationBatchSize(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := a.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now()})

	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok)

	// Unset param: the store receives the default (0 = paused).
	a.applyMigrationBatchSize(ctx)
	got, ok := a.rootStore.GetMigrationBatchSize()
	require.True(t, ok, "SC store should track a migration batch size")
	require.Equal(t, 0, got)

	// Governance raises the rate: BeginBlock forwards the new value.
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(500))
	a.applyMigrationBatchSize(ctx)
	got, _ = a.rootStore.GetMigrationBatchSize()
	require.Equal(t, 500, got)

	// Defense-in-depth: an out-of-range value reaching state (gov validation
	// already rejects these) is clamped to the sane maximum, never overflowing
	// the int cast or the migration iterator preallocation.
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(math.MaxUint64))
	a.applyMigrationBatchSize(ctx)
	got, _ = a.rootStore.GetMigrationBatchSize()
	require.Equal(t, int(migration.MaxNumKeysToMigratePerBlock), got)
}

// TestBeginBlockAppliesMigrationBatchSize exercises the full BeginBlock path
// (not the helper in isolation): it mimics a governance ParameterChangeProposal
// having set NumKeysToMigratePerBlock, then runs app.BeginBlock and asserts the
// new rate landed in the SC commit store.
func TestBeginBlockAppliesMigrationBatchSize(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := a.NewContext(false, tmproto.Header{Height: 2, ChainID: "sei-test", Time: time.Now()})

	// Sanity: nothing set yet, so the store is paused at 0.
	before, ok := a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 0, before)

	// Simulate the gov proposal landing in chain state.
	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok)
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(321))

	// Run the real BeginBlock (checkHeight=false to skip height validation).
	require.NotPanics(t, func() {
		a.BeginBlock(ctx, 2, nil, nil, false)
	})

	after, _ := a.rootStore.GetMigrationBatchSize()
	require.Equal(t, 321, after, "BeginBlock should push the gov param into the SC store")
}

// TestMigrationBatchSizeTakesEffectNextBlock pins when a param change reaches the SC store: BeginBlock
// applies whatever the param says at the moment it runs, so a proposal that lands later in a block cannot
// change the rate that block is already migrating at — only the next block's BeginBlock picks it up.
//
// The BeginBlock step is driven directly rather than through FinalizeBlock/Commit because the sequence
// this pins has no legal block-level spelling: getting the param written after BeginBlock but committed at
// the same height means writing between FinalizeBlock and Commit, which changes committed state for a
// height whose app hash was already announced to Tendermint. See TestMigrationBatchSizeAppliedAtBlockStart
// for the same path through a real block.
func TestMigrationBatchSizeTakesEffectNextBlock(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := a.GetContextForDeliverTx([]byte{})

	// A block begins with the param unset, so the lazily-persisted default leaves migration paused.
	a.applyMigrationBatchSize(ctx)
	got, ok := a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 0, got, "the default param must leave migration paused")

	// A gov proposal raises the rate part-way through that same block.
	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok)
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(640))

	got, ok = a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 0, got, "a param write must not change the rate the current block is migrating at")

	// The next block's BeginBlock reads the param and applies it.
	a.applyMigrationBatchSize(ctx)
	got, ok = a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 640, got, "the next BeginBlock must apply the new rate")
}

// TestMigrationBatchSizeAppliedAtBlockStart covers the same path through a real block: a param already in
// state when the block starts is applied by that block's BeginBlock and survives the commit.
func TestMigrationBatchSizeAppliedAtBlockStart(t *testing.T) {
	a := Setup(t, false, false, false)
	bg := context.Background()

	// Written into the genesis deliver state, before any block's app hash has been taken.
	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok)
	subspace.Set(a.GetContextForDeliverTx([]byte{}), migration.KeyNumKeysToMigratePerBlock, uint64(640))

	_, err := a.FinalizeBlock(bg, &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: "sei-test", Height: 1, Time: time.Now()},
	})
	require.NoError(t, err)
	_, err = a.Commit(bg)
	require.NoError(t, err)

	got, ok := a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 640, got, "BeginBlock must apply a param already in state when the block starts")
}
