package memiavl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// TestCopyReadRaceWithLiveCommits covers the cross-copy hole that the per-tree
// write lock does not close. RootHash and the proof builders take the write lock
// because MemNode.Hash fills the hash cache in place (Immunefi 83246), but Copy
// gives each tree its own mutex, so a copy and the tree it came from serialize
// against nothing. Reading a copy whose nodes the live tree is still free to
// mutate is the same unsynchronized access, one lock away from the fix for it.
//
// This is the trace-snapshot path: SnapshotSCStore hands EndBlock a DB.Copy, and
// ApplyChangeSets has already released db.mtx by then, so the copy can share
// nodes stamped at version+1 that the old cowVersion = t.version floor left
// mutable. Its consequence is a wrong trace or proof rather than a bad snapshot
// file, which is why it is separate from the writer test below.
func TestCopyReadRaceWithLiveCommits(t *testing.T) {
	const (
		rounds           = 4
		blocksPerRound   = 20
		keysPerChangeSet = 200
	)

	tree := seedTree(t, copyTestSeedKeys)

	for round := 0; round < rounds; round++ {
		// Copy mid-version, where DB.Copy lands between ApplyChangeSets and Commit.
		tree.ApplyChangeSet(changeSet(round*53, keysPerChangeSet, fmt.Sprintf("pre%02d", round)))
		leased := tree.Copy()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < blocksPerRound; i++ {
				_ = leased.RootHash()
				_ = leased.Get([]byte(fmt.Sprintf("key%08d", i)))
				_ = leased.GetProof([]byte(fmt.Sprintf("key%08d", i)))
			}
		}()

		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
		for block := 0; block < blocksPerRound; block++ {
			tree.ApplyChangeSet(changeSet(block*17, keysPerChangeSet, fmt.Sprintf("r%02db%02d", round, block)))
			_, _, err := tree.SaveVersion(true)
			require.NoError(t, err)
		}
		wg.Wait()
	}
}

// TestSnapshotWriteRaceWithLiveCommits reproduces the corrupted-snapshot failure
// that surfaces at startup as "leaves file size N is not a multiple of 48". The
// background rewrite serializes a Copy of the tree while consensus keeps
// committing. If the copy is not frozen, those commits mutate nodes the writer is
// traversing: Mutate clears MemNode.hash under the writer, and writeLeafDirect
// emits the 16-byte header and the hash as two writes, so a torn read of the
// cleared slice header writes a leaf record with no hash and leaves the file 32
// bytes short.
//
// The copy is taken mid-version, between a changeset and SaveVersion, which is
// what DB.Copy and CommitStore.Copy produce: ApplyChangeSets releases db.mtx on
// return, so a copy taken before the following Commit sees nodes stamped at
// version+1. That is the state the old cowVersion = t.version floor left
// mutable. The background rewrite, by contrast, copies inside Commit after
// SaveVersion, where that floor already covered every node.
//
// Driving the snapshot writer over such a copy composes the two: the mid-version
// copy is the state a real caller produces, and the writer is the reader that
// touches every reachable node, which makes it the strongest probe of the freeze.
//
// Under -race the unsynchronized MemNode access is the signal. The assertions
// then cover the on-disk result: well-formedness, and that each snapshot holds
// the tree as it stood when copied rather than some blend of it and later blocks.
func TestSnapshotWriteRaceWithLiveCommits(t *testing.T) {
	const (
		rounds           = 6
		blocksPerRound   = 30
		keysPerChangeSet = 400
	)

	dir := t.TempDir()
	tree := seedTree(t, copyTestSeedKeys)
	roundDir := func(round int) string {
		return filepath.Join(dir, fmt.Sprintf("snapshot-%d", round))
	}

	var (
		wg       sync.WaitGroup
		errMtx   sync.Mutex
		writeErr []error
	)

	// Root hash of each copy at the moment it was handed to the writer.
	wantHash := make([][]byte, rounds)

	for round := 0; round < rounds; round++ {
		tree.ApplyChangeSet(changeSet(round*137, keysPerChangeSet, fmt.Sprintf("pre%02d", round)))
		frozen := tree.Copy()
		wantHash[round] = frozen.RootHash()

		snapshotDir := roundDir(round)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := frozen.WriteSnapshot(context.Background(), snapshotDir); err != nil {
				errMtx.Lock()
				writeErr = append(writeErr, err)
				errMtx.Unlock()
			}
		}()

		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
		for block := 0; block < blocksPerRound; block++ {
			tree.ApplyChangeSet(changeSet(block*29, keysPerChangeSet, fmt.Sprintf("r%02db%02d", round, block)))
			_, _, err := tree.SaveVersion(true)
			require.NoError(t, err)
		}
	}
	wg.Wait()
	require.Empty(t, writeErr, "snapshot writer failed while the live tree committed")

	for round := 0; round < rounds; round++ {
		snapshotDir := roundDir(round)

		info, err := os.Stat(filepath.Join(snapshotDir, FileNameLeaves))
		require.NoError(t, err)
		require.Zerof(t, info.Size()%int64(SizeLeaf),
			"round %d: leaves file size %d is not a multiple of %d", round, info.Size(), SizeLeaf)

		info, err = os.Stat(filepath.Join(snapshotDir, FileNameNodes))
		require.NoError(t, err)
		require.Zerof(t, info.Size()%int64(SizeNode),
			"round %d: nodes file size %d is not a multiple of %d", round, info.Size(), SizeNode)

		// The published snapshot must reopen, which is the check that panics the
		// node on restart when a rewrite raced a commit.
		snapshot, err := OpenSnapshot(snapshotDir, Options{})
		require.NoErrorf(t, err, "round %d: snapshot written during live commits does not reopen", round)

		// A well-formed snapshot holding the wrong nodes reopens fine, so compare
		// contents: the serialized tree must be the one that was copied.
		require.Equalf(t, wantHash[round], snapshot.RootHash(),
			"round %d: snapshot contents drifted from the copy the writer was given", round)

		require.NoError(t, snapshot.Close())
	}
}
