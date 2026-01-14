package store

import (
	"testing"

	"github.com/sei-protocol/sei-stream/pkg/require"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/sei-protocol/sei-stream/storage/db"
	dbtypes "github.com/sei-protocol/sei-stream/storage/db/types"
	streamtypes "github.com/tendermint/tendermint/internal/autobahn/types"
)

func TestTipCutStore(t *testing.T) {
	rng := utils.TestRng()

	db, err := db.OpenDB("", TipCutDBName, "", dbtypes.GenericKind)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	store := NewTipCutStore(db)
	want := streamtypes.GenCommitQC(rng)
	store.Set(want)
	key := uint64(want.Proposal().Index())

	// try get - hits cache
	got, found := store.cache.Get(key)
	require.True(t, found)
	require.NoError(t, utils.TestDiff(want, got))

	// try get from DB - shouldn't exist yet
	val, err := db.Get(tipCutStoreKeySerializer(key))
	require.NoError(t, err)
	require.Nil(t, val)

	require.NoError(t, store.FlushToDB(key))

	// try get from DB - should exist now
	val, err = db.Get(tipCutStoreKeySerializer(key))
	require.NoError(t, err)
	parsed, err := tipCutStoreValueDeserializer(val)
	require.NoError(t, err)
	require.NoError(t, utils.TestDiff(want, parsed))
}
