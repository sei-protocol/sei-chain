package p2p

import (
	"testing"
	"time"

	"slices"
	"github.com/tendermint/tendermint/libs/utils"

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

func toMap[T comparable](vs []T) map[T]bool {
	m := map[T]bool{}
	for _,v := range vs {
		m[v] = true
	}
	return m
}

func truncate[K comparable](m map[K]time.Time, n int) map[K]time.Time {
	var keys []K
	for k := range m { keys = append(keys,k) }
	// Sort from newest to oldest.
	slices.SortFunc(keys,func(a,b K) int { return -m[a].Compare(m[b]) })
	r := map[K]time.Time{}
	if len(keys)>n { keys = keys[:max(0,n)] }
	for _,k := range keys {
		r[k] = m[k]
	}
	return r
}

func TestPeerDB(t *testing.T) {
	rng := utils.TestRng()
	db := dbm.NewMemDB()

	addrs := map[NodeAddress]time.Time{}
	for range 10 {
		t.Log("load & populate")
		peerDB,err := newPeerDB(db, &RouterOptions{})
		require.NoError(t, err)
		if err:=utils.TestDiff(justKeys(addrs),toMap(peerDB.Advertise(1000))); err!=nil {
			t.Fatal(err)
		}
		for range 20 {
			addr := makeAddr(rng)
			ts := utils.GenTimestamp(rng)
			require.NoError(t, peerDB.Insert(addr,ts))
			addrs[addr] = ts
		}
		if err:=utils.TestDiff(justKeys(addrs),toMap(peerDB.Advertise(1000))); err!=nil {
			t.Fatal(err)
		}

		t.Log("load & truncate")
		peerDB,err = newPeerDB(db, &RouterOptions{})
		require.NoError(t, err)
		if err:=utils.TestDiff(justKeys(addrs),toMap(peerDB.Advertise(1000))); err!=nil {
			t.Fatal(err)
		}
		addrs = truncate(addrs,15)
		require.NoError(t, peerDB.Truncate(15))
		if err:=utils.TestDiff(justKeys(addrs),toMap(peerDB.Advertise(1000))); err!=nil {
			t.Fatal(err)
		}

		t.Log("advertise")
		for n := range len(addrs)+5 {
			if err:=utils.TestDiff(justKeys(truncate(addrs,n)),toMap(peerDB.Advertise(n))); err!=nil {
				t.Fatal(err)
			}
		}
		t.Log("advertise with self")
		selfAddr := makeAddr(rng)
		peerDB,err = newPeerDB(db, &RouterOptions{
			SelfAddress: utils.Some(selfAddr),
		})
		require.NoError(t, err)
		for n := range len(addrs)+5 {
			x := justKeys(truncate(addrs,n-1))
			if n>0 { x[selfAddr] = true }
			if err:=utils.TestDiff(justKeys(x),toMap(peerDB.Advertise(n))); err!=nil {
				t.Fatal(err)
			}
		}
	}
}
