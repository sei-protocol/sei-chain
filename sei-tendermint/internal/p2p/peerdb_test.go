package p2p

import (
	"iter"
	"testing"
	"time"

	"github.com/tendermint/tendermint/libs/utils"
	"slices"

	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils/require"
)

func justKeys[K comparable, V any](m map[K]V) map[K]bool {
	r := map[K]bool{}
	for k := range m {
		r[k] = true
	}
	return r
}

func toMap[T comparable](vs iter.Seq[T]) map[T]bool {
	m := map[T]bool{}
	for v := range vs {
		m[v] = true
	}
	return m
}

func truncate[K comparable](m map[K]time.Time, n int) map[K]time.Time {
	var keys []K
	for k := range m {
		keys = append(keys, k)
	}
	// Sort from newest to oldest.
	slices.SortFunc(keys, func(a, b K) int { return -m[a].Compare(m[b]) })
	r := map[K]time.Time{}
	if len(keys) > n {
		keys = keys[:max(0, n)]
	}
	for _, k := range keys {
		r[k] = m[k]
	}
	return r
}

func TestPeerDB(t *testing.T) {
	rng := utils.TestRng()
	db := dbm.NewMemDB()

	addrs := map[NodeAddress]time.Time{}
	maxRows := 30
	for range 10 {
		t.Log("load")
		peerDB, err := newPeerDB(db, maxRows)
		require.NoError(t, err)
		if err := utils.TestDiff(justKeys(addrs), toMap(peerDB.All())); err != nil {
			t.Fatal(err)
		}
		t.Log("populate")
		for range 20 {
			addr := makeAddr(rng)
			ts := utils.GenTimestamp(rng)
			require.NoError(t, peerDB.Insert(addr, ts))
			addrs[addr] = ts
		}
		addrs = truncate(addrs, maxRows)
		if err := utils.TestDiff(justKeys(addrs), toMap(peerDB.All())); err != nil {
			t.Fatal(err)
		}
	}
}
