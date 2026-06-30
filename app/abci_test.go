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

// TestMigrationBatchSizeTakesEffectNextBlock is the full end-to-end timing
// check: a governance proposal committed in block N (written into the block's
// deliver state, then Commit) only changes the SC store's migration rate when
// block N+1's BeginBlock runs and reads it from committed state.
func TestMigrationBatchSizeTakesEffectNextBlock(t *testing.T) {
	a := Setup(t, false, false, false)
	bg := context.Background()

	// Block 1: BeginBlock runs first (param still unset), then the gov
	// proposal lands by writing into this block's deliver state, then Commit
	// persists it to the committed multistore.
	_, err := a.FinalizeBlock(bg, &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: "sei-test", Height: 1, Time: time.Now()},
	})
	require.NoError(t, err)

	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok)
	subspace.Set(a.GetContextForDeliverTx([]byte{}), migration.KeyNumKeysToMigratePerBlock, uint64(640))

	_, err = a.Commit(bg)
	require.NoError(t, err)

	// The param was committed in block 1, but BeginBlock(1) ran before it
	// existed, so the rate is still paused at this point.
	got, ok := a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 0, got, "param committed in block 1 must not take effect within block 1")

	// Block 2: BeginBlock reads the now-committed param and applies it.
	_, err = a.FinalizeBlock(bg, &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: "sei-test", Height: 2, Time: time.Now().Add(time.Second)},
	})
	require.NoError(t, err)

	got, _ = a.rootStore.GetMigrationBatchSize()
	require.Equal(t, 640, got, "migration rate must take effect on the block after the param is committed")
}
