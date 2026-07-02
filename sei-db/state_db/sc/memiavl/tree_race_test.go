package memiavl

import (
	"fmt"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestTreeProofRaceWithCommit reproduces the Immunefi 83246 / STO-601 race:
// a latest-height proof query (GetProof) that runs concurrently with a commit
// (Set + RootHash) used to mutate the shared MemNode.hash cache under a read
// lock, corrupting the cached internal hashes and diverging the AppHash.
//
// With the write-lock fix in RootHash/GetProof this must run clean under
// `go test -race`.
func TestTreeProofRaceWithCommit(t *testing.T) {
	tree := NewEmptyTree(0, 0)

	const seedKeys = 200
	seed := proto.ChangeSet{}
	for i := 0; i < seedKeys; i++ {
		seed.Pairs = append(seed.Pairs, &proto.KVPair{
			Key:   []byte(fmt.Sprintf("key%05d", i)),
			Value: []byte(fmt.Sprintf("val%05d", i)),
		})
	}
	tree.ApplyChangeSet(seed)
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)

	const iterations = 300
	var wg sync.WaitGroup

	// Writer goroutine: mimics the consensus commit path (mutate + RootHash).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			tree.ApplyChangeSet(proto.ChangeSet{Pairs: []*proto.KVPair{{
				Key:   []byte(fmt.Sprintf("key%05d", i%seedKeys)),
				Value: []byte(fmt.Sprintf("upd%05d", i)),
			}}})
			_, _, _ = tree.SaveVersion(true) // calls RootHash internally
		}
	}()

	// Reader goroutines: mimic latest-height prove=true ABCI queries plus the
	// occasional RootHash a query path may trigger.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// membership proof for a key known to exist
				_ = tree.GetProof([]byte(fmt.Sprintf("key%05d", (i*7+r)%seedKeys)))
				// non-membership proof for a key that never exists
				_ = tree.GetProof([]byte(fmt.Sprintf("absent%05d", i)))
				_ = tree.RootHash()
			}
		}(r)
	}

	wg.Wait()
}

// TestTreeConcurrentRootHash covers concurrent RootHash() calls over a tree
// whose freshly-inserted nodes still have an empty hash cache, so every caller
// races to populate MemNode.hash. The digest is deterministic, so all callers
// must agree and (under -race) must not race.
func TestTreeConcurrentRootHash(t *testing.T) {
	tree := NewEmptyTree(0, 0)

	cs := proto.ChangeSet{}
	for i := 0; i < 500; i++ {
		cs.Pairs = append(cs.Pairs, &proto.KVPair{
			Key:   []byte(fmt.Sprintf("k%05d", i)),
			Value: []byte("v"),
		})
	}
	tree.ApplyChangeSet(cs)
	// Intentionally do NOT SaveVersion(true) here: leave the node hashes unset
	// so the concurrent RootHash calls below all attempt the lazy fill.

	const workers = 8
	var wg sync.WaitGroup
	hashes := make([][]byte, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hashes[i] = tree.RootHash()
		}(i)
	}
	wg.Wait()

	for i := 1; i < workers; i++ {
		require.Equal(t, hashes[0], hashes[i], "concurrent RootHash produced divergent hashes")
	}
}
