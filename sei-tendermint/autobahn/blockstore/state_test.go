package blockstore

import (
	"errors"
	"testing"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestFailedFirstWriteKeepsStoreEmpty(t *testing.T) {
	rng := utils.TestRngFromSeed(42)
	failure := errors.New("write failed")
	cfg, err := DefaultConfig(t.TempDir())
	require.NoError(t, err)
	store, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	store.table = failingWriteTable{Table: store.table, err: failure}
	err = store.WriteBlock(types.NewRoadBlock(types.GenFullCommitQCAt(rng, 42, 0), nil))
	require.ErrorIs(t, err, failure)
	require.False(t, store.Status().IsPresent())
	require.Zero(t, store.First())
	suffix, err := store.ReadSuffix()
	require.NoError(t, err)
	require.False(t, suffix.Status.IsPresent())
}

type failingWriteTable struct {
	littdb.Table
	err error
}

func (t failingWriteTable) Put([]byte, []byte, ...*litttypes.SecondaryKey) error { return t.err }
