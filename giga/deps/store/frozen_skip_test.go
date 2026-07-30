package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	gigastore "github.com/sei-protocol/sei-chain/giga/deps/store"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/dbadapter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

func bz(s string) []byte { return []byte(s) }

// TestGigaFrozenEmptyLayerSkip verifies that Get through a deep stack of frozen,
// empty giga cache layers returns exactly the base value (the giga store is used
// for the evm/bank stores, so EVM SLOAD at call depth N would otherwise walk N
// layers).
func TestGigaFrozenEmptyLayerSkip(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	mem.Set(bz("k1"), bz("v1"))
	mem.Set(bz("k2"), bz("v2"))

	var parent types.KVStore = mem
	layers := make([]*gigastore.Store, 32)
	for i := 0; i < 32; i++ {
		s := gigastore.NewStore(parent, types.NewKVStoreKey("giga"), types.DefaultCacheSizeLimit)
		layers[i] = s
		parent = s
	}
	for i := 0; i < len(layers)-1; i++ {
		layers[i].Freeze()
	}
	top := layers[len(layers)-1]

	require.Equal(t, bz("v1"), top.Get(bz("k1")))
	require.Equal(t, bz("v2"), top.Get(bz("k2")))
	require.Nil(t, top.Get(bz("missing")))
}

// TestGigaFrozenLayerWithWriteNotSkipped verifies a frozen giga layer with a write
// (or delete) still shadows the base and is never skipped.
func TestGigaFrozenLayerWithWriteNotSkipped(t *testing.T) {
	mem := dbadapter.Store{DB: dbm.NewMemDB()}
	mem.Set(bz("k1"), bz("v1"))
	mem.Set(bz("k2"), bz("v2"))

	mid := gigastore.NewStore(mem, types.NewKVStoreKey("giga"), types.DefaultCacheSizeLimit)
	mid.Set(bz("k1"), bz("override"))
	mid.Delete(bz("k2"))
	mid.Freeze()

	top := gigastore.NewStore(mid, types.NewKVStoreKey("giga"), types.DefaultCacheSizeLimit)
	require.Equal(t, bz("override"), top.Get(bz("k1")))
	require.Nil(t, top.Get(bz("k2")))
}
