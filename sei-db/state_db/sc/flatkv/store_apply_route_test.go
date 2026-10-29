package flatkv

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
)

// routeTestMutations returns n mutations spread across all four data databases.
func routeTestMutations(n int) []mutation.Mutation {
	mutations := make([]mutation.Mutation, n)
	for i := range n {
		addr := ktype.Address{byte(i >> 16), byte(i >> 8), byte(i)}
		var key []byte
		switch i % 5 {
		case 0, 1:
			key = append([]byte{0x03}, ktype.StorageKey(addr, ktype.Slot{byte(i)})...)
		case 2:
			key = append([]byte{byte(ktype.EVMKeyAccount)}, addr[:]...)
		case 3:
			key = append([]byte{byte(keys.EVMKeyCode)}, addr[:]...)
		default:
			mutations[i] = mutation.Mutation{Key: ktype.BuildPhysicalKey("bank", addr[:]), NewValue: []byte{byte(i)}}
			continue
		}
		mutations[i] = mutation.Mutation{Key: ktype.BuildPhysicalKey(keys.EVMStoreKey, key), NewValue: []byte{byte(i)}}
	}
	return mutations
}

// serialRouteMutations is the reference routeMutations must agree with: one pass, appending in order.
func serialRouteMutations(mutations []mutation.Mutation, version int64, alreadyHave map[string]int64) [][]mutation.Mutation {
	routed := make([][]mutation.Mutation, len(dataDBDirs))
	for _, m := range mutations {
		dir, err := routePhysicalKey(m.Key)
		if err != nil {
			panic(err)
		}
		if alreadyHave[dir] >= version {
			continue
		}
		routed[dataDBIndex[dir]] = append(routed[dataDBIndex[dir]], m)
	}
	return routed
}

func TestRouteMutationsMatchesASerialPass(t *testing.T) {
	skip := map[string]int64{codeDBDir: 7}
	for _, n := range []int{0, 1, 100, minRouteChunk*3 + 17, minRouteChunk*maxRouteChunks*2 + 5} {
		for _, alreadyHave := range []map[string]int64{nil, skip} {
			t.Run(fmt.Sprintf("n=%d/skip=%v", n, alreadyHave != nil), func(t *testing.T) {
				mutations := routeTestMutations(n)
				got, err := routeMutations(mutations, 7, alreadyHave)
				require.NoError(t, err)
				require.Equal(t, serialRouteMutations(mutations, 7, alreadyHave), got)
			})
		}
	}
}

func TestRouteMutationsReportsTheFirstMalformedKey(t *testing.T) {
	mutations := routeTestMutations(minRouteChunk * 4)
	mutations[minRouteChunk*3+1].Key = []byte("no-module-separator-3")
	mutations[minRouteChunk+1].Key = []byte("no-module-separator-1")
	_, err := routeMutations(mutations, 1, nil)
	require.ErrorContains(t, err, fmt.Sprintf("%x", "no-module-separator-1"))
}
